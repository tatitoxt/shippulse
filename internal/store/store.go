// Package store is the PostgreSQL persistence layer. Every state change that
// must be observed by another component is written in the same transaction as
// its outbox event and audit record.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tatitoxt/shippulse/internal/domain"
)

type Store struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}

func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func validUUID(s string) bool { _, err := uuid.Parse(s); return err == nil }

var sensitiveKeys = []string{"password", "secret", "token", "authorization", "signature", "key"}

// Redact drops values whose key looks sensitive before metadata is persisted.
func Redact(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		redacted := false
		for _, s := range sensitiveKeys {
			if strings.Contains(lk, s) && lk != "idempotency_key" {
				redacted = true
				break
			}
		}
		if redacted {
			out[k] = "[redacted]"
		} else {
			out[k] = v
		}
	}
	return out
}

func insertAudit(ctx context.Context, tx pgx.Tx, org, actor, action, subjectType, subjectID, corr string, meta map[string]any) error {
	body, err := json.Marshal(Redact(meta))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
		(organization_id, actor, action, subject_type, subject_id, correlation_id, metadata_redacted)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, org, actor, action, subjectType, subjectID, corr, body)
	if err != nil {
		return fmt.Errorf("insert audit %s: %w", action, err)
	}
	return nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, org, aggregateID, eventType, corr string) error {
	id := uuid.NewString()
	payload, err := json.Marshal(domain.JobEvent{
		EventID: id, Type: eventType, PayloadVersion: 1,
		OrganizationID: org, AggregateID: aggregateID, CorrelationID: corr,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events
		(id, organization_id, aggregate_id, event_type, payload, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, org, aggregateID, eventType, payload, corr)
	if err != nil {
		return fmt.Errorf("insert outbox %s: %w", eventType, err)
	}
	return nil
}

// UserCredentials returns the password hash and first membership for login.
func (s *Store) UserCredentials(ctx context.Context, email string) (Principal, string, error) {
	var p Principal
	var hash string
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.full_name, u.password_hash, m.organization_id, o.name, m.role
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		JOIN organizations o ON o.id = m.organization_id
		WHERE lower(u.email) = lower($1)
		ORDER BY m.created_at LIMIT 1`, email).
		Scan(&p.UserID, &p.Email, &p.FullName, &hash, &p.OrganizationID, &p.Organization, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, "", domain.ErrNotFound
	}
	return p, hash, err
}

// Membership re-reads the caller's role so revoked access takes effect immediately.
func (s *Store) Membership(ctx context.Context, userID, orgID string) (string, error) {
	var role string
	err := s.Pool.QueryRow(ctx, `SELECT role FROM memberships WHERE user_id = $1 AND organization_id = $2`, userID, orgID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrForbidden
	}
	return role, err
}

func (s *Store) ListServices(ctx context.Context, org string) ([]Service, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT s.id, s.organization_id, s.project_id, p.name, s.name, s.slug, s.repository_full_name, s.health_path
		FROM services s JOIN projects p ON p.id = s.project_id
		WHERE s.organization_id = $1 ORDER BY p.name, s.name`, org)
	if err != nil {
		return nil, err
	}
	services, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Service, error) {
		var sv Service
		err := r.Scan(&sv.ID, &sv.OrganizationID, &sv.ProjectID, &sv.ProjectName, &sv.Name, &sv.Slug, &sv.Repository, &sv.HealthPath)
		return sv, err
	})
	if err != nil {
		return nil, err
	}
	for i := range services {
		envs, err := s.serviceEnvironments(ctx, services[i].ID)
		if err != nil {
			return nil, err
		}
		services[i].Environments = envs
	}
	return services, nil
}

func (s *Store) GetService(ctx context.Context, org, id string) (Service, error) {
	if !validUUID(id) {
		return Service{}, domain.ErrNotFound
	}
	var sv Service
	err := s.Pool.QueryRow(ctx, `
		SELECT s.id, s.organization_id, s.project_id, p.name, s.name, s.slug, s.repository_full_name, s.health_path
		FROM services s JOIN projects p ON p.id = s.project_id
		WHERE s.organization_id = $1 AND s.id = $2`, org, id).
		Scan(&sv.ID, &sv.OrganizationID, &sv.ProjectID, &sv.ProjectName, &sv.Name, &sv.Slug, &sv.Repository, &sv.HealthPath)
	if errors.Is(err, pgx.ErrNoRows) {
		return sv, domain.ErrNotFound
	}
	if err != nil {
		return sv, err
	}
	sv.Environments, err = s.serviceEnvironments(ctx, sv.ID)
	return sv, err
}

func (s *Store) serviceEnvironments(ctx context.Context, serviceID string) ([]ServiceEnvironment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.slug, e.name, se.current_version, se.current_deployment_id, se.updated_at
		FROM services s
		JOIN environments e ON e.project_id = s.project_id
		LEFT JOIN service_environments se ON se.service_id = s.id AND se.environment_id = e.id
		WHERE s.id = $1 ORDER BY e.created_at, e.slug`, serviceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ServiceEnvironment, error) {
		var e ServiceEnvironment
		err := r.Scan(&e.EnvironmentID, &e.EnvironmentSlug, &e.EnvironmentName, &e.CurrentVersion, &e.CurrentDeploymentID, &e.UpdatedAt)
		return e, err
	})
}

// ResolveRepositoryTarget maps a GitHub repository and environment name to a
// registered service. Unknown repositories return ErrNotFound.
func (s *Store) ResolveRepositoryTarget(ctx context.Context, repo, envSlug string) (org, serviceID, envID string, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT s.organization_id, s.id, e.id
		FROM services s JOIN environments e ON e.project_id = s.project_id
		WHERE lower(s.repository_full_name) = lower($1) AND e.slug = $2`, repo, envSlug).Scan(&org, &serviceID, &envID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return
}
