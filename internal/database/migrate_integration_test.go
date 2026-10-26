//go:build integration

package database_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"secunda/internal/database"
	"secunda/internal/testutil"
)

func TestMigrationIntegrity(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
	}{
		{"unfinished migration", `UPDATE schema_migrations SET dirty = TRUE WHERE version = '001_init.sql'`},
		{"changed checksum", `UPDATE schema_migrations SET checksum = REPEAT('0', 64) WHERE version = '001_init.sql'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := testutil.Database(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if _, err := db.ExecContext(ctx, test.query); err != nil {
				t.Fatal(err)
			}

			err := database.Migrate(ctx, db)
			if err == nil || !strings.Contains(err.Error(), "dirty or its checksum has changed") {
				t.Fatalf("invalid migration state was not rejected: %v", err)
			}

			var lockFree bool
			if err := db.QueryRowContext(ctx, `SELECT IS_FREE_LOCK(CONCAT(DATABASE(), ':migrations'))`).Scan(&lockFree); err != nil {
				t.Fatal(err)
			}
			if !lockFree {
				t.Fatal("migration lock was not released after failure")
			}
		})
	}
}
