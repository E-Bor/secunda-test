package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type taskCursor struct {
	TeamID     int64  `json:"team"`
	Status     Status `json:"status"`
	AssigneeID int64  `json:"assignee"`
	After      int64  `json:"after"`
}

func validPagination(limit, offset int) bool {
	return limit >= 1 && limit <= 100 && offset >= 0 && offset <= 10000
}

func signData(secret []byte, purpose string, data []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(purpose))
	mac.Write(data)

	return mac.Sum(nil)
}

func (s *Service) encodeCursor(filter TaskFilter, after int64) string {
	cursor := taskCursor{
		TeamID:     filter.TeamID,
		Status:     filter.Status,
		AssigneeID: filter.AssigneeID,
		After:      after,
	}
	data, _ := json.Marshal(cursor)
	signature := signData(s.auth.secret, "task-cursor:", data)

	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (s *Service) decodeCursor(filter TaskFilter) (int64, error) {
	if filter.Cursor == "" {
		return 0, nil
	}

	invalid := fmt.Errorf("%w: invalid cursor or mismatched filters", ErrInvalid)
	parts := strings.Split(filter.Cursor, ".")
	if len(parts) != 2 || len(filter.Cursor) > 1024 {
		return 0, invalid
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, invalid
	}
	if !hmac.Equal(signature, signData(s.auth.secret, "task-cursor:", data)) {
		return 0, invalid
	}

	var cursor taskCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.After <= 0 || cursor.TeamID != filter.TeamID ||
		cursor.Status != filter.Status || cursor.AssigneeID != filter.AssigneeID {
		return 0, invalid
	}

	return cursor.After, nil
}

func (s *Service) ListTasks(ctx context.Context, userID int64, filter TaskFilter) (TaskPage, error) {
	if filter.TeamID <= 0 || (filter.Status != "" && !filter.Status.Valid()) || filter.AssigneeID < 0 ||
		!validPagination(filter.Limit, filter.Offset) || (filter.Offset != 0 && filter.Cursor != "") {
		return TaskPage{}, fmt.Errorf("%w: invalid filters or pagination", ErrInvalid)
	}

	after, err := s.decodeCursor(filter)
	if err != nil {
		return TaskPage{}, err
	}
	_, revision, err := teamAccess(ctx, s.db, userID, filter.TeamID)
	if err != nil {
		return TaskPage{}, err
	}

	key := s.taskCacheKey(filter, revision)
	var page TaskPage
	if data, ok := s.cache.Get(ctx, key); ok && json.Unmarshal(data, &page) == nil {
		return page, nil
	}

	page.Items, err = s.queryTasks(ctx, filter, after)
	if err != nil {
		return TaskPage{}, err
	}
	if len(page.Items) > filter.Limit {
		page.Items = page.Items[:filter.Limit]
		page.NextCursor = s.encodeCursor(filter, page.Items[len(page.Items)-1].ID)
	}

	if data, err := json.Marshal(page); err == nil {
		s.cache.Set(ctx, key, data)
	}

	return page, nil
}

func (s *Service) taskCacheKey(filter TaskFilter, revision int64) string {
	data, _ := json.Marshal(filter)
	signature := signData(s.auth.secret, "task-list-cache:", data)

	return fmt.Sprintf("tasks:%d:%d:%x", filter.TeamID, revision, signature)
}

func taskListQuery(filter TaskFilter, after int64) (string, []any) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE team_id = ?`
	args := []any{filter.TeamID}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.AssigneeID != 0 {
		query += ` AND assignee_id = ?`
		args = append(args, filter.AssigneeID)
	}
	if after != 0 {
		query += ` AND id < ?`
		args = append(args, after)
	}

	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit+1, filter.Offset)

	return query, args
}

func (s *Service) queryTasks(ctx context.Context, filter TaskFilter, after int64) ([]Task, error) {
	query, args := taskListQuery(filter, after)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0, filter.Limit+1)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}
