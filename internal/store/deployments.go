package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/tatitoxt/shippulse/internal/domain"
)

const deploymentColumns = `d.id, d.organization_id, d.service_id, s.slug, d.environment_id, e.slug, d.version,
	d.failure_profile, d.source, d.actor, d.git_sha, s.repository_full_name, d.status,
	d.previous_version, d.previous_deployment_id,
	d.error_code, d.error_message, d.correlation_id, d.attempts, d.created_at, d.started_at, d.finished_at,
	d.canary_weights`

const deploymentFrom = ` FROM deployments d
	JOIN services s ON s.id = d.service_id
	JOIN environments e ON e.id = d.environment_id `

func scanDeployment(r pgx.Row) (Deployment, error) {
	var d Deployment
	err := r.Scan(&d.ID, &d.OrganizationID, &d.ServiceID, &d.ServiceSlug, &d.EnvironmentID, &d.EnvironmentSlug,
		&d.Version, &d.FailureProfile, &d.Source, &d.Actor, &d.GitSHA, &d.Repository, &d.Status, &d.PreviousVersion,
		&d.PreviousDeploymentID, &d.ErrorCode, &d.ErrorMessage, &d.CorrelationID, &d.Attempts,
		&d.CreatedAt, &d.StartedAt, &d.FinishedAt, &d.CanaryWeights)
	return d, err
}

const rollbackColumns = `id, organization_id, target_deployment_id, restore_deployment_id, from_version, to_version,
	trigger, reason, status, error_code, safe_output, correlation_id, actor, attempts, created_at, started_at, finished_at`

func scanRollback(r pgx.Row) (Rollback, error) {
	var rb Rollback
	err := r.Scan(&rb.ID, &rb.OrganizationID, &rb.TargetDeploymentID, &rb.RestoreDeploymentID, &rb.FromVersion,
		&rb.ToVersion, &rb.Trigger, &rb.Reason, &rb.Status, &rb.ErrorCode, &rb.SafeOutput, &rb.CorrelationID,
		&rb.Actor, &rb.Attempts, &rb.CreatedAt, &rb.StartedAt, &rb.FinishedAt)
	return rb, err
}

