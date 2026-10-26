package httpapi

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"secunda/docs"
	"secunda/internal/app"
)

type handler struct {
	service     *app.Service
	auth        *app.Auth
	db          *sql.DB
	logger      *slog.Logger
	apiLimiter  *limiter
	authLimiter *limiter
}

func New(service *app.Service, auth *app.Auth, db *sql.DB, logger *slog.Logger, rpm, authRPM int) http.Handler {
	h := &handler{
		service:     service,
		auth:        auth,
		db:          db,
		logger:      logger,
		apiLimiter:  newLimiter(rpm),
		authLimiter: newLimiter(authRPM),
	}

	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/teams", h.createTeam)
	api.HandleFunc("GET /api/v1/teams", h.listTeams)
	api.HandleFunc("POST /api/v1/teams/{id}/invite", h.invite)
	api.HandleFunc("GET /api/v1/teams/{id}/members", h.listMembers)
	api.HandleFunc("PUT /api/v1/teams/{id}/members/{user_id}", h.changeRole)
	api.HandleFunc("GET /api/v1/teams/{id}/stats", h.stats)
	api.HandleFunc("POST /api/v1/tasks", h.createTask)
	api.HandleFunc("GET /api/v1/tasks", h.listTasks)
	api.HandleFunc("GET /api/v1/tasks/{id}", h.getTask)
	api.HandleFunc("PUT /api/v1/tasks/{id}", h.updateTask)
	api.HandleFunc("GET /api/v1/tasks/{id}/history", h.history)
	api.HandleFunc("POST /api/v1/tasks/{id}/comments", h.addComment)
	api.HandleFunc("GET /api/v1/tasks/{id}/comments", h.comments)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/register", h.limitAuth(h.register))
	mux.HandleFunc("POST /api/v1/login", h.limitAuth(h.login))
	mux.Handle("/api/v1/", h.authenticate(api))
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(docs.OpenAPI)
	})

	return h.observe(mux)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		h.fail(w, r, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return
	}
	h.respond(w, r, http.StatusOK, map[string]string{"status": "ok"}, nil)
}
