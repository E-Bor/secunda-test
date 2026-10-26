package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"secunda/internal/app"
)

func TestAuthRateLimit(t *testing.T) {
	auth, err := app.NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, auth, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)), 1, 1)
	for i, status := range []int{400, 429} {
		r := httptest.NewRequest("POST", "/api/v1/login", strings.NewReader("invalid"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status=%d, want %d", w.Code, status)
		}
		if status == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("Retry-After missing")
		}
	}
}

func TestDecode(t *testing.T) {
	h := &handler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	for _, tt := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"valid", `{"name":"test"}`, "application/json; charset=utf-8", 200},
		{"empty", "", "application/json", 400},
		{"unknown field", `{"role":"owner"}`, "application/json", 400},
		{"multiple values", `{"name":"test"}{}`, "application/json", 400},
		{"malformed", `{"name":`, "application/json", 400},
		{"wrong content type", `{"name":"test"}`, "text/plain", 415},
		{"large", `{"name":"` + strings.Repeat("x", 65536) + `"}`, "application/json", 413},
		{"large trailing whitespace", `{"name":"test"}` + strings.Repeat(" ", 65536), "application/json", 413},
		{"duplicate field", `{"name":"first","name":"second"}`, "application/json", 400},
		{"escaped duplicate field", `{"name":"first","na\u006de":"second"}`, "application/json", 400},
		{"case alias", `{"name":"first","NAME":"second"}`, "application/json", 400},
		{"null object", `null`, "application/json", 400},
		{"null field", `{"name":null}`, "application/json", 400},
		{"invalid UTF-8", "{\"name\":\"\xff\"}", "application/json", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			var input struct {
				Name string `json:"name"`
			}
			ok := h.decode(w, r, &input)
			if ok != (tt.status == 200) || w.Code != tt.status {
				t.Fatalf("ok=%v status=%d want=%d", ok, w.Code, tt.status)
			}
		})
	}
}

func TestRecovery(t *testing.T) {
	h := &handler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	wrapped := h.observe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("injected failure") }))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "internal_error") || strings.Contains(w.Body.String(), "injected failure") {
		t.Fatalf("unsafe panic response: %d %s", w.Code, w.Body)
	}
}
