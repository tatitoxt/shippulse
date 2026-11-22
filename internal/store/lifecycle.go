package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tatitoxt/shippulse/internal/domain"
)

// ErrLeaseLost means another worker took over the job; the caller must stop.
var ErrLeaseLost = errors.New("lease lost")

type ClaimResult int

const (
	Claimed   ClaimResult = iota
	ClaimDone             // already terminal (or cancelled): ack and drop the message
	ClaimBusy             // held by another live worker or target busy: retry later
)

func seconds(d time.Duration) float64 { return d.Seconds() }

// ClaimDeployment moves a queued deployment (or a running one whose lease
// expired) to running under owner's lease. Duplicate deliveries of the same
// job therefore execute at most one worker at a time. A queued deployment also
// waits while a rollback on the same target is pending, so a rollback that was
// requested first is never overtaken by a newer release.
func (s *Store) ClaimDeployment(ctx context.Context, id, owner string, lease time.Duration) (Deployment, ClaimResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Deployment{}, ClaimBusy, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var org, corr, prevStatus string
	var attempts int
	err = tx.QueryRow(ctx, `
		WITH prev AS (SELECT status FROM deployments WHERE id = $1)
		UPDATE deployments SET status = 'running', lease_owner = $2,
			lease_expires_at = now() + make_interval(secs => $3), attempts = attempts + 1,
			started_at = COALESCE(started_at, now())
		WHERE id = $1 AND (
			(status = 'running' AND lease_expires_at < now()) OR
			(status = 'queued' AND NOT EXISTS (
				SELECT 1 FROM rollback_operations r JOIN deployments t ON t.id = r.target_deployment_id
				WHERE t.service_id = deployments.service_id AND t.environment_id = deployments.environment_id
				  AND r.status IN ('queued', 'running'))))
		RETURNING organization_id, correlation_id, attempts, (SELECT status FROM prev)`,
		id, owner, seconds(lease)).Scan(&org, &corr, &attempts, &prevStatus)
	if isUniqueViolation(err, "deployments_one_running_per_target") {
		return Deployment{}, ClaimBusy, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		var status string
		err := s.Pool.QueryRow(ctx, `SELECT status FROM deployments WHERE id = $1`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ClaimDone, nil
		}
		if err != nil {
			return Deployment{}, ClaimBusy, err
		}
		if domain.DeploymentStatus(status).Terminal() {
			return Deployment{}, ClaimDone, nil
		}
		return Deployment{}, ClaimBusy, nil
	}
	if err != nil {
		return Deployment{}, ClaimBusy, fmt.Errorf("claim deployment: %w", err)
	}
	action := "deployment.started"
	if prevStatus == string(domain.DeploymentRunning) {
		action = "deployment.reclaimed"
	}
	if err := insertAudit(ctx, tx, org, "worker:"+owner, action, "deployment", id, corr,
		map[string]any{"attempt": attempts}); err != nil {
		return Deployment{}, ClaimBusy, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Deployment{}, ClaimBusy, err
	}
	d, err := s.GetDeployment(ctx, org, id)
	return d, Claimed, err
}

func lockOwned(ctx context.Context, tx pgx.Tx, table, id, owner string) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT true FROM `+table+` WHERE id = $1 AND status = 'running' AND lease_owner = $2 FOR UPDATE`, id, owner).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	return err
}

// StartStep marks a step running and counts the attempt.
func (s *Store) StartStep(ctx context.Context, d Deployment, owner, step string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "deployments", d.ID, owner); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployment_steps SET status = 'running', attempts = attempts + 1,
		started_at = COALESCE(started_at, now()) WHERE deployment_id = $1 AND name = $2`, d.ID, step); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployments SET lease_expires_at = GREATEST(lease_expires_at, now() + interval '60 seconds') WHERE id = $1`, d.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CompleteStep records a succeeded step with its safe (secret-free) output.
func (s *Store) CompleteStep(ctx context.Context, d Deployment, owner, step, output string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "deployments", d.ID, owner); err != nil {
		return err
	}
	if err := completeStep(ctx, tx, d, owner, step, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func completeStep(ctx context.Context, tx pgx.Tx, d Deployment, owner, step, output string) error {
	if _, err := tx.Exec(ctx, `UPDATE deployment_steps SET status = 'succeeded', safe_output = $3, finished_at = now()
		WHERE deployment_id = $1 AND name = $2`, d.ID, step, output); err != nil {
		return err
	}
	return insertAudit(ctx, tx, d.OrganizationID, "worker:"+owner, "deployment.step_succeeded", "deployment", d.ID,
		d.CorrelationID, map[string]any{"step": step, "output": output})
}

// ValidateTarget re-checks that the target still exists and snapshots the
// currently promoted version, which becomes the rollback target on failure.
func (s *Store) ValidateTarget(ctx context.Context, d Deployment, owner string) (Deployment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "deployments", d.ID, owner); err != nil {
		return d, err
	}
	var prevVersion, prevDep *string
	err = tx.QueryRow(ctx, `SELECT se.current_version, se.current_deployment_id
		FROM services s JOIN environments e ON e.project_id = s.project_id
		LEFT JOIN service_environments se ON se.service_id = s.id AND se.environment_id = e.id
		WHERE s.id = $1 AND e.id = $2 AND s.organization_id = $3`,
		d.ServiceID, d.EnvironmentID, d.OrganizationID).Scan(&prevVersion, &prevDep)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, fmt.Errorf("%w: service environment no longer exists", domain.ErrNotFound)
	}
	if err != nil {
		return d, err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployments SET previous_version = $2, previous_deployment_id = $3 WHERE id = $1`,
		d.ID, prevVersion, prevDep); err != nil {
		return d, err
	}
	out := "target valid; no previous stable version"
	if prevVersion != nil {
		out = "target valid; previous stable version " + *prevVersion
	}
	if err := completeStep(ctx, tx, d, owner, domain.StepValidate, out); err != nil {
		return d, err
	}
	if err := tx.Commit(ctx); err != nil {
		return d, err
	}
	d.PreviousVersion, d.PreviousDeploymentID = prevVersion, prevDep
	return d, nil
}

