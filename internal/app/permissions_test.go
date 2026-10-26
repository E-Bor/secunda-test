package app

import "testing"

func TestTaskPermissions(t *testing.T) {
	assignee := int64(20)
	task := Task{CreatedBy: 10, AssigneeID: &assignee}
	status := InProgress
	title := "changed"
	description := "changed"
	tests := []struct {
		name   string
		role   Role
		userID int64
		input  UpdateTaskInput
		want   bool
	}{
		{"owner edits any task", Owner, 30, UpdateTaskInput{Title: &title, AssigneeID: OptionalID{Set: true}}, true},
		{"admin edits any task", Admin, 30, UpdateTaskInput{Description: &description}, true},
		{"creator edits title", Member, 10, UpdateTaskInput{Title: &title}, true},
		{"creator reassigns", Member, 10, UpdateTaskInput{AssigneeID: OptionalID{Set: true, Value: &assignee}}, true},
		{"creator unassigns", Member, 10, UpdateTaskInput{AssigneeID: OptionalID{Set: true}}, true},
		{"assignee changes status", Member, 20, UpdateTaskInput{Status: &status}, true},
		{"assignee cannot change title", Member, 20, UpdateTaskInput{Title: &title}, false},
		{"assignee cannot change description", Member, 20, UpdateTaskInput{Description: &description}, false},
		{"assignee cannot reassign", Member, 20, UpdateTaskInput{AssigneeID: OptionalID{Set: true, Value: &assignee}}, false},
		{"assignee cannot unassign", Member, 20, UpdateTaskInput{AssigneeID: OptionalID{Set: true}}, false},
		{"mixed update denied entirely", Member, 20, UpdateTaskInput{Status: &status, Title: &title}, false},
		{"unrelated member denied", Member, 30, UpdateTaskInput{Status: &status}, false},
		{"nonmember creator denied", "", 10, UpdateTaskInput{Title: &title}, false},
		{"unknown role denied", "superuser", 30, UpdateTaskInput{Status: &status}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanUpdateTask(tt.role, tt.userID, task, tt.input); got != tt.want {
				t.Fatalf("CanUpdateTask = %v, want %v", got, tt.want)
			}
		})
	}
	task.AssigneeID = nil
	if CanUpdateTask(Member, 20, task, UpdateTaskInput{Status: &status}) {
		t.Fatal("unassigned task was editable by an unrelated member")
	}
	task.AssigneeID = &task.CreatedBy
	if !CanUpdateTask(Member, 10, task, UpdateTaskInput{Title: &title}) {
		t.Fatal("creator must retain full editing rights when also assigned")
	}
}

func TestTeamPermissions(t *testing.T) {
	roles := []Role{Owner, Admin, Member, "", "unknown"}
	managers := map[Role]bool{Owner: true, Admin: true}
	invites := map[[2]Role]bool{
		{Owner, Admin}:  true,
		{Owner, Member}: true,
		{Admin, Member}: true,
	}
	changes := map[[3]Role]bool{
		{Owner, Admin, Admin}:   true,
		{Owner, Admin, Member}:  true,
		{Owner, Member, Admin}:  true,
		{Owner, Member, Member}: true,
	}
	for _, actor := range roles {
		if got := CanManage(actor); got != managers[actor] {
			t.Errorf("CanManage(%q) = %v", actor, got)
		}
		for _, target := range roles {
			if got := CanInvite(actor, target); got != invites[[2]Role{actor, target}] {
				t.Errorf("CanInvite(%q, %q) = %v", actor, target, got)
			}
			for _, next := range roles {
				if got := CanChangeRole(actor, target, next); got != changes[[3]Role{actor, target, next}] {
					t.Errorf("CanChangeRole(%q, %q, %q) = %v", actor, target, next, got)
				}
			}
		}
	}
}
