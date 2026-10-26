//go:build integration

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"secunda/internal/cache"
	"secunda/internal/testutil"
)

func testService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db := testutil.Database(t)
	auth, err := NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, nil, auth), db
}

func seedUser(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO users (email, password_hash, name, created_at) VALUES (?, 'unused', ?, UTC_TIMESTAMP(6))`, name+"@example.com", name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedTeam(t *testing.T, s *Service, owner int64) Team {
	t.Helper()
	team, err := s.CreateTeam(context.Background(), owner, "Test team")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestTeamStatsIntegration(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	owner := seedUser(t, db, "owner")
	team := seedTeam(t, s, owner)
	otherTeam := seedTeam(t, s, owner)
	emptyTeam := seedTeam(t, s, owner)
	users := make([]int64, 4)
	for i := range users {
		users[i] = seedUser(t, db, fmt.Sprintf("worker%d", i))
		if err := s.Invite(ctx, owner, team.ID, users[i], Member); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeRole(ctx, owner, team.ID, users[1], Admin); err != nil {
		t.Fatal(err)
	}
	outsider := seedUser(t, db, "outsider")
	var now time.Time
	if err := db.QueryRow(`SELECT UTC_TIMESTAMP(6)`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	seed := func(teamID int64, status Status, assignee *int64, closed time.Time, duration time.Duration) int64 {
		t.Helper()
		var closedAt *time.Time
		createdAt := now.Add(-48 * time.Hour)
		if status == Done {
			closedAt = &closed
			createdAt = closed.Add(-duration)
		}
		result, err := db.Exec(`INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version)
			VALUES (?, 'Task', '', ?, ?, ?, ?, ?, ?, 1)`, teamID, status, owner, assignee, createdAt, now, closedAt)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	recent := now.Add(-24 * time.Hour)
	ids := make([]int64, 0)
	for i := range 3 {
		ids = append(ids, seed(team.ID, Done, &users[0], recent, time.Duration(i+1)*time.Hour))
	}
	for range 2 {
		ids = append(ids, seed(team.ID, Done, &users[1], recent, time.Hour))
	}
	ids = append(ids, seed(team.ID, Done, &users[2], recent, time.Hour))
	ids = append(ids, seed(team.ID, Done, &users[3], recent, time.Hour))
	ids = append(ids, seed(team.ID, Done, &users[0], now.Add(-31*24*time.Hour), 2*time.Hour))
	ids = append(ids, seed(team.ID, Done, nil, recent, time.Hour))
	ids = append(ids, seed(team.ID, Todo, nil, time.Time{}, 0))
	ids = append(ids, seed(team.ID, Todo, nil, time.Time{}, 0))
	ids = append(ids, seed(team.ID, InProgress, nil, time.Time{}, 0))
	for range 5 {
		id := seed(otherTeam.ID, Done, &owner, recent, 100*time.Hour)
		mustExec(t, db, `INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, 'foreign', ?)`, id, owner, now)
	}
	for _, id := range []int64{ids[0], ids[0], ids[1], ids[9]} {
		mustExec(t, db, `INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, 'comment', ?)`, id, owner, now)
	}
	for _, userID := range []int64{owner, users[1]} {
		stats, err := s.TeamStats(ctx, userID, team.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stats.TeamID != team.ID || stats.ByStatus[Todo] != 2 || stats.ByStatus[InProgress] != 1 || stats.ByStatus[Done] != 9 {
			t.Fatalf("unexpected statuses: %+v", stats)
		}
		if len(stats.TopAssignees) != 3 {
			t.Fatalf("top assignees: %+v", stats.TopAssignees)
		}
		for i, count := range []int64{3, 2, 1} {
			if stats.TopAssignees[i].UserID != users[i] || stats.TopAssignees[i].ClosedTasks != count || stats.TopAssignees[i].Name != fmt.Sprintf("worker%d", i) {
				t.Fatalf("wrong ranking: %+v", stats.TopAssignees)
			}
		}
		if stats.AverageCloseSeconds == nil || math.Abs(*stats.AverageCloseSeconds-5200) > 0.001 {
			t.Fatalf("wrong average: %+v", stats.AverageCloseSeconds)
		}
		if stats.CommentsTotal != 4 || len(stats.CommentsByTask) != 12 {
			t.Fatalf("wrong comments: %+v", stats)
		}
		for i, item := range stats.CommentsByTask {
			want := int64(0)
			if i == 0 {
				want = 2
			} else if i == 1 || i == 9 {
				want = 1
			}
			if item.TaskID != ids[i] || item.Count != want {
				t.Fatalf("comment count: %+v, want task %d count %d", item, ids[i], want)
			}
		}
	}
	if _, err := s.TeamStats(ctx, users[0], team.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member stats: %v", err)
	}
	if _, err := s.TeamStats(ctx, outsider, team.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider stats: %v", err)
	}
	stats, err := s.TeamStats(ctx, owner, emptyTeam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AverageCloseSeconds != nil || stats.CommentsTotal != 0 || len(stats.TopAssignees) != 0 || len(stats.CommentsByTask) != 0 || stats.ByStatus[Done] != 0 {
		t.Fatalf("empty team stats: %+v", stats)
	}
}

func TestTaskConcurrencyAndHistory(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	owner := seedUser(t, db, "owner")
	team := seedTeam(t, s, owner)
	task, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"First", "Second"} {
		wg.Go(func() {
			<-start
			_, err := s.UpdateTask(ctx, owner, task.ID, UpdateTaskInput{Version: 1, Title: &title})
			errorsCh <- err
		})
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	successes, conflicts := 0, 0
	for err := range errorsCh {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	current, err := s.GetTask(ctx, owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 {
		t.Fatalf("version = %d", current.Version)
	}
	history, err := s.ListHistory(ctx, owner, task.ID, 20, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	var changes map[string]Change
	if err := json.Unmarshal(history[0].Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if changes["title"].Old != "Original" || changes["title"].New != current.Title || history[0].ChangedBy != owner {
		t.Fatalf("wrong history: %+v", history[0])
	}
	done := Done
	closed, err := s.UpdateTask(ctx, owner, task.ID, UpdateTaskInput{Version: 2, Status: &done})
	if err != nil || closed.ClosedAt == nil {
		t.Fatalf("closing: %+v %v", closed, err)
	}
	todo := Todo
	reopened, err := s.UpdateTask(ctx, owner, task.ID, UpdateTaskInput{Version: 3, Status: &todo})
	if err != nil || reopened.ClosedAt != nil {
		t.Fatalf("reopening: %+v %v", reopened, err)
	}
	noop, err := s.UpdateTask(ctx, owner, task.ID, UpdateTaskInput{Version: 4, Status: &todo})
	if err != nil || noop.Version != 4 {
		t.Fatalf("no-op: %+v %v", noop, err)
	}
	history, err = s.ListHistory(ctx, owner, task.ID, 20, 0)
	if err != nil || len(history) != 4 {
		t.Fatalf("no-op changed history: %d %v", len(history), err)
	}
}

func TestTaskHistoryRollback(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	owner := seedUser(t, db, "owner")
	team := seedTeam(t, s, owner)
	task, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TRIGGER reject_history BEFORE INSERT ON task_history FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected history failure'`)
	title := "Must roll back"
	if _, err := s.UpdateTask(ctx, owner, task.ID, UpdateTaskInput{Version: 1, Title: &title}); err == nil {
		t.Fatal("update should fail")
	}
	if _, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: title}); err == nil {
		t.Fatal("create should fail")
	}
	current, err := s.GetTask(ctx, owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Title != "Original" || current.Version != 1 {
		t.Fatalf("update did not roll back: %+v", current)
	}
	var tasks, histories, revision int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_history`).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT tasks_revision FROM teams WHERE id = ?`, team.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || histories != 1 || revision != 1 {
		t.Fatalf("partial commit: tasks=%d history=%d revision=%d", tasks, histories, revision)
	}
}

