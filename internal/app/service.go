package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"secunda/internal/cache"
)

type Service struct {
	db    *sql.DB
	cache *cache.Cache
	auth  *Auth
}

func New(db *sql.DB, cache *cache.Cache, auth *Auth) *Service {
	return &Service{db: db, cache: cache, auth: auth}
}

type scanner interface {
	Scan(...any) error
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func teamAccess(ctx context.Context, db queryer, userID, teamID int64) (Role, int64, error) {
	var role Role
	var revision int64
	err := db.QueryRowContext(ctx, `SELECT m.role, t.tasks_revision
		FROM teams t JOIN team_members m ON m.team_id = t.id
		WHERE t.id = ? AND m.user_id = ?`, teamID, userID).Scan(&role, &revision)

	return role, revision, dbError(err)
}

func lockMemberRole(ctx context.Context, tx *sql.Tx, userID, teamID int64) (Role, error) {
	var role Role
	err := tx.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ? FOR SHARE`,
		teamID, userID).Scan(&role)

	return role, dbError(err)
}

func dbError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return err
	}

	switch mysqlErr.Number {
	case 1062:
		return fmt.Errorf("%w: already exists", ErrConflict)
	case 1452:
		return fmt.Errorf("%w: referenced record does not exist", ErrInvalid)
	case 1205, 1213:
		return fmt.Errorf("%w: concurrent write, retry the request", ErrConflict)
	default:
		return err
	}
}

func validText(value string, min, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max &&
		utf8.RuneCountInString(strings.TrimSpace(value)) >= min
}

func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
