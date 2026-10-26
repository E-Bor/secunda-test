package app

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
)

func validateTaskUpdate(input UpdateTaskInput) error {
	if input.Version <= 0 || (input.Title == nil && input.Description == nil && input.Status == nil && !input.AssigneeID.Set) {
		return fmt.Errorf("%w: version and at least one changed field required", ErrInvalid)
	}
	if input.Title != nil && !validText(*input.Title, 1, 200) {
		return fmt.Errorf("%w: title must contain 1–200 characters", ErrInvalid)
	}
	if input.Description != nil && !validText(*input.Description, 0, 10000) {
		return fmt.Errorf("%w: description must contain at most 10000 characters", ErrInvalid)
	}
	if input.Status != nil && !input.Status.Valid() {
		return fmt.Errorf("%w: unknown status", ErrInvalid)
	}

	return nil
}

func (s *Service) authorizeTaskUpdate(ctx context.Context, userID, taskID int64, input UpdateTaskInput) (Task, error) {
	var task Task
	var role Role
	err := s.db.QueryRowContext(ctx, `SELECT t.team_id, t.created_by, t.assignee_id, m.role
		FROM tasks t JOIN team_members m ON m.team_id = t.team_id
		WHERE t.id = ? AND m.user_id = ?`, taskID, userID).
		Scan(&task.TeamID, &task.CreatedBy, &task.AssigneeID, &role)
	if err != nil {
		return Task{}, dbError(err)
	}
	if !CanUpdateTask(role, userID, task, input) {
		return Task{}, ErrForbidden
	}

	return task, nil
}

func lockTaskForUpdate(ctx context.Context, tx *sql.Tx, userID, taskID int64, visible Task, input UpdateTaskInput) (Task, error) {
	role, err := lockMemberRole(ctx, tx, userID, visible.TeamID)
	if err != nil {
		return Task{}, err
	}
	if !CanUpdateTask(role, userID, visible, input) {
		return Task{}, ErrForbidden
	}

	task, err := scanTask(tx.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? AND team_id = ? FOR UPDATE`, taskID, visible.TeamID))
	if err != nil {
		return Task{}, err
	}
	if !CanUpdateTask(role, userID, task, input) {
		return Task{}, ErrForbidden
	}
	if task.Version != input.Version {
		return Task{}, fmt.Errorf("%w: task version has changed; reload the task", ErrConflict)
	}

	return task, nil
}

func applyTaskUpdate(before Task, input UpdateTaskInput) (Task, bool) {
	after := before
	if input.Title != nil {
		after.Title = *input.Title
	}
	if input.Description != nil {
		after.Description = *input.Description
	}
	if input.Status != nil {
		after.Status = *input.Status
	}
	if input.AssigneeID.Set {
		after.AssigneeID = input.AssigneeID.Value
	}
	if reflect.DeepEqual(before, after) {
		return before, false
	}

	after.UpdatedAt = nowUTC()
	after.Version++
	if before.Status != Done && after.Status == Done {
		after.ClosedAt = &after.UpdatedAt
	} else if after.Status != Done {
		after.ClosedAt = nil
	}

	return after, true
}
