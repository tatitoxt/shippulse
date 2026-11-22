// Package ghstatus reports deployment outcomes back to GitHub as commit
// statuses (the same statuses CI posts), so a deploy shows up on the commit
// and PR that triggered it. It is a no-op without a token; failures are
// logged, never fatal — a status-reporting problem must never fail a deploy.
package ghstatus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
)

const DefaultBaseURL = "https://api.github.com"

// StateFor maps a deployment status to a GitHub commit status state.
func StateFor(s domain.DeploymentStatus) string {
	switch s {
	case domain.DeploymentSucceeded:
		return "success"
	case domain.DeploymentFailed:
		return "failure"
	case domain.DeploymentCancelled:
		return "error"
	default:
		return "pending"
	}
}

type Reporter struct {
	Token      string       // GitHub token; empty disables reporting entirely
	BaseURL    string       // API base; defaults to DefaultBaseURL (tests point at httptest)
	Context    string       // status context; defaults to "shippulse/<environment>"
	TargetBase string       // optional dashboard URL; statuses link to <TargetBase>/deployments/<id>
	HTTP       *http.Client // nil uses a default client with a 10s timeout
	Log        *slog.Logger
}

func (r *Reporter) Enabled() bool { return r != nil && r.Token != "" }

func (r *Reporter) baseURL() string {
	if r != nil && r.BaseURL != "" {
		return strings.TrimRight(r.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (r *Reporter) client() *http.Client {
	if r != nil && r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Report posts one commit status. Returns nil when the reporter is disabled
// or the deployment has nothing to report against (no repo or sha).
func (r *Reporter) Report(
	ctx context.Context, repo, sha, environment, deploymentID, state, description string,
) error {
	if !r.Enabled() || repo == "" || sha == "" {
		return nil
	}
	contextName := r.Context
	if contextName == "" {
		contextName = "shippulse/" + environment
	}
	payload := map[string]string{
		"state":       state,
		"context":     contextName,
		"description": description,
	}
	if r.TargetBase != "" {
		payload["target_url"] = strings.TrimRight(r.TargetBase, "/") + "/deployments/" + deploymentID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/statuses/%s", r.baseURL(), repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client().Do(req)
	if err != nil {
		return fmt.Errorf("github status post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github status post: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// ReportLogged is Report for callers that treat status delivery as
// best-effort: errors are warned, not returned.
func (r *Reporter) ReportLogged(
	ctx context.Context, repo, sha, environment, deploymentID, state, description string,
) {
	if !r.Enabled() {
		return
	}
	if err := r.Report(ctx, repo, sha, environment, deploymentID, state, description); err != nil && r.Log != nil {
		r.Log.Warn("github status report failed", "repo", repo, "sha", sha, "state", state, "error", err)
	}
}

// DeploymentStateFor maps a deployment status to a GitHub deployment_status
// state (the states shown on the repo's Environments view).
func DeploymentStateFor(s domain.DeploymentStatus) string {
	switch s {
	case domain.DeploymentQueued:
		return "queued"
	case domain.DeploymentRunning:
		return "in_progress"
	case domain.DeploymentSucceeded:
		return "success"
	case domain.DeploymentFailed:
		return "failure"
	case domain.DeploymentCancelled:
		return "error"
	default:
		return "pending"
	}
}

// ReportDeploymentStatus posts a deployment_status back to the GitHub
// deployment that triggered the run, so the repo's Environments view shows
// queued → in_progress → success/failure alongside the commit status.
// Returns nil when disabled or the deployment was not created by GitHub.
func (r *Reporter) ReportDeploymentStatus(
	ctx context.Context, repo string, ghDeploymentID int64, environment, deploymentID, state, description string,
) error {
	if !r.Enabled() || repo == "" || ghDeploymentID == 0 {
		return nil
	}
	payload := map[string]string{
		"state":       state,
		"environment": environment,
		"description": description,
	}
	if r.TargetBase != "" {
		payload["log_url"] = strings.TrimRight(r.TargetBase, "/") + "/deployments/" + deploymentID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/deployments/%d/statuses", r.baseURL(), repo, ghDeploymentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client().Do(req)
	if err != nil {
		return fmt.Errorf("github deployment status post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github deployment status post: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// ReportDeploymentStatusLogged is ReportDeploymentStatus for best-effort
// callers: errors are warned, never returned.
func (r *Reporter) ReportDeploymentStatusLogged(
	ctx context.Context, repo string, ghDeploymentID int64, environment, deploymentID, state, description string,
) {
	if !r.Enabled() {
		return
	}
	if err := r.ReportDeploymentStatus(ctx, repo, ghDeploymentID, environment, deploymentID, state, description); err != nil && r.Log != nil {
		r.Log.Warn("github deployment status report failed",
			"repo", repo, "github_deployment_id", ghDeploymentID, "state", state, "error", err)
	}
}
