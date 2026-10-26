package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var locked int
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(CONCAT(DATABASE(), ':migrations'), 30)`).Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return fmt.Errorf("migration lock unavailable")
	}
	defer releaseMigrationLock(conn)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(100) PRIMARY KEY, checksum CHAR(64) NOT NULL, dirty BOOLEAN NOT NULL
	)`); err != nil {
		return err
	}

	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := applyMigration(ctx, conn, file.Name()); err != nil {
			return err
		}
	}

	return nil
}

func releaseMigrationLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = conn.ExecContext(ctx, `DO RELEASE_LOCK(CONCAT(DATABASE(), ':migrations'))`)
}

func applyMigration(ctx context.Context, conn *sql.Conn, name string) error {
	data, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(data))

	var checksum string
	var dirty bool
	err = conn.QueryRowContext(ctx, `SELECT checksum, dirty FROM schema_migrations WHERE version = ?`, name).Scan(&checksum, &dirty)
	switch err {
	case nil:
		if dirty || checksum != sum {
			return fmt.Errorf("migration %s is dirty or its checksum has changed", name)
		}
		return nil
	case sql.ErrNoRows:
	default:
		return err
	}

	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, checksum, dirty) VALUES (?, ?, TRUE)`, name, sum); err != nil {
		return err
	}

	for _, statement := range strings.Split(string(data), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}

	_, err = conn.ExecContext(ctx, `UPDATE schema_migrations SET dirty = FALSE WHERE version = ?`, name)
	return err
}