// CreateDeployment validates the target and atomically writes the deployment,
// its ordered steps, the outbox job and the audit record. A repeated
// idempotency key returns the original deployment with created=false.
func (s *Store) CreateDeployment(ctx context.Context, in CreateDeploymentInput) (Deployment, bool, error) {
	if !validUUID(in.ServiceID) || !validUUID(in.EnvironmentID) {
		return Deployment{}, false, fmt.Errorf("%w: service or environment", domain.ErrNotFound)
	}
	if existing, err := s.deploymentByKey(ctx, in.OrganizationID, in.IdempotencyKey); err == nil {
		return checkSameRequest(existing, in)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return Deployment{}, false, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Deployment{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ok bool
	err = tx.QueryRow(ctx, `
		SELECT true FROM services s JOIN environments e ON e.project_id = s.project_id
		WHERE s.organization_id = $1 AND s.id = $2 AND e.id = $3`,
		in.OrganizationID, in.ServiceID, in.EnvironmentID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, false, fmt.Errorf("%w: service or environment", domain.ErrNotFound)
	}
	if err != nil {
		return Deployment{}, false, err
	}

	var gitSHA *string
	if in.GitSHA != "" {
		gitSHA = &in.GitSHA
	}
	if in.Canary == nil {
		in.Canary = []int{}
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO deployments (organization_id, service_id, environment_id, version, failure_profile,
			idempotency_key, source, actor, git_sha, correlation_id, canary_weights)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
		RETURNING id`,
		in.OrganizationID, in.ServiceID, in.EnvironmentID, in.Version, in.FailureProfile,
		in.IdempotencyKey, in.Source, in.Actor, gitSHA, in.CorrelationID, in.Canary).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent request with the same key won the race.
		_ = tx.Rollback(ctx)
		existing, err := s.deploymentByKey(ctx, in.OrganizationID, in.IdempotencyKey)
		if err != nil {
			return Deployment{}, false, err
		}
		return checkSameRequest(existing, in)
	}
	if err != nil {
		return Deployment{}, false, fmt.Errorf("insert deployment: %w", err)
	}
	for i, name := range domain.DeploymentSteps(in.Canary) {
		if _, err := tx.Exec(ctx, `INSERT INTO deployment_steps (deployment_id, ordinal, name) VALUES ($1, $2, $3)`,
			id, i+1, name); err != nil {
			return Deployment{}, false, fmt.Errorf("insert step %s: %w", name, err)
		}
	}
	if err := insertOutbox(ctx, tx, in.OrganizationID, id, domain.EventDeploymentRequested, in.CorrelationID); err != nil {
		return Deployment{}, false, err
	}
	if err := insertAudit(ctx, tx, in.OrganizationID, in.Actor, "deployment.requested", "deployment", id, in.CorrelationID,
		map[string]any{"version": in.Version, "source": in.Source, "failure_profile": in.FailureProfile,
			"service_id": in.ServiceID, "environment_id": in.EnvironmentID, "canary": in.Canary}); err != nil {
		return Deployment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Deployment{}, false, err
	}
	d, err := s.GetDeployment(ctx, in.OrganizationID, id)
	return d, true, err
}

func checkSameRequest(d Deployment, in CreateDeploymentInput) (Deployment, bool, error) {
	if d.ServiceID != in.ServiceID || d.EnvironmentID != in.EnvironmentID || d.Version != in.Version {
		return d, false, fmt.Errorf("%w: idempotency key already used for a different deployment", domain.ErrConflict)
	}
	return d, false, nil
}

func (s *Store) deploymentByKey(ctx context.Context, org, key string) (Deployment, error) {
	d, err := scanDeployment(s.Pool.QueryRow(ctx, `SELECT `+deploymentColumns+deploymentFrom+
		`WHERE d.organization_id = $1 AND d.idempotency_key = $2`, org, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	return d, err
}

// GetDeployment returns a tenant-scoped deployment with its steps and rollbacks.
func (s *Store) GetDeployment(ctx context.Context, org, id string) (Deployment, error) {
	if !validUUID(id) {
		return Deployment{}, domain.ErrNotFound
	}
	d, err := scanDeployment(s.Pool.QueryRow(ctx, `SELECT `+deploymentColumns+deploymentFrom+
		`WHERE d.organization_id = $1 AND d.id = $2`, org, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if d.Steps, err = s.steps(ctx, d.ID); err != nil {
		return d, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+rollbackColumns+` FROM rollback_operations
		WHERE target_deployment_id = $1 ORDER BY created_at`, d.ID)
	if err != nil {
		return d, err
	}
	d.Rollbacks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Rollback, error) { return scanRollback(r) })
	return d, err
}

func (s *Store) steps(ctx context.Context, deploymentID string) ([]Step, error) {
	rows, err := s.Pool.Query(ctx, `SELECT ordinal, name, status, attempts, safe_output, started_at, finished_at
		FROM deployment_steps WHERE deployment_id = $1 ORDER BY ordinal`, deploymentID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Step, error) {
		var st Step
		err := r.Scan(&st.Ordinal, &st.Name, &st.Status, &st.Attempts, &st.SafeOutput, &st.StartedAt, &st.FinishedAt)
		return st, err
	})
}

type ListFilter struct {
	ServiceID string
	Limit     int
}

func (s *Store) ListDeployments(ctx context.Context, org string, f ListFilter) ([]Deployment, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var serviceID *string
	if f.ServiceID != "" {
		if !validUUID(f.ServiceID) {
			return []Deployment{}, nil
		}
		serviceID = &f.ServiceID
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+deploymentColumns+deploymentFrom+`
		WHERE d.organization_id = $1 AND ($2::uuid IS NULL OR d.service_id = $2)
		ORDER BY d.created_at DESC LIMIT $3`, org, serviceID, f.Limit)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Deployment, error) { return scanDeployment(r) })
	if err != nil {
		return nil, err
	}
	for i := range list {
		rb, err := scanRollback(s.Pool.QueryRow(ctx, `SELECT `+rollbackColumns+` FROM rollback_operations
			WHERE target_deployment_id = $1 ORDER BY created_at DESC LIMIT 1`, list[i].ID))
		if err == nil {
			list[i].Rollbacks = []Rollback{rb}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return list, nil
}

// CancelDeployment cancels a deployment that has not been claimed by a worker yet.
func (s *Store) CancelDeployment(ctx context.Context, org, id, actor, corr string) (Deployment, error) {
	if !validUUID(id) {
		return Deployment{}, domain.ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Deployment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM deployments WHERE organization_id = $1 AND id = $2 FOR UPDATE`, org, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, domain.ErrNotFound
	}
	if err != nil {
		return Deployment{}, err
	}
	if !domain.CanTransitionDeployment(domain.DeploymentStatus(status), domain.DeploymentCancelled) {
		return Deployment{}, fmt.Errorf("%w: deployment is %s; only queued deployments can be cancelled", domain.ErrConflict, status)
	}
	if _, err := tx.Exec(ctx, `UPDATE deployments SET status = 'cancelled', finished_at = now() WHERE id = $1`, id); err != nil {
		return Deployment{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployment_steps SET status = 'skipped' WHERE deployment_id = $1 AND status = 'pending'`, id); err != nil {
		return Deployment{}, err
	}
	if err := insertAudit(ctx, tx, org, actor, "deployment.cancelled", "deployment", id, corr, nil); err != nil {
		return Deployment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Deployment{}, err
	}
	return s.GetDeployment(ctx, org, id)
}

// RequestRollback queues a manual rollback of a deployment to its previous stable version.
func (s *Store) RequestRollback(ctx context.Context, org, deploymentID, actor, idemKey, reason, corr string) (Rollback, bool, error) {
	if !validUUID(deploymentID) {
		return Rollback{}, false, domain.ErrNotFound
	}
	if rb, err := scanRollback(s.Pool.QueryRow(ctx, `SELECT `+rollbackColumns+` FROM rollback_operations
		WHERE organization_id = $1 AND idempotency_key = $2`, org, idemKey)); err == nil {
		if rb.TargetDeploymentID != deploymentID {
			return rb, false, fmt.Errorf("%w: idempotency key already used for a different rollback", domain.ErrConflict)
		}
		return rb, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Rollback{}, false, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Rollback{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		status, version                        string
		prevVersion, prevDepID                 *string
		isCurrent, active, restored, unchanged bool
	)
	err = tx.QueryRow(ctx, `
		SELECT d.status, d.version, d.previous_version, d.previous_deployment_id,
			COALESCE(se.current_deployment_id = d.id, false),
			EXISTS (SELECT 1 FROM rollback_operations r WHERE r.target_deployment_id = d.id AND r.status IN ('queued','running')),
			EXISTS (SELECT 1 FROM rollback_operations r WHERE r.target_deployment_id = d.id AND r.status = 'succeeded'),
			se.current_deployment_id IS NOT DISTINCT FROM d.previous_deployment_id
		FROM deployments d
		LEFT JOIN service_environments se ON se.service_id = d.service_id AND se.environment_id = d.environment_id
		WHERE d.organization_id = $1 AND d.id = $2
		FOR UPDATE OF d`, org, deploymentID).
		Scan(&status, &version, &prevVersion, &prevDepID, &isCurrent, &active, &restored, &unchanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rollback{}, false, domain.ErrNotFound
	}
	if err != nil {
		return Rollback{}, false, err
	}
	prev := ""
	if prevVersion != nil {
		prev = *prevVersion
	}
	if err := domain.CheckManualRollback(domain.RollbackEligibility{
		Status: domain.DeploymentStatus(status), PreviousVersion: prev,
		IsCurrent: isCurrent, ActiveRollback: active, RestoredAlready: restored, StableUnchanged: unchanged,
	}); err != nil {
		return Rollback{}, false, err
	}
	rb, err := insertRollback(ctx, tx, org, deploymentID, prevDepID, version, prev, "manual", reason, idemKey, actor, corr)
	if err != nil {
		return Rollback{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Rollback{}, false, err
	}
	return rb, true, nil
}

func insertRollback(ctx context.Context, tx pgx.Tx, org, targetID string, restoreID *string, from, to, trigger, reason, key, actor, corr string) (Rollback, error) {
	rb, err := scanRollback(tx.QueryRow(ctx, `
		INSERT INTO rollback_operations (organization_id, target_deployment_id, restore_deployment_id,
			from_version, to_version, trigger, reason, idempotency_key, actor, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+rollbackColumns, org, targetID, restoreID, from, to, trigger, reason, key, actor, corr))
	if isUniqueViolation(err, "rollback_one_active_per_target") {
		return rb, fmt.Errorf("%w: a rollback is already in progress", domain.ErrConflict)
	}
	if err != nil {
		return rb, fmt.Errorf("insert rollback: %w", err)
	}
	if err := insertOutbox(ctx, tx, org, rb.ID, domain.EventRollbackRequested, corr); err != nil {
		return rb, err
	}
	err = insertAudit(ctx, tx, org, actor, "rollback.requested", "deployment", targetID, corr,
		map[string]any{"rollback_id": rb.ID, "trigger": trigger, "from_version": from, "to_version": to, "reason": reason})
	return rb, err
}

func (s *Store) GetRollback(ctx context.Context, org, id string) (Rollback, error) {
	if !validUUID(id) {
		return Rollback{}, domain.ErrNotFound
	}
	rb, err := scanRollback(s.Pool.QueryRow(ctx, `SELECT `+rollbackColumns+` FROM rollback_operations
		WHERE organization_id = $1 AND id = $2`, org, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return rb, domain.ErrNotFound
	}
	return rb, err
}

// ListAudit returns audit events for a tenant, optionally for one subject.
func (s *Store) ListAudit(ctx context.Context, org, subjectID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var subject *string
	if subjectID != "" {
		if !validUUID(subjectID) {
			return []AuditEvent{}, nil
		}
		subject = &subjectID
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, actor, action, subject_type, subject_id, correlation_id, metadata_redacted, occurred_at
		FROM audit_events WHERE organization_id = $1 AND ($2::uuid IS NULL OR subject_id = $2)
		ORDER BY occurred_at DESC, id LIMIT $3`, org, subject, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEvent, error) {
		var a AuditEvent
		err := r.Scan(&a.ID, &a.Actor, &a.Action, &a.SubjectType, &a.SubjectID, &a.CorrelationID, &a.Metadata, &a.OccurredAt)
		return a, err
	})
}
