//go:build integration

// Package testutil provisions real PostgreSQL databases for integration tests.
package testutil

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tatitoxt/shippulse/internal/migrate"
	"github.com/tatitoxt/shippulse/internal/seed"
	"github.com/tatitoxt/shippulse/internal/store"
)

const Password = "integration-password"

func env(t testing.TB, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s must be set for integration tests (see `make deps-up`)", key)
	}
	return v
}

func NATSURL(t testing.TB) string { return env(t, "TEST_NATS_URL") }

// DB returns a store backed by a dedicated database (one per test package,
// so packages can run in parallel), reset to a migrated and seeded state.
func DB(t testing.TB, name string) *store.Store {
	t.Helper()
	ctx := context.Background()
	base := env(t, "TEST_DATABASE_URL")
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	dbName := "shippulse_it_" + name
	var exists bool
	_ = admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbName).Scan(&exists)
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()); err != nil {
			t.Fatalf("create database: %v", err)
		}
	}
	admin.Close(ctx)

	u, _ := url.Parse(base)
	u.Path = "/" + dbName
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := migrate.Up(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := seed.Run(ctx, pool, Password); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return store.New(pool)
}

// Target identifies a seeded service environment.
type Target struct{ Org, Service, Env string }

func Lookup(t testing.TB, s *store.Store, orgSlug, serviceSlug, envSlug string) Target {
	t.Helper()
	var tg Target
	err := s.Pool.QueryRow(context.Background(), `
		SELECT o.id, s.id, e.id FROM organizations o
		JOIN services s ON s.organization_id = o.id
		JOIN environments e ON e.project_id = s.project_id
		WHERE o.slug = $1 AND s.slug = $2 AND e.slug = $3`, orgSlug, serviceSlug, envSlug).Scan(&tg.Org, &tg.Service, &tg.Env)
	if err != nil {
		t.Fatalf("lookup %s/%s/%s: %v", orgSlug, serviceSlug, envSlug, err)
	}
	return tg
}

func Count(t *testing.T, s *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}
