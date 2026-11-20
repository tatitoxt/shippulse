package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDeploymentTransitions(t *testing.T) {
	all := []DeploymentStatus{DeploymentQueued, DeploymentRunning, DeploymentSucceeded, DeploymentFailed, DeploymentCancelled}
	allowed := map[[2]DeploymentStatus]bool{
		{DeploymentQueued, DeploymentRunning}:    true,
		{DeploymentQueued, DeploymentCancelled}:  true,
		{DeploymentRunning, DeploymentSucceeded}: true,
		{DeploymentRunning, DeploymentFailed}:    true,
	}
	for _, from := range all {
		for _, to := range all {
			if got, want := CanTransitionDeployment(from, to), allowed[[2]DeploymentStatus{from, to}]; got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalStatesHaveNoExits(t *testing.T) {
	for _, s := range []DeploymentStatus{DeploymentSucceeded, DeploymentFailed, DeploymentCancelled} {
		if !s.Terminal() || len(deploymentTransitions[s]) != 0 {
			t.Errorf("%s should be terminal with no transitions", s)
		}
	}
	for _, s := range []RollbackStatus{RollbackSucceeded, RollbackFailed} {
		if !s.Terminal() || len(rollbackTransitions[s]) != 0 {
			t.Errorf("rollback %s should be terminal with no transitions", s)
		}
	}
}

func TestRollbackTransitions(t *testing.T) {
	cases := []struct {
		from, to RollbackStatus
		want     bool
	}{
		{RollbackQueued, RollbackRunning, true},
		{RollbackRunning, RollbackSucceeded, true},
		{RollbackRunning, RollbackFailed, true},
		{RollbackQueued, RollbackSucceeded, false},
		{RollbackSucceeded, RollbackRunning, false},
		{RollbackFailed, RollbackQueued, false},
	}
	for _, c := range cases {
		if got := CanTransitionRollback(c.from, c.to); got != c.want {
			t.Errorf("%s -> %s: got %v want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestValidateVersion(t *testing.T) {
	for _, ok := range []string{"v1", "v1.2.3", "2026.11.0", "a1b2c3d", "release_7-rc.1"} {
		if err := ValidateVersion(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-v1", "v1 2", "v1;rm", "../etc", string(make([]byte, 65))} {
		if err := ValidateVersion(bad); !errors.Is(err, ErrValidation) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestParseFailureProfile(t *testing.T) {
	if p, err := ParseFailureProfile(""); err != nil || p != ProfileHealthy {
		t.Fatalf("empty profile should default to healthy, got %q %v", p, err)
	}
	if _, err := ParseFailureProfile("explode"); !errors.Is(err, ErrValidation) {
		t.Fatal("unknown profile should be a validation error")
	}
}

func TestRoleAllows(t *testing.T) {
	if !RoleOwner.Allows(RoleEngineer) || !RoleEngineer.Allows(RoleEngineer) {
		t.Fatal("owner and engineer may deploy")
	}
	if RoleViewer.Allows(RoleEngineer) {
		t.Fatal("viewer must not deploy")
	}
	if Role("ROOT").Allows(RoleViewer) {
		t.Fatal("unknown roles grant nothing")
	}
}

func TestCheckManualRollback(t *testing.T) {
	cases := []struct {
		name string
		in   RollbackEligibility
		ok   bool
	}{
		{"current succeeded", RollbackEligibility{Status: DeploymentSucceeded, PreviousVersion: "v1", IsCurrent: true}, true},
		{"superseded succeeded", RollbackEligibility{Status: DeploymentSucceeded, PreviousVersion: "v1"}, false},
		{"failed not restored", RollbackEligibility{Status: DeploymentFailed, PreviousVersion: "v1"}, true},
		{"failed already restored", RollbackEligibility{Status: DeploymentFailed, PreviousVersion: "v1", RestoredAlready: true}, false},
		{"no previous version", RollbackEligibility{Status: DeploymentSucceeded, IsCurrent: true}, false},
		{"active rollback", RollbackEligibility{Status: DeploymentFailed, PreviousVersion: "v1", ActiveRollback: true}, false},
		{"queued", RollbackEligibility{Status: DeploymentQueued, PreviousVersion: "v1"}, false},
	}
	for _, c := range cases {
		err := CheckManualRollback(c.in)
		if (err == nil) != c.ok {
			t.Errorf("%s: got err=%v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Errorf("%s: want ErrConflict, got %v", c.name, err)
		}
	}
}

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	base, max := 100*time.Millisecond, time.Second
	want := []time.Duration{100, 200, 400, 800, 1000, 1000}
	for i, w := range want {
		if got := Backoff(i+1, base, max); got != w*time.Millisecond {
			t.Errorf("attempt %d: got %v want %v", i+1, got, w*time.Millisecond)
		}
	}
	if Backoff(0, base, max) != base {
		t.Error("attempt 0 should be treated as 1")
	}
}
