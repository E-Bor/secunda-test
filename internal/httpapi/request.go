package httpapi

import (
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"secunda/internal/app"
)

func (h *handler) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		h.fail(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	data, err := io.ReadAll(r.Body)
	if err == nil {
		err = decodeJSON(data, target)
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.fail(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 64 KiB")
		return false
	}

	h.fail(w, r, http.StatusBadRequest, "invalid_input", "invalid JSON body or unknown fields")
	return false
}

func (h *handler) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		err = fmt.Errorf("%w: %s must be a positive integer", app.ErrInvalid, name)
		h.respond(w, r, 0, nil, err)
		return 0, false
	}

	return id, true
}

func queryInt(r *http.Request, name string, fallback, minValue, maxValue int64) (int64, error) {
	values, exists := r.URL.Query()[name]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("%w: duplicate %s", app.ErrInvalid, name)
	}

	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("%w: invalid %s", app.ErrInvalid, name)
	}

	return value, nil
}

func pagination(r *http.Request) (int, int, error) {
	limit, err := queryInt(r, "limit", 20, 1, 100)
	if err != nil {
		return 0, 0, err
	}

	offset, err := queryInt(r, "offset", 0, 0, 10000)
	return int(limit), int(offset), err
}

func taskFilter(r *http.Request) (app.TaskFilter, error) {
	teamID, err := queryInt(r, "team_id", 0, 1, math.MaxInt64)
	if err != nil || teamID == 0 {
		return app.TaskFilter{}, fmt.Errorf("%w: team_id is required and must be positive", app.ErrInvalid)
	}

	assigneeID, err := queryInt(r, "assignee_id", 0, 1, math.MaxInt64)
	if err != nil {
		return app.TaskFilter{}, err
	}

	limit, offset, err := pagination(r)
	if err != nil {
		return app.TaskFilter{}, err
	}

	return app.TaskFilter{
		TeamID:     teamID,
		Status:     app.Status(r.URL.Query().Get("status")),
		AssigneeID: assigneeID,
		Limit:      limit,
		Offset:     offset,
		Cursor:     r.URL.Query().Get("cursor"),
	}, nil
}

func validateQuery(raw string) error {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return errors.New("invalid query string")
	}

	for _, values := range query {
		if len(values) != 1 {
			return errors.New("duplicate query parameter")
		}
	}

	return nil
}