func setCurrent(ctx context.Context, tx pgx.Tx, org, serviceID, envID, version string, deploymentID *string) error {
	_, err := tx.Exec(ctx, `INSERT INTO service_environments (service_id, environment_id, organization_id, current_version, current_deployment_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (service_id, environment_id) DO UPDATE SET current_version = EXCLUDED.current_version,
			current_deployment_id = EXCLUDED.current_deployment_id,
			row_version = service_environments.row_version + 1, updated_at = now()`,
		serviceID, envID, org, version, deploymentID)
	return err
}

// Promote marks the deployment succeeded and records its version as the stable one.
func (s *Store) Promote(ctx context.Context, d Deployment, owner string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "deployments", d.ID, owner); err != nil {
		return err
	}
	if err := completeStep(ctx, tx, d, owner, domain.StepPromote, "promoted "+d.Version+" as stable"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployments SET status = 'succeeded', finished_at = now(),
		lease_owner = NULL, lease_expires_at = NULL WHERE id = $1`, d.ID); err != nil {
		return err
	}
	if err := setCurrent(ctx, tx, d.OrganizationID, d.ServiceID, d.EnvironmentID, d.Version, &d.ID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, d.OrganizationID, "worker:"+owner, "deployment.succeeded", "deployment", d.ID,
		d.CorrelationID, map[string]any{"version": d.Version}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Failure describes why a step failed.
type Failure struct {
	Step     string
	Code     string
	Message  string
	Rollback bool // runtime was changed, so the previous version must be restored
}

// Fail marks the step and deployment failed, skips the remaining steps and,
// when requested and possible, queues an automatic rollback in the same transaction.
func (s *Store) Fail(ctx context.Context, d Deployment, owner string, f Failure) (*Rollback, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "deployments", d.ID, owner); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployment_steps SET status = 'failed', safe_output = $3, finished_at = now()
		WHERE deployment_id = $1 AND name = $2`, d.ID, f.Step, f.Message); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployment_steps SET status = 'skipped'
		WHERE deployment_id = $1 AND status IN ('pending', 'running')`, d.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE deployments SET status = 'failed', error_code = $2, error_message = $3,
		finished_at = now(), lease_owner = NULL, lease_expires_at = NULL WHERE id = $1`, d.ID, f.Code, f.Message); err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, d.OrganizationID, "worker:"+owner, "deployment.failed", "deployment", d.ID,
		d.CorrelationID, map[string]any{"step": f.Step, "error_code": f.Code, "message": f.Message}); err != nil {
		return nil, err
	}
	var rb *Rollback
	if f.Rollback && d.PreviousVersion != nil {
		r, err := insertRollback(ctx, tx, d.OrganizationID, d.ID, d.PreviousDeploymentID, d.Version, *d.PreviousVersion,
			"automatic", "health probe failed: "+f.Code, "auto:"+d.ID, "worker:"+owner, d.CorrelationID)
		if err != nil {
			return nil, err
		}
		rb = &r
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return rb, nil
}

