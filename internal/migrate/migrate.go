// Package migrate applies the versioned SQL files in migrations/ in order.
// Migrations are forward-only; see docs/runbook.md for the downgrade strategy.
package migrate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var files embed.FS

// Migration is one versioned SQL file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// advisoryLockID serialises concurrent migrators (api and worker may start together).
const advisoryLockID = 7468001

// Load returns the embedded migrations sorted by version.
func Load() ([]Migration, error) {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q: missing version prefix", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q: %w", e.Name(), err)
		}
		body, err := files.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: e.Name(), SQL: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 1, got %d at position %d", m.Version, i+1)
		}
	}
	return out, nil
}

// Up applies every pending migration. It returns the resulting schema version.
func Up(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	return To(ctx, pool, -1)
}

// To applies pending migrations up to and including target (-1 means latest).
func To(ctx context.Context, pool *pgxpool.Pool, target int) (int, error) {
	migrations, err := Load()
	if err != nil {
		return 0, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockID); err != nil {
		return 0, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockID) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	var current int
	if err := conn.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, err
	}
	for _, m := range migrations {
		if m.Version <= current || (target >= 0 && m.Version > target) {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return current, err
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return current, fmt.Errorf("apply %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback(ctx)
			return current, err
		}
		if err := tx.Commit(ctx); err != nil {
			return current, err
		}
		current = m.Version
	}
	return current, nil
}