func TestTeamCreationRollback(t *testing.T) {
	s, db := testService(t)
	owner := seedUser(t, db, "owner")
	mustExec(t, db, `CREATE TRIGGER reject_member BEFORE INSERT ON team_members FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected member failure'`)
	if _, err := s.CreateTeam(context.Background(), owner, "Must roll back"); err == nil {
		t.Fatal("create should fail")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM teams`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("team was committed without its owner")
	}
}

func TestTaskCacheAndPagination(t *testing.T) {
	s, db := testService(t)
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Fatal("TEST_REDIS_ADDR is required")
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	taskCache := cache.New(address, "", 0, logger)
	s.cache = taskCache
	t.Cleanup(func() { taskCache.Close() })
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	owner := seedUser(t, db, "owner")
	outsider := seedUser(t, db, "outsider")
	member := seedUser(t, db, "member")
	team := seedTeam(t, s, owner)
	if err := s.Invite(ctx, owner, team.ID, member, Member); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if _, err := s.CreateTask(ctx, owner, CreateTaskInput{TeamID: team.ID, Title: fmt.Sprint(i), AssigneeID: &member}); err != nil {
			t.Fatal(err)
		}
	}
	f := TaskFilter{TeamID: team.ID, Limit: 2}
	page, err := s.ListTasks(ctx, owner, f)
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	keys, err := client.Keys(ctx, "tasks:*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("cache keys: %v %v", keys, err)
	}
	ttl, err := client.TTL(ctx, keys[0]).Result()
	if err != nil || ttl <= 4*time.Minute || ttl > 5*time.Minute {
		t.Fatalf("cache TTL: %v %v", ttl, err)
	}
	if _, err := s.ListTasks(ctx, outsider, f); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cache leaked to outsider: %v", err)
	}
	cached, err := s.ListTasks(ctx, member, f)
	if err != nil || len(cached.Items) != 2 || cached.Items[0].ID != page.Items[0].ID {
		t.Fatalf("cached page: %+v %v", cached, err)
	}
	firstID := page.Items[0].ID
	title := "Updated"
	if _, err := s.UpdateTask(ctx, owner, firstID, UpdateTaskInput{Version: 1, Title: &title}); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListTasks(ctx, member, f)
	if err != nil || page.Items[0].Title != title {
		t.Fatalf("stale cache: %+v %v", page, err)
	}
	f.Cursor = page.NextCursor
	second, err := s.ListTasks(ctx, owner, f)
	if err != nil || len(second.Items) != 2 || second.Items[0].ID >= page.Items[1].ID {
		t.Fatalf("second page: %+v %v", second, err)
	}
	f.Cursor = second.NextCursor
	last, err := s.ListTasks(ctx, owner, f)
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatalf("last page: %+v %v", last, err)
	}
	f.Cursor, f.Offset = "", 2
	offset, err := s.ListTasks(ctx, owner, f)
	if err != nil || offset.Items[0].ID != second.Items[0].ID {
		t.Fatalf("offset page: %+v %v", offset, err)
	}
	f.Offset, f.Status = 0, Done
	empty, err := s.ListTasks(ctx, owner, f)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("status filter: %+v %v", empty, err)
	}
	f.Status, f.AssigneeID = "", owner
	empty, err = s.ListTasks(ctx, owner, f)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("assignee filter: %+v %v", empty, err)
	}
	broken := cache.New("127.0.0.1:1", "", 0, logger)
	defer broken.Close()
	s.cache = broken
	f.AssigneeID = 0
	for range 6 {
		if _, err := s.ListTasks(ctx, owner, f); err != nil {
			t.Fatalf("Redis failure broke reads: %v", err)
		}
	}
	title = "Changed while cache unavailable"
	if _, err := s.UpdateTask(ctx, owner, firstID, UpdateTaskInput{Version: 2, Title: &title}); err != nil {
		t.Fatal(err)
	}
	s.cache = taskCache
	recovered, err := s.ListTasks(ctx, owner, f)
	if err != nil || recovered.Items[0].Title != title {
		t.Fatalf("stale data after cache recovery: %+v %v", recovered, err)
	}
}
