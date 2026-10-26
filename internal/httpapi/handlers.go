package httpapi

import (
	"net/http"

	"secunda/internal/app"
)

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var input app.RegisterInput
	if !h.decode(w, r, &input) {
		return
	}

	user, err := h.service.Register(r.Context(), input)
	h.respond(w, r, http.StatusCreated, user, err)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !h.decode(w, r, &input) {
		return
	}

	token, err := h.service.Login(r.Context(), input.Email, input.Password)
	h.respond(w, r, http.StatusOK, map[string]string{"token": token, "token_type": "Bearer"}, err)
}

func (h *handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !h.decode(w, r, &input) {
		return
	}

	team, err := h.service.CreateTeam(r.Context(), userID(r), input.Name)
	h.respond(w, r, http.StatusCreated, team, err)
}

func (h *handler) listTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := h.service.ListTeams(r.Context(), userID(r))
	h.respond(w, r, http.StatusOK, teams, err)
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	var input struct {
		UserID int64    `json:"user_id"`
		Role   app.Role `json:"role"`
	}
	if !h.decode(w, r, &input) {
		return
	}

	err := h.service.Invite(r.Context(), userID(r), id, input.UserID, input.Role)
	h.respond(w, r, http.StatusNoContent, nil, err)
}

func (h *handler) listMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	members, err := h.service.ListMembers(r.Context(), userID(r), id)
	h.respond(w, r, http.StatusOK, members, err)
}

func (h *handler) changeRole(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	memberID, ok := h.pathID(w, r, "user_id")
	if !ok {
		return
	}

	var input struct {
		Role app.Role `json:"role"`
	}
	if !h.decode(w, r, &input) {
		return
	}

	err := h.service.ChangeRole(r.Context(), userID(r), id, memberID, input.Role)
	h.respond(w, r, http.StatusNoContent, nil, err)
}

func (h *handler) stats(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	stats, err := h.service.TeamStats(r.Context(), userID(r), id)
	h.respond(w, r, http.StatusOK, stats, err)
}

func (h *handler) createTask(w http.ResponseWriter, r *http.Request) {
	var input app.CreateTaskInput
	if !h.decode(w, r, &input) {
		return
	}

	task, err := h.service.CreateTask(r.Context(), userID(r), input)
	h.respond(w, r, http.StatusCreated, task, err)
}

func (h *handler) listTasks(w http.ResponseWriter, r *http.Request) {
	filter, err := taskFilter(r)
	if err != nil {
		h.respond(w, r, 0, nil, err)
		return
	}

	page, err := h.service.ListTasks(r.Context(), userID(r), filter)
	h.respond(w, r, http.StatusOK, page, err)
}

func (h *handler) getTask(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	task, err := h.service.GetTask(r.Context(), userID(r), id)
	h.respond(w, r, http.StatusOK, task, err)
}

func (h *handler) updateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	var input app.UpdateTaskInput
	if !h.decode(w, r, &input) {
		return
	}

	task, err := h.service.UpdateTask(r.Context(), userID(r), id, input)
	h.respond(w, r, http.StatusOK, task, err)
}

func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	limit, offset, err := pagination(r)
	if err != nil {
		h.respond(w, r, 0, nil, err)
		return
	}

	items, err := h.service.ListHistory(r.Context(), userID(r), id, limit, offset)
	h.respond(w, r, http.StatusOK, items, err)
}

func (h *handler) addComment(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	var input struct {
		Content string `json:"content"`
	}
	if !h.decode(w, r, &input) {
		return
	}

	comment, err := h.service.AddComment(r.Context(), userID(r), id, input.Content)
	h.respond(w, r, http.StatusCreated, comment, err)
}

func (h *handler) comments(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	limit, offset, err := pagination(r)
	if err != nil {
		h.respond(w, r, 0, nil, err)
		return
	}

	items, err := h.service.ListComments(r.Context(), userID(r), id, limit, offset)
	h.respond(w, r, http.StatusOK, items, err)
}
