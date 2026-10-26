package app

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("invalid credentials")
	ErrForbidden    = errors.New("access denied")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrBusy         = errors.New("authentication capacity exceeded")
)

type Role string

const (
	Owner  Role = "owner"
	Admin  Role = "admin"
	Member Role = "member"
)

type Status string

const (
	Todo       Status = "todo"
	InProgress Status = "in_progress"
	Done       Status = "done"
)

func (s Status) Valid() bool {
	return s == Todo || s == InProgress || s == Done
}

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Role      Role      `json:"role"`
}

type TeamMember struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
}

type Task struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	CreatedBy   int64      `json:"created_by"`
	AssigneeID  *int64     `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     int64      `json:"version"`
}

type CreateTaskInput struct {
	TeamID      int64  `json:"team_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	AssigneeID  *int64 `json:"assignee_id"`
}

type OptionalID struct {
	Set   bool
	Value *int64
}

func (o *OptionalID) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

type UpdateTaskInput struct {
	Version     int64      `json:"version"`
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	Status      *Status    `json:"status"`
	AssigneeID  OptionalID `json:"assignee_id"`
}

type TaskFilter struct {
	TeamID     int64  `json:"team_id"`
	Status     Status `json:"status"`
	AssigneeID int64  `json:"assignee_id"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	Cursor     string `json:"cursor"`
}

type TaskPage struct {
	Items      []Task `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type History struct {
	ID        int64           `json:"id"`
	TaskID    int64           `json:"task_id"`
	ChangedBy int64           `json:"changed_by"`
	Changes   json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Performer struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int64  `json:"closed_tasks"`
}

type TaskComments struct {
	TaskID int64 `json:"task_id"`
	Count  int64 `json:"count"`
}

type Stats struct {
	TeamID              int64            `json:"team_id"`
	ByStatus            map[Status]int64 `json:"by_status"`
	TopAssignees        []Performer      `json:"top_assignees"`
	AverageCloseSeconds *float64         `json:"average_close_seconds"`
	CommentsTotal       int64            `json:"comments_total"`
	CommentsByTask      []TaskComments   `json:"comments_by_task"`
}
