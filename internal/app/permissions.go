package app

func CanManage(role Role) bool {
	return role == Owner || role == Admin
}

func CanInvite(role, invitedRole Role) bool {
	return (role == Owner && (invitedRole == Admin || invitedRole == Member)) ||
		(role == Admin && invitedRole == Member)
}

func CanChangeRole(actor, current, next Role) bool {
	return actor == Owner && (current == Admin || current == Member) && (next == Admin || next == Member)
}

func CanUpdateTask(role Role, userID int64, task Task, input UpdateTaskInput) bool {
	if role != Owner && role != Admin && role != Member {
		return false
	}

	if CanManage(role) || task.CreatedBy == userID {
		return true
	}

	return task.AssigneeID != nil && *task.AssigneeID == userID &&
		input.Title == nil && input.Description == nil && !input.AssigneeID.Set
}
