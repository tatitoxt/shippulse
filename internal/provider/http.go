package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/simruntime"
)

// HTTP talks to a simruntime server (cmd: `shippulse simruntime`).
type HTTP struct {
	BaseURL string
	Client  *http.Client
}

func NewHTTP(baseURL string) *HTTP {
	return &HTTP{BaseURL: baseURL, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *HTTP) url(t Target, suffix string) string {
	return fmt.Sprintf("%s/v1/runtimes/%s/%s/%s%s", h.BaseURL, url.PathEscape(t.Tenant), url.PathEscape(t.Service), url.PathEscape(t.Environment), suffix)
}

func (h *HTTP) Apply(ctx context.Context, t Target, version string, profile domain.FailureProfile) error {
	body, _ := json.Marshal(map[string]string{"version": version, "profile": string(profile)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, h.url(t, ""), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	default:
		return fmt.Errorf("provider rejected apply: status %d", resp.StatusCode)
	}
}

func (h *HTTP) Probe(ctx context.Context, t Target) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url(t, "/health"), nil)
	if err != nil {
		return err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrUnhealthy, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnhealthy, resp.StatusCode)
	}
	return nil
}

func (h *HTTP) Status(ctx context.Context, t Target) (simruntime.Status, error) {
	var st simruntime.Status
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url(t, ""), nil)
	if err != nil {
		return st, err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return st, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return st, simruntime.ErrNoRuntime
	}
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}
