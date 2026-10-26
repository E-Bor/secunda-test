package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"secunda/internal/app"
)

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *handler) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	var response errorResponse
	response.Error.Code = code
	response.Error.Message = message
	h.writeJSON(w, r, status, response)
}

func (h *handler) respond(w http.ResponseWriter, r *http.Request, status int, data any, err error) {
	if err != nil {
		status, code, message := errorDetails(err)
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		if status >= 500 {
			h.logger.Error("request failed", "request_id", r.Context().Value(requestKey), "error", err)
		}
		h.fail(w, r, status, code, message)
		return
	}

	h.writeJSON(w, r, status, data)
}

func errorDetails(err error) (int, string, string) {
	switch {
	case errors.Is(err, app.ErrInvalid):
		return http.StatusBadRequest, "invalid_input", err.Error()
	case errors.Is(err, app.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized", "invalid credentials"
	case errors.Is(err, app.ErrForbidden):
		return http.StatusForbidden, "forbidden", "access denied"
	case errors.Is(err, app.ErrNotFound):
		return http.StatusNotFound, "not_found", "resource not found"
	case errors.Is(err, app.ErrConflict):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, app.ErrBusy):
		return http.StatusServiceUnavailable, "busy", "authentication is busy; retry later"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "request timed out"
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}

func (h *handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Warn("write response", "request_id", r.Context().Value(requestKey), "error", err)
	}
}
