package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const taskColumns = `id, team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version`

func scanTask(row scanner) (Task, error) {
	var task Task
	err := row.Scan(&task.ID, &task.TeamID, &task.Title, &task.Description, &task.Status, &task.CreatedBy,
		&task.AssigneeID, &task.CreatedAt, &task.UpdatedAt, &task.ClosedAt, &task.Version)

	return task, dbError(err)
}

func checkAssignee(ctx context.Context, tx *sql.Tx, teamID int64, assigneeID *int64) error {
	if assigneeID == nil {
		return nil
	}

	if *assigneeID <= 0 {
		return fmt.Errorf("%w: assignee_id must be positive", ErrInvalid)
	}

	var id int64
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM team_members WHERE team_id = ? AND user_id = ? FOR SHARE`, teamID, *assigneeID).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: assignee must belong to the team", ErrInvalid)
	}

	return err
}

func bumpRevision(ctx context.Context, tx *sql.Tx, teamID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE teams SET tasks_revision = tasks_revision + 1 WHERE id = ?`, teamID)
	return dbError(err)
}

func (s *Service) CreateTask(ctx context.Context, userID int64, input CreateTaskInput) (Task, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Status == "" {
		input.Status = Todo
	}

	if input.TeamID <= 0 || !validText(input.Title, 1, 200) || !validText(input.Description, 0, 10000) || !input.Status.Valid() {
		return Task{}, fmt.Errorf("%w: valid team_id, title (1–200), description (0–10000) and status required", ErrInvalid)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	if _, err := lockMemberRole(ctx, tx, userID, input.TeamID); err != nil {
		return Task{}, err
	}
	if err := checkAssignee(ctx, tx, input.TeamID, input.AssigneeID); err != nil {
		return Task{}, err
	}
	if err := bumpRevision(ctx, tx, input.TeamID); err != nil {
		return Task{}, err
	}

	now := nowUTC()
	task := Task{
		TeamID:      input.TeamID,
		Title:       input.Title,
		Description: input.Description,
		Status:      input.Status,
		CreatedBy:   userID,
		AssigneeID:  input.AssigneeID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
	}
	if task.Status == Done {
		task.ClosedAt = &now
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO tasks
		(team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.TeamID, task.Title, task.Description, task.Status,
		task.CreatedBy, task.AssigneeID, task.CreatedAt, task.UpdatedAt, task.ClosedAt, task.Version)
	if err != nil {
		return Task{}, dbError(err)
	}
	task.ID, err = result.LastInsertId()
	if err != nil {
		return Task{}, err
	}
	if err := writeHistory(ctx, tx, userID, nil, task); err != nil {
		return Task{}, err
	}

	return task, dbError(tx.Commit())
}

func (s *Service) UpdateTask(ctx context.Context, userID, taskID int64, input UpdateTaskInput) (Task, error) {
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		input.Title = &title
	}
	if err := validateTaskUpdate(input); err != nil {
		return Task{}, err
	}

	visible, err := s.authorizeTaskUpdate(ctx, userID, taskID, input)
	if err != nil {
		return Task{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	before, err := lockTaskForUpdate(ctx, tx, userID, taskID, visible, input)
	if err != nil {
		return Task{}, err
	}
	if input.AssigneeID.Set {
		if err := checkAssignee(ctx, tx, before.TeamID, input.AssigneeID.Value); err != nil {
			return Task{}, err
		}
	}

	after, changed := applyTaskUpdate(before, input)
	if !changed {
		return before, nil
	}

	if err := bumpRevision(ctx, tx, before.TeamID); err != nil {
		return Task{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET title = ?, description = ?, status = ?, assignee_id = ?,
		updated_at = ?, closed_at = ?, version = ? WHERE id = ? AND version = ?`, after.Title, after.Description,
		after.Status, after.AssigneeID, after.UpdatedAt, after.ClosedAt, after.Version, taskID, before.Version)
	if err != nil {
		return Task{}, dbError(err)
	}
	if err := writeHistory(ctx, tx, userID, &before, after); err != nil {
		return Task{}, err
	}

	return after, dbError(tx.Commit())
}

func (s *Service) GetTask(ctx context.Context, userID, taskID int64) (Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE id = ? AND team_id IN (SELECT team_id FROM team_members WHERE user_id = ?)`, taskID, userID))
}
