// Package domain holds the release lifecycle rules: states, allowed
// transitions and input validation. It has no I/O so it is unit-testable.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type DeploymentStatus string

const (
	DeploymentQueued    DeploymentStatus = "queued"
	DeploymentRunning   DeploymentStatus = "running"
	DeploymentSucceeded DeploymentStatus = "succeeded"
	DeploymentFailed    DeploymentStatus = "failed"
	DeploymentCancelled DeploymentStatus = "cancelled"
)

func (s DeploymentStatus) Terminal() bool {
	return s == DeploymentSucceeded || s == DeploymentFailed || s == DeploymentCancelled
}

var deploymentTransitions = map[DeploymentStatus][]DeploymentStatus{
	DeploymentQueued:  {DeploymentRunning, DeploymentCancelled},
	DeploymentRunning: {DeploymentSucceeded, DeploymentFailed},
}

// CanTransitionDeployment reports whether from -> to is a legal move.
func CanTransitionDeployment(from, to DeploymentStatus) bool {
	for _, allowed := range deploymentTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

type RollbackStatus string

const (
	RollbackQueued    RollbackStatus = "queued"
	RollbackRunning   RollbackStatus = "running"
	RollbackSucceeded RollbackStatus = "succeeded"
	RollbackFailed    RollbackStatus = "failed"
)

func (s RollbackStatus) Terminal() bool { return s == RollbackSucceeded || s == RollbackFailed }

var rollbackTransitions = map[RollbackStatus][]RollbackStatus{
	RollbackQueued:  {RollbackRunning},
	RollbackRunning: {RollbackSucceeded, RollbackFailed},
}

func CanTransitionRollback(from, to RollbackStatus) bool {
	for _, allowed := range rollbackTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// Ordered deployment steps. The worker resumes at the first step that has not succeeded.
const (
	StepValidate     = "validate"
	StepApplyVersion = "apply_version"
	StepHealthProbe  = "health_probe"
	StepPromote      = "promote"
)

var Steps = []string{StepValidate, StepApplyVersion, StepHealthProbe, StepPromote}

// FailureProfile selects how the simulated runtime behaves for a version.
type FailureProfile string

const (
	ProfileHealthy             FailureProfile = "healthy"
	ProfileUnhealthy           FailureProfile = "unhealthy"
	ProfileTimeout             FailureProfile = "timeout"
	ProfileProviderUnavailable FailureProfile = "provider_unavailable"
)

func ParseFailureProfile(s string) (FailureProfile, error) {
	switch p := FailureProfile(strings.TrimSpace(s)); p {
	case "":
		return ProfileHealthy, nil
	case ProfileHealthy, ProfileUnhealthy, ProfileTimeout, ProfileProviderUnavailable:
		return p, nil
	default:
		return "", fmt.Errorf("%w: unknown failure_profile %q", ErrValidation, s)
	}
}

// Error codes persisted on failed deployments and rollbacks.
const (
	CodeProviderUnavailable = "provider_unavailable"
	CodeHealthCheckFailed   = "health_check_failed"
	CodeHealthCheckTimeout  = "health_check_timeout"
	CodeRollbackFailed      = "rollback_failed"
	CodeRollbackSuperseded  = "rollback_superseded"
	CodeTargetNotFound      = "target_not_found"
)

var (
	ErrValidation = errors.New("validation failed")
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrForbidden  = errors.New("forbidden")
)

var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateVersion accepts tags such as v1.2.3, 2024.05.1 or a short git sha.
func ValidateVersion(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("%w: version must match %s", ErrValidation, versionRe.String())
	}
	return nil
}

func ValidateIdempotencyKey(k string) error {
	if k == "" || len(k) > 200 {
		return fmt.Errorf("%w: Idempotency-Key header is required (1-200 chars)", ErrValidation)
	}
	return nil
}

// Role is a tenant membership role, ordered from most to least privileged.
type Role string

const (
	RoleOwner    Role = "OWNER"
	RoleAdmin    Role = "ADMIN"
	RoleEngineer Role = "ENGINEER"
	RoleViewer   Role = "VIEWER"
)

var roleRank = map[Role]int{RoleOwner: 4, RoleAdmin: 3, RoleEngineer: 2, RoleViewer: 1}

// Allows reports whether r grants at least the privileges of min.
func (r Role) Allows(min Role) bool {
	return roleRank[r] > 0 && roleRank[r] >= roleRank[min]
}

// RollbackEligibility decides whether a manual rollback may be requested.
type RollbackEligibility struct {
	Status          DeploymentStatus
	PreviousVersion string
	IsCurrent       bool // deployment is the promoted version of its target
	ActiveRollback  bool
	RestoredAlready bool // a rollback for this deployment already succeeded
	// StableUnchanged: the target's stable release is still the one this
	// deployment replaced, i.e. nothing was promoted after it.
	StableUnchanged bool
}

func CheckManualRollback(e RollbackEligibility) error {
	switch {
	case e.PreviousVersion == "":
		return fmt.Errorf("%w: no previous stable version to roll back to", ErrConflict)
	case e.ActiveRollback:
		return fmt.Errorf("%w: a rollback is already in progress", ErrConflict)
	case e.Status == DeploymentSucceeded && e.IsCurrent:
		return nil
	case e.Status == DeploymentFailed && !e.RestoredAlready && e.StableUnchanged:
		return nil
	case e.Status == DeploymentFailed && !e.StableUnchanged:
		return fmt.Errorf("%w: a newer release was promoted after this deployment failed", ErrConflict)
	default:
		return fmt.Errorf("%w: deployment in status %s cannot be rolled back", ErrConflict, e.Status)
	}
}
