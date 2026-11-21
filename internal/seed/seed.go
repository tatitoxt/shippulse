// Package seed loads the synthetic demo catalog. All names are fictional and
// use the reserved .example domain. Running it twice is safe.
package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tatitoxt/shippulse/internal/auth"
)

type User struct{ Email, Name, Role string }

type Service struct{ Name, Slug, Repo string }

type Tenant struct {
	Name, Slug   string
	Project      string
	Environments []string
	Services     []Service
	Users        []User
}

var Tenants = []Tenant{
	{
		Name: "Acme Demo", Slug: "acme-demo", Project: "storefront",
		Environments: []string{"staging", "production"},
		Services: []Service{
			{Name: "Checkout API", Slug: "checkout-api", Repo: "acme-demo/checkout-api"},
			{Name: "Catalog API", Slug: "catalog-api", Repo: "acme-demo/catalog-api"},
		},
		Users: []User{
			{Email: "operator@acme.example", Name: "Ada Operator", Role: "ENGINEER"},
			{Email: "viewer@acme.example", Name: "Vic Viewer", Role: "VIEWER"},
		},
	},
	{
		Name: "Globex Demo", Slug: "globex-demo", Project: "billing",
		Environments: []string{"staging"},
		Services:     []Service{{Name: "Invoice Worker", Slug: "invoice-worker", Repo: "globex-demo/invoice-worker"}},
		Users:        []User{{Email: "operator@globex.example", Name: "Gus Operator", Role: "ENGINEER"}},
	},
}

// Run upserts the demo tenants. password is applied to every demo user.
func Run(ctx context.Context, pool *pgxpool.Pool, password string) error {
	if len(password) < 8 {
		return fmt.Errorf("DEMO_PASSWORD must be at least 8 characters")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, t := range Tenants {
		var orgID, projectID string
		if err := tx.QueryRow(ctx, `INSERT INTO organizations (name, slug) VALUES ($1, $2)
			ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name RETURNING id`, t.Name, t.Slug).Scan(&orgID); err != nil {
			return fmt.Errorf("org %s: %w", t.Slug, err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO projects (organization_id, name, slug) VALUES ($1, $2, $2)
			ON CONFLICT (organization_id, slug) DO UPDATE SET name = EXCLUDED.name RETURNING id`, orgID, t.Project).Scan(&projectID); err != nil {
			return fmt.Errorf("project %s: %w", t.Project, err)
		}
		for _, env := range t.Environments {
			if _, err := tx.Exec(ctx, `INSERT INTO environments (organization_id, project_id, name, slug) VALUES ($1, $2, $3, $3)
				ON CONFLICT (project_id, slug) DO NOTHING`, orgID, projectID, env); err != nil {
				return fmt.Errorf("environment %s: %w", env, err)
			}
		}
		for _, s := range t.Services {
			if _, err := tx.Exec(ctx, `INSERT INTO services (organization_id, project_id, name, slug, repository_full_name)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (project_id, slug) DO UPDATE SET name = EXCLUDED.name, repository_full_name = EXCLUDED.repository_full_name`,
				orgID, projectID, s.Name, s.Slug, s.Repo); err != nil {
				return fmt.Errorf("service %s: %w", s.Slug, err)
			}
		}
		for _, u := range t.Users {
			var userID string
			if err := tx.QueryRow(ctx, `INSERT INTO users (email, full_name, password_hash) VALUES ($1, $2, $3)
				ON CONFLICT (email) DO UPDATE SET full_name = EXCLUDED.full_name, password_hash = EXCLUDED.password_hash
				RETURNING id`, u.Email, u.Name, hash).Scan(&userID); err != nil {
				return fmt.Errorf("user %s: %w", u.Email, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, $3)
				ON CONFLICT (organization_id, user_id) DO UPDATE SET role = EXCLUDED.role`, orgID, userID, u.Role); err != nil {
				return fmt.Errorf("membership %s: %w", u.Email, err)
			}
		}
	}
	return tx.Commit(ctx)
}
