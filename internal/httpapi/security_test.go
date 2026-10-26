package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"secunda/internal/app"
)

func TestAmbiguousQueryRejected(t *testing.T) {
	h := &handler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	for _, query := range []string{
		"team_id=1&team_id=2",
		"status=todo&status=done",
		"cursor=a&cursor=b",
		"team_id=1;team_id=2",
		"team_id=1&status=%zz",
	} {
		t.Run(query, func(t *testing.T) {
			called := false
			next := h.observe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			w := httptest.NewRecorder()
			next.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/tasks?"+query, nil))
			if w.Code != 400 || called {
				t.Fatalf("ambiguous query reached handler: status=%d called=%v", w.Code, called)
			}
		})
	}
}

func TestAuthorizationHeaders(t *testing.T) {
	auth, err := app.NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{auth: auth, apiLimiter: newLimiter(100), logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	for _, tt := range []struct {
		name   string
		modify func(*http.Request)
	}{
		{"two authorization headers", func(r *http.Request) {
			r.Header.Add("Authorization", "Bearer "+token)
			r.Header.Add("Authorization", "Bearer invalid")
		}},
		{"cookie is not authentication", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "token", Value: token}) }},
		{"query is not authentication", func(r *http.Request) { r.URL.RawQuery = "token=" + token }},
		{"oversized token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 4097)) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := h.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			r := httptest.NewRequest("GET", "/api/v1/tasks?team_id=1", nil)
			tt.modify(r)
			w := httptest.NewRecorder()
			next.ServeHTTP(w, r)
			if w.Code != 401 || called {
				t.Fatalf("invalid authentication accepted: status=%d called=%v", w.Code, called)
			}
		})
	}
}

func TestSQLIdentifiersAndMassAssignmentRejected(t *testing.T) {
	auth, err := app.NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, auth, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)), 1000, 1000)
	for _, tt := range []struct{ method, path, body string }{
		{"GET", "/api/v1/tasks?team_id=" + url.QueryEscape("1 OR 1=1"), ""},
		{"GET", "/api/v1/tasks?team_id=1&assignee_id=" + url.QueryEscape("1; DROP TABLE users"), ""},
		{"GET", "/api/v1/tasks/" + url.PathEscape("1 OR 1=1"), ""},
		{"POST", "/api/v1/tasks", `{"team_id":"1 OR 1=1","title":"Task"}`},
		{"POST", "/api/v1/tasks", `{"team_id":1,"title":"Task","created_by":2}`},
		{"PUT", "/api/v1/tasks/1", `{"version":1,"title":"Task","team_id":2}`},
		{"PUT", "/api/v1/tasks/1", `{"version":1,"status":"done","closed_at":"2000-01-01T00:00:00Z"}`},
		{"POST", "/api/v1/register", `{"email":"x@example.com","password":"password123","name":"X","role":"owner"}`},
	} {
		t.Run(tt.method+" "+tt.path+" "+tt.body, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatalf("unsafe input reached service: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestJSONResponseEscapesHTML(t *testing.T) {
	h := &handler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	request := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.respond(w, request, 200, map[string]string{"content": `<script>alert("xss")</script>`}, nil)
	if strings.Contains(w.Body.String(), "<script>") || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unsafe response: %s", w.Body)
	}
}

func TestAuthenticationBusyResponse(t *testing.T) {
	h := &handler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	w := httptest.NewRecorder()
	h.respond(w, httptest.NewRequest("POST", "/api/v1/login", nil), 0, nil, app.ErrBusy)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response: %d %v", w.Code, w.Header())
	}
}

func FuzzDecodeJSON(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"version":1,"assignee_id":null}`, `{"title":"a","title":"b"}`, `{"status":null}`, `{"title":"тест"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		var input app.UpdateTaskInput
		_ = decodeJSON([]byte(data), &input)
	})
}
