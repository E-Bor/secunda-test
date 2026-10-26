package app

import (
	"context"
	"database/sql"
	_ "embed"
	"sort"
)

//go:embed stats.sql
var statsQuery string

func (s *Service) TeamStats(ctx context.Context, userID, teamID int64) (Stats, error) {
	role, _, err := teamAccess(ctx, s.db, userID, teamID)
	if err != nil {
		return Stats{}, err
	}
	if !CanManage(role) {
		return Stats{}, ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, statsQuery, teamID)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()

	stats := Stats{
		TeamID:         teamID,
		ByStatus:       map[Status]int64{Todo: 0, InProgress: 0, Done: 0},
		TopAssignees:   make([]Performer, 0),
		CommentsByTask: make([]TaskComments, 0),
	}
	for rows.Next() {
		if err := stats.scanRow(rows); err != nil {
			return Stats{}, err
		}
	}

	sort.Slice(stats.TopAssignees, func(i, j int) bool {
		a, b := stats.TopAssignees[i], stats.TopAssignees[j]
		if a.ClosedTasks != b.ClosedTasks {
			return a.ClosedTasks > b.ClosedTasks
		}
		return a.UserID < b.UserID
	})
	sort.Slice(stats.CommentsByTask, func(i, j int) bool {
		return stats.CommentsByTask[i].TaskID < stats.CommentsByTask[j].TaskID
	})

	return stats, rows.Err()
}

func (stats *Stats) scanRow(row scanner) error {
	var kind string
	var label sql.NullString
	var id sql.NullInt64
	var amount int64
	var average sql.NullFloat64
	if err := row.Scan(&kind, &label, &id, &amount, &average); err != nil {
		return err
	}

	switch kind {
	case "status":
		stats.ByStatus[Status(label.String)] = amount
	case "assignee":
		stats.TopAssignees = append(stats.TopAssignees, Performer{
			UserID:      id.Int64,
			Name:        label.String,
			ClosedTasks: amount,
		})
	case "comments":
		stats.CommentsByTask = append(stats.CommentsByTask, TaskComments{TaskID: id.Int64, Count: amount})
	case "summary":
		stats.CommentsTotal = amount
		if average.Valid {
			stats.AverageCloseSeconds = &average.Float64
		}
	}

	return nil
}
