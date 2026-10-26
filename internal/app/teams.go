package app

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) CreateTeam(ctx context.Context, userID int64, name string) (Team, error) {
	name = strings.TrimSpace(name)
	if !validText(name, 1, 100) {
		return Team{}, fmt.Errorf("%w: name must contain 1–100 characters", ErrInvalid)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Team{}, err
	}
	defer tx.Rollback()

	team := Team{
		Name:      name,
		CreatedBy: userID,
		CreatedAt: nowUTC(),
		Role:      Owner,
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO teams (name, created_by, created_at) VALUES (?, ?, ?)`, name, userID, team.CreatedAt)
	if err != nil {
		return Team{}, dbError(err)
	}
	team.ID, err = result.LastInsertId()
	if err != nil {
		return Team{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'owner')`, team.ID, userID); err != nil {
		return Team{}, dbError(err)
	}

	return team, tx.Commit()
}

func (s *Service) ListTeams(ctx context.Context, userID int64) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.created_by, t.created_at, m.role
		FROM teams t JOIN team_members m ON m.team_id = t.id WHERE m.user_id = ? ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := make([]Team, 0)
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.ID, &team.Name, &team.CreatedBy, &team.CreatedAt, &team.Role); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}

	return teams, rows.Err()
}

func (s *Service) Invite(ctx context.Context, actorID, teamID, userID int64, invitedRole Role) error {
	if invitedRole == "" {
		invitedRole = Member
	}
	if userID <= 0 || (invitedRole != Admin && invitedRole != Member) {
		return fmt.Errorf("%w: positive user_id and role admin or member required", ErrInvalid)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	role, err := lockMemberRole(ctx, tx, actorID, teamID)
	if err != nil {
		return err
	}
	if !CanInvite(role, invitedRole) {
		return ErrForbidden
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, teamID, userID, invitedRole); err != nil {
		return dbError(err)
	}

	return tx.Commit()
}

func (s *Service) ChangeRole(ctx context.Context, actorID, teamID, userID int64, next Role) error {
	if next != Admin && next != Member {
		return fmt.Errorf("%w: role must be admin or member", ErrInvalid)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	role, _, err := teamAccess(ctx, tx, actorID, teamID)
	if err != nil {
		return err
	}
	if role != Owner {
		return ErrForbidden
	}

	var current Role
	if err := tx.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, userID).Scan(&current); err != nil {
		return dbError(err)
	}
	if !CanChangeRole(role, current, next) {
		return ErrForbidden
	}

	if _, err := tx.ExecContext(ctx, `UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?`, next, teamID, userID); err != nil {
		return dbError(err)
	}

	return tx.Commit()
}

func (s *Service) ListMembers(ctx context.Context, userID, teamID int64) ([]TeamMember, error) {
	if _, _, err := teamAccess(ctx, s.db, userID, teamID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT m.user_id, u.name, m.role FROM team_members m
		JOIN users u ON u.id = m.user_id WHERE m.team_id = ? ORDER BY m.user_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]TeamMember, 0)
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(&member.UserID, &member.Name, &member.Role); err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	return members, rows.Err()
}