// ClaimRollback is the rollback counterpart of ClaimDeployment.
func (s *Store) ClaimRollback(ctx context.Context, id, owner string, lease time.Duration) (Rollback, ClaimResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Rollback{}, ClaimBusy, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rb, err := scanRollback(tx.QueryRow(ctx, `
		UPDATE rollback_operations SET status = 'running', lease_owner = $2,
			lease_expires_at = now() + make_interval(secs => $3), attempts = attempts + 1,
			started_at = COALESCE(started_at, now())
		WHERE id = $1 AND (status = 'queued' OR (status = 'running' AND lease_expires_at < now()))
		  AND NOT EXISTS (
			SELECT 1 FROM deployments t JOIN deployments x
			  ON x.service_id = t.service_id AND x.environment_id = t.environment_id
			WHERE t.id = rollback_operations.target_deployment_id AND x.status = 'running')
		RETURNING `+rollbackColumns, id, owner, seconds(lease)))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		var status string
		err := s.Pool.QueryRow(ctx, `SELECT status FROM rollback_operations WHERE id = $1`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) || domain.RollbackStatus(status).Terminal() {
			return Rollback{}, ClaimDone, nil
		}
		return Rollback{}, ClaimBusy, err
	}
	if err != nil {
		return Rollback{}, ClaimBusy, fmt.Errorf("claim rollback: %w", err)
	}
	// The rollback is only valid while the target still runs what it was meant
	// to undo: the rolled-back release itself (manual) or the stable release
	// the failed deployment replaced (automatic). Otherwise a newer release
	// was promoted in between and restoring would overwrite it.
	var valid bool
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(se.current_deployment_id = t.id, false) OR COALESCE(se.current_deployment_id = $2::uuid, false)
		FROM deployments t
		LEFT JOIN service_environments se ON se.service_id = t.service_id AND se.environment_id = t.environment_id
		WHERE t.id = $1`, rb.TargetDeploymentID, rb.RestoreDeploymentID).Scan(&valid); err != nil {
		return Rollback{}, ClaimBusy, fmt.Errorf("check rollback target: %w", err)
	}
	if !valid {
		out := "a newer release was promoted after this rollback was requested; nothing was changed"
		if _, err := tx.Exec(ctx, `UPDATE rollback_operations SET status = 'failed', error_code = $2, safe_output = $3,
			finished_at = now(), lease_owner = NULL, lease_expires_at = NULL WHERE id = $1`,
			rb.ID, domain.CodeRollbackSuperseded, out); err != nil {
			return Rollback{}, ClaimBusy, err
		}
		if err := insertAudit(ctx, tx, rb.OrganizationID, "worker:"+owner, "rollback.failed", "deployment", rb.TargetDeploymentID,
			rb.CorrelationID, map[string]any{"rollback_id": rb.ID, "error_code": domain.CodeRollbackSuperseded, "output": out}); err != nil {
			return Rollback{}, ClaimBusy, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Rollback{}, ClaimBusy, err
		}
		return Rollback{}, ClaimDone, nil
	}
	if err := insertAudit(ctx, tx, rb.OrganizationID, "worker:"+owner, "rollback.started", "deployment", rb.TargetDeploymentID,
		rb.CorrelationID, map[string]any{"rollback_id": rb.ID, "attempt": rb.Attempts, "to_version": rb.ToVersion}); err != nil {
		return Rollback{}, ClaimBusy, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Rollback{}, ClaimBusy, err
	}
	return rb, Claimed, nil
}

// FinishRollback records the rollback outcome. On success the restored version
// becomes the stable version of the target again.
func (s *Store) FinishRollback(ctx context.Context, rb Rollback, owner string, ok bool, code, output string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockOwned(ctx, tx, "rollback_operations", rb.ID, owner); err != nil {
		return err
	}
	status, action := domain.RollbackSucceeded, "rollback.succeeded"
	var errCode *string
	if !ok {
		status, action = domain.RollbackFailed, "rollback.failed"
		errCode = &code
	}
	if _, err := tx.Exec(ctx, `UPDATE rollback_operations SET status = $2, error_code = $3, safe_output = $4,
		finished_at = now(), lease_owner = NULL, lease_expires_at = NULL WHERE id = $1`, rb.ID, status, errCode, output); err != nil {
		return err
	}
	if ok {
		var serviceID, envID string
		if err := tx.QueryRow(ctx, `SELECT service_id, environment_id FROM deployments WHERE id = $1`, rb.TargetDeploymentID).
			Scan(&serviceID, &envID); err != nil {
			return err
		}
		if err := setCurrent(ctx, tx, rb.OrganizationID, serviceID, envID, rb.ToVersion, rb.RestoreDeploymentID); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, rb.OrganizationID, "worker:"+owner, action, "deployment", rb.TargetDeploymentID,
		rb.CorrelationID, map[string]any{"rollback_id": rb.ID, "to_version": rb.ToVersion, "output": output, "error_code": code}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WhileQueued runs fn while holding a share lock on the deployment if it is
// still queued. ClaimDeployment needs a row lock, so no worker can start the
// deployment (and report a newer status) until fn returns. It reports whether
// fn ran.
func (s *Store) WhileQueued(ctx context.Context, org, id string, fn func()) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM deployments WHERE organization_id = $1 AND id = $2 FOR SHARE`, org, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if status != string(domain.DeploymentQueued) {
		return false, nil
	}
	fn()
	return true, tx.Commit(ctx)
}
