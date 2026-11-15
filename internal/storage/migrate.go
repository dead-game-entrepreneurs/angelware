package store

import (
	"context"
	"embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// All migrations and their version records commit atomically. The advisory lock
// also serializes first startup when multiple processes share one database.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7142423074)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS angelware_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var current int
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM angelware_migrations`).Scan(&current); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	if current > len(entries) {
		return fmt.Errorf("store: database migration %d is newer than this binary", current)
	}
	for _, entry := range entries {
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		if version != current+1 {
			return fmt.Errorf("store: missing migration %d", current+1)
		}
		sql, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("store: migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO angelware_migrations (version) VALUES ($1)`, version); err != nil {
			return err
		}
		current = version
	}
	return tx.Commit(ctx)
}
