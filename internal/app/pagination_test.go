package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCursor(t *testing.T) {
	s := &Service{auth: &Auth{secret: []byte(strings.Repeat("x", 32))}}
	f := TaskFilter{TeamID: 5, Status: Todo, AssigneeID: 8}
	f.Cursor = s.encodeCursor(f, 123)
	if after, err := s.decodeCursor(f); err != nil || after != 123 {
		t.Fatalf("decodeCursor = %d, %v", after, err)
	}
	for _, modify := range []func(*TaskFilter){
		func(f *TaskFilter) { f.TeamID++ },
		func(f *TaskFilter) { f.Status = Done },
		func(f *TaskFilter) { f.AssigneeID++ },
		func(f *TaskFilter) { f.Cursor = "invalid" },
		func(f *TaskFilter) { f.Cursor = strings.Repeat("a", 1025) },
		func(f *TaskFilter) { f.Cursor = "a" + f.Cursor[1:] },
	} {
		changed := f
		modify(&changed)
		if _, err := s.decodeCursor(changed); err == nil {
			t.Errorf("accepted invalid cursor: %+v", changed)
		}
	}
}

func TestOptionalAssignee(t *testing.T) {
	for _, tt := range []struct {
		body string
		set  bool
		id   *int64
	}{
		{`{"version":1}`, false, nil},
		{`{"version":1,"assignee_id":null}`, true, nil},
		{`{"version":1,"assignee_id":42}`, true, new(int64(42))},
	} {
		var input UpdateTaskInput
		if err := json.Unmarshal([]byte(tt.body), &input); err != nil {
			t.Fatal(err)
		}
		if input.AssigneeID.Set != tt.set || (input.AssigneeID.Value == nil) != (tt.id == nil) {
			t.Fatalf("unexpected assignment for %s: %+v", tt.body, input.AssigneeID)
		}
		if tt.id != nil && *input.AssigneeID.Value != *tt.id {
			t.Fatal("wrong assignee")
		}
	}
}
