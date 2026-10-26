//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"secunda/internal/app"
	"secunda/internal/testutil"
)

func TestAPIIntegration(t *testing.T) {
	db := testutil.Database(t)
	auth, err := app.NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(db, nil, auth)
	handler := New(service, auth, db, slog.New(slog.NewJSONHandler(io.Discard, nil)), 1000, 1000)
	call := func(method, path, token, body string, status int, result any) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s: HTTP %d, want %d: %s", method, path, response.Code, status, response.Body)
		}
		if response.Header().Get("X-Request-ID") == "" {
			t.Fatal("request ID missing")
		}
		if result != nil {
			if err := json.Unmarshal(response.Body.Bytes(), result); err != nil {
				t.Fatal(err)
			}
		}
		if status >= 400 && !bytes.Contains(response.Body.Bytes(), []byte(`"error"`)) {
			t.Fatalf("non-JSON error: %s", response.Body)
		}
		return response
	}
	type account struct {
		ID    int64
		Token string
	}
	register := func(name string) account {
		t.Helper()
		var user app.User
		body := fmt.Sprintf(`{"email":%q,"password":"password123","name":%q}`, name+"@example.com", name)
		call("POST", "/api/v1/register", "", body, 201, &user)
		var login struct {
			Token string `json:"token"`
		}
		call("POST", "/api/v1/login", "", fmt.Sprintf(`{"email":%q,"password":"password123"}`, name+"@example.com"), 200, &login)
		return account{ID: user.ID, Token: login.Token}
	}
	owner, creator, assignee, admin, outsider, member := register("owner"), register("creator"), register("assignee"), register("admin"), register("outsider"), register("member")
	call("POST", "/api/v1/register", "", `{"email":"OWNER@example.com","password":"password123","name":"duplicate"}`, 409, nil)
	call("POST", "/api/v1/login", "", `{"email":"owner@example.com","password":"wrong"}`, 401, nil)
	call("POST", "/api/v1/login", "", `{"email":"missing@example.com","password":"password123"}`, 401, nil)
	call("GET", "/api/v1/teams", "", "", 401, nil)
	call("GET", "/api/v1/teams", "invalid", "", 401, nil)
	var team app.Team
	call("POST", "/api/v1/teams", owner.Token, `{"name":"Team A"}`, 201, &team)
	if team.Role != app.Owner || team.CreatedBy != owner.ID {
		t.Fatalf("wrong owner: %+v", team)
	}
	teamPath := fmt.Sprintf("/api/v1/teams/%d", team.ID)
	for _, person := range []account{creator, assignee, admin, member} {
		role := "member"
		if person.ID == admin.ID {
			role = "admin"
		}
		call("POST", teamPath+"/invite", owner.Token, fmt.Sprintf(`{"user_id":%d,"role":%q}`, person.ID, role), 204, nil)
	}
	call("POST", teamPath+"/invite", owner.Token, fmt.Sprintf(`{"user_id":%d}`, creator.ID), 409, nil)
	call("POST", teamPath+"/invite", admin.Token, fmt.Sprintf(`{"user_id":%d,"role":"admin"}`, outsider.ID), 403, nil)
	call("POST", teamPath+"/invite", member.Token, fmt.Sprintf(`{"user_id":%d}`, outsider.ID), 403, nil)
	call("POST", teamPath+"/invite", owner.Token, fmt.Sprintf(`{"user_id":%d,"role":"owner"}`, outsider.ID), 400, nil)
	call("PUT", fmt.Sprintf("%s/members/%d", teamPath, owner.ID), owner.Token, `{"role":"member"}`, 403, nil)
	call("PUT", fmt.Sprintf("%s/members/%d", teamPath, owner.ID), admin.Token, `{"role":"member"}`, 403, nil)
	call("PUT", fmt.Sprintf("%s/members/%d", teamPath, creator.ID), admin.Token, `{"role":"admin"}`, 403, nil)
	call("PUT", fmt.Sprintf("%s/members/%d", teamPath, member.ID), owner.Token, `{"role":"admin"}`, 204, nil)
	call("PUT", fmt.Sprintf("%s/members/%d", teamPath, member.ID), owner.Token, `{"role":"member"}`, 204, nil)
	var teams []app.Team
	call("GET", "/api/v1/teams", creator.Token, "", 200, &teams)
	if len(teams) != 1 || teams[0].ID != team.ID {
		t.Fatalf("wrong teams: %+v", teams)
	}
	call("GET", "/api/v1/teams", outsider.Token, "", 200, &teams)
	if len(teams) != 0 {
		t.Fatal("outsider received foreign team")
	}
	var members []app.TeamMember
	call("GET", teamPath+"/members", creator.Token, "", 200, &members)
	if len(members) != 5 {
		t.Fatalf("members=%d", len(members))
	}
	call("GET", teamPath+"/members", outsider.Token, "", 404, nil)
	var task app.Task
	call("POST", "/api/v1/tasks", creator.Token, fmt.Sprintf(`{"team_id":%d,"title":"Task","assignee_id":%d}`, team.ID, assignee.ID), 201, &task)
	if task.CreatedBy != creator.ID || task.Version != 1 {
		t.Fatalf("wrong task: %+v", task)
	}
	taskPath := fmt.Sprintf("/api/v1/tasks/%d", task.ID)
	call("GET", "/api/v1/tasks", creator.Token, "", 400, nil)
	call("GET", fmt.Sprintf("/api/v1/tasks?team_id=%d&limit=-1", team.ID), creator.Token, "", 400, nil)
	call("GET", fmt.Sprintf("/api/v1/tasks?team_id=%d", team.ID), outsider.Token, "", 404, nil)
	call("POST", "/api/v1/tasks", outsider.Token, fmt.Sprintf(`{"team_id":%d,"title":"Foreign"}`, team.ID), 404, nil)
	call("POST", "/api/v1/tasks", creator.Token, fmt.Sprintf(`{"team_id":%d,"title":"Bad assignee","assignee_id":%d}`, team.ID, outsider.ID), 400, nil)
	call("GET", taskPath, outsider.Token, "", 404, nil)
	call("PUT", taskPath, outsider.Token, `{"version":1,"status":"done"}`, 404, nil)
	call("PUT", taskPath, member.Token, `{"version":1,"status":"done"}`, 403, nil)
	call("PUT", taskPath, assignee.Token, `{"version":1,"title":"Not allowed"}`, 403, nil)
	call("PUT", taskPath, assignee.Token, `{"version":1,"assignee_id":null}`, 403, nil)
	call("PUT", taskPath, creator.Token, fmt.Sprintf(`{"version":1,"assignee_id":%d}`, outsider.ID), 400, nil)
	call("PUT", taskPath, creator.Token, `{"version":1,"created_by":1}`, 400, nil)
	call("PUT", taskPath, assignee.Token, `{"version":1,"status":"done"}`, 200, &task)
	if task.Version != 2 || task.ClosedAt == nil {
		t.Fatalf("close failed: %+v", task)
	}
	call("PUT", taskPath, creator.Token, `{"version":1,"title":"Stale"}`, 409, nil)
	call("PUT", taskPath, creator.Token, `{"version":2,"title":"Updated","assignee_id":null}`, 200, &task)
	if task.Title != "Updated" || task.AssigneeID != nil || task.Version != 3 {
		t.Fatalf("update failed: %+v", task)
	}
	call("PUT", taskPath, admin.Token, `{"version":3,"description":"Admin edit"}`, 200, &task)
	call("PUT", taskPath, owner.Token, `{"version":4,"status":"todo"}`, 200, &task)
	if task.ClosedAt != nil {
		t.Fatal("reopened task remains closed")
	}
	var comment app.Comment
	call("POST", taskPath+"/comments", member.Token, `{"content":"Comment"}`, 201, &comment)
	if comment.UserID != member.ID {
		t.Fatal("wrong comment author")
	}
	call("POST", taskPath+"/comments", outsider.Token, `{"content":"Foreign"}`, 404, nil)
	call("GET", taskPath+"/comments", outsider.Token, "", 404, nil)
	var comments []app.Comment
	call("GET", taskPath+"/comments", creator.Token, "", 200, &comments)
	if len(comments) != 1 {
		t.Fatalf("comments=%d", len(comments))
	}
	call("GET", taskPath+"/history", outsider.Token, "", 404, nil)
	var history []app.History
	call("GET", taskPath+"/history", member.Token, "", 200, &history)
	if len(history) != 5 {
		t.Fatalf("history=%d", len(history))
	}
	call("GET", teamPath+"/stats", outsider.Token, "", 404, nil)
	call("GET", teamPath+"/stats", creator.Token, "", 403, nil)
	call("GET", teamPath+"/stats", admin.Token, "", 200, nil)
	call("GET", teamPath+"/stats", owner.Token, "", 200, nil)
	call("GET", "/healthz", "", "", 200, nil)
}
