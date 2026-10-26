package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func (s *Service) ListHistory(ctx context.Context, userID, taskID int64, limit, offset int) ([]History, error) {
	if !validPagination(limit, offset) {
		return nil, fmt.Errorf("%w: invalid pagination", ErrInvalid)
	}
	if _, err := s.GetTask(ctx, userID, taskID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, changed_by, changes, created_at FROM task_history
		WHERE task_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]History, 0)
	for rows.Next() {
		var item History
		if err := rows.Scan(&item.ID, &item.TaskID, &item.ChangedBy, &item.Changes, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func taskValues(task Task) map[string]any {
	return map[string]any{
		"team_id":     task.TeamID,
		"title":       task.Title,
		"description": task.Description,
		"status":      task.Status,
		"created_by":  task.CreatedBy,
		"assignee_id": task.AssigneeID,
		"created_at":  task.CreatedAt,
		"updated_at":  task.UpdatedAt,
		"closed_at":   task.ClosedAt,
		"version":     task.Version,
	}
}

func writeHistory(ctx context.Context, tx *sql.Tx, userID int64, before *Task, after Task) error {
	oldValues := make(map[string]any)
	if before != nil {
		oldValues = taskValues(*before)
	}

	changes := make(map[string]Change)
	for field, value := range taskValues(after) {
		if before == nil || !reflect.DeepEqual(oldValues[field], value) {
			changes[field] = Change{Old: oldValues[field], New: value}
		}
	}

	data, err := json.Marshal(changes)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?)`,
		after.ID, userID, data, after.UpdatedAt)
	return dbError(err)
}

func (s *Service) AddComment(ctx context.Context, userID, taskID int64, content string) (Comment, error) {
	content = strings.TrimSpace(content)
	if !validText(content, 1, 10000) {
		return Comment{}, fmt.Errorf("%w: content must contain 1–10000 characters", ErrInvalid)
	}
	if _, err := s.GetTask(ctx, userID, taskID); err != nil {
		return Comment{}, err
	}

	comment := Comment{
		TaskID:    taskID,
		UserID:    userID,
		Content:   content,
		CreatedAt: nowUTC(),
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, ?, ?)`,
		taskID, userID, content, comment.CreatedAt)
	if err != nil {
		return Comment{}, dbError(err)
	}
	comment.ID, err = result.LastInsertId()
	return comment, err
}

func (s *Service) ListComments(ctx context.Context, userID, taskID int64, limit, offset int) ([]Comment, error) {
	if !validPagination(limit, offset) {
		return nil, fmt.Errorf("%w: invalid pagination", ErrInvalid)
	}
	if _, err := s.GetTask(ctx, userID, taskID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, user_id, content, created_at FROM task_comments
		WHERE task_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Comment, 0)
	for rows.Next() {
		var item Comment
		if err := rows.Scan(&item.ID, &item.TaskID, &item.UserID, &item.Content, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
