package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Config struct {
	ControlPlaneURL string
	APIKey          string
	ServiceID       string
	Environment     string
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type HealthReport struct {
	Status    string            `json:"status"`
	UptimeSec int64             `json:"uptime_sec"`
	Details   map[string]string `json:"details,omitempty"`
}

func (c *Client) ReportHealth(status string, details map[string]string) error {
	payload := map[string]interface{}{
		"service_id":  c.cfg.ServiceID,
		"environment": c.cfg.Environment,
		"status":      status,
		"details":     details,
		"timestamp":   time.Now().UTC(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/health/reports", c.cfg.ControlPlaneURL), bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("shippulse sdk error: HTTP %d", resp.StatusCode)
	}

	return nil
}
