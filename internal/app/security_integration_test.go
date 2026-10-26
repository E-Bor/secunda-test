//go:build integration

package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"secunda/internal/cache"
)

func TestLoginRejectsTruncatedPassword(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	password := strings.Repeat("p", 72)
	user, err := s.Register(ctx, RegisterInput{Email: "long@example.com", Password: password, Name: "Long password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, user.Email, password); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, user.Email, password+"different suffix"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("password with an ignored suffix was accepted: %v", err)
	}
}

func TestLoginEmailIdentity(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	user, err := s.Register(ctx, RegisterInput{Email: "alice@example.com", Password: "password123", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "ALICE@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "álice@example.com", "password123"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("distinct Unicode email authenticated as %s: %v", user.Email, err)
	}
	other, err := s.Register(ctx, RegisterInput{Email: "álice@example.com", Password: "other-password", Name: "Other Alice"})
	if err != nil {
		t.Fatalf("distinct Unicode email could not register: %v", err)
	}
	if other.ID == user.ID {
		t.Fatal("distinct addresses share an identity")
	}
}

func TestUnauthorizedUpdateDoesNotWaitForTaskLock(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	owner := seedUser(t, db, "owner")
	outsider := seedUser(t, db, "outsider")
	member := seedUser(t, db, "member")
	team := seedTeam(t, s, owner)
	if err := s.Invite(ctx, owner, team.ID, member, Member); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var id int64
	if err := lock.QueryRow(`SELECT id FROM tasks WHERE id = ? FOR UPDATE`, task.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		userID int64
		want   error
	}{
		{"outsider", outsider, ErrNotFound},
		{"unrelated member", member, ErrForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			status := Done
			_, err := s.UpdateTask(ctx, tt.userID, task.ID, UpdateTaskInput{Version: 1, Status: &status})
			if !errors.Is(err, tt.want) {
				t.Fatalf("unauthorized request waited for a task lock: %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSQLInjectionIsData(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	owner := seedUser(t, db, "owner")
	outsider := seedUser(t, db, "outsider")
	team := seedTeam(t, s, owner)
	for _, payload := range []string{
		`' OR 1=1 --`,
		`'); DROP TABLE users; --`,
		`x' UNION SELECT 1,2,3 --`,
		`\\'; SELECT SLEEP(10); --`,
		`<script>alert("xss")</script>`,
	} {
		t.Run(payload, func(t *testing.T) {
			task, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: payload, Description: payload})
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetTask(ctx, owner, task.ID)
			if err != nil || got.Title != payload || got.Description != payload {
				t.Fatalf("input was not stored as data: %+v %v", got, err)
			}
			if _, err := s.Login(ctx, payload, "password123"); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("login injection: %v", err)
			}
			if _, err := s.ListTasks(ctx, owner, TaskFilter{TeamID: team.ID, Status: Status(payload), Limit: 20}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("filter injection: %v", err)
			}
			if _, err := s.GetTask(ctx, outsider, task.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("foreign task access: %v", err)
			}
		})
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("users table was modified: %d %v", count, err)
	}
}

func TestSharedRedisSeparatesDeployments(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Fatal("TEST_REDIS_ADDR is required")
	}
	first, firstDB := testService(t)
	second, secondDB := testService(t)
	first.auth.secret = []byte(strings.Repeat("a", 32))
	second.auth.secret = []byte(strings.Repeat("b", 32))
	shared := cache.New(address, "", 0, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	defer shared.Close()
	first.cache, second.cache = shared, shared
	ctx := context.Background()
	firstOwner := seedUser(t, firstDB, "first")
	secondOwner := seedUser(t, secondDB, "second")
	firstTeam := seedTeam(t, first, firstOwner)
	secondTeam := seedTeam(t, second, secondOwner)
	if firstTeam.ID != secondTeam.ID {
		t.Fatal("test requires colliding team IDs in different databases")
	}
	if _, err := first.CreateTask(ctx, firstOwner, CreateTaskInput{TeamID: firstTeam.ID, Title: "Private first deployment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.CreateTask(ctx, secondOwner, CreateTaskInput{TeamID: secondTeam.ID, Title: "Second deployment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ListTasks(ctx, firstOwner, TaskFilter{TeamID: firstTeam.ID, Limit: 20}); err != nil {
		t.Fatal(err)
	}
	page, err := second.ListTasks(ctx, secondOwner, TaskFilter{TeamID: secondTeam.ID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Second deployment" {
		t.Fatalf("cache leaked data between deployments: %+v", page)
	}
}
