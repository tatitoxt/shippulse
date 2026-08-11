package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	pool       *pgxpool.Pool
	httpClient *http.Client
}

func main() {
	log.Println("Starting ShipPulse Background Worker Service...")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Printf("Warning: Worker failed to connect to Postgres (%v). Retrying in loop...", err)
	} else {
		defer pool.Close()
		log.Println("Connected worker to PostgreSQL.")
	}

	w := &Worker{
		pool: pool,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	log.Println("Background worker active. Running health checks and alert evaluation loop...")

	for {
		select {
		case <-ticker.C:
			if w.pool != nil {
				w.runHealthChecks(ctx)
				w.evaluateAlerts(ctx)
			}
		}
	}
}

type MonitorItem struct {
	ID                           string
	OrganizationID               string
	ServiceID                    string
	EndpointURL                  string
	ExpectedStatus               int
	ConsecutiveFailureThreshold  int
	ConsecutiveRecoveryThreshold int
	CurrentState                 string
	ConsecutiveFailures          int
	ConsecutiveSuccesses         int
}

func (w *Worker) runHealthChecks(ctx context.Context) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, organization_id, service_id, endpoint_url, expected_status,
		       consecutive_failure_threshold, consecutive_recovery_threshold,
		       current_state, consecutive_failures, consecutive_successes
		FROM monitors
		LIMIT 50
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	var monitors []MonitorItem
	for rows.Next() {
		var m MonitorItem
		if err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.ServiceID, &m.EndpointURL, &m.ExpectedStatus,
			&m.ConsecutiveFailureThreshold, &m.ConsecutiveRecoveryThreshold,
			&m.CurrentState, &m.ConsecutiveFailures, &m.ConsecutiveSuccesses,
		); err == nil {
			monitors = append(monitors, m)
		}
	}

	for _, m := range monitors {
		w.probeMonitor(ctx, m)
	}
}

func (w *Worker) probeMonitor(ctx context.Context, m MonitorItem) {
	start := time.Now()
	resp, err := w.httpClient.Get(m.EndpointURL)
	latencyMS := int(time.Since(start).Milliseconds())

	isHealthy := false
	statusCode := 0
	errMsg := ""

	if err != nil {
		errMsg = err.Error()
	} else {
		statusCode = resp.StatusCode
		resp.Body.Close()
		if statusCode == m.ExpectedStatus || (m.ExpectedStatus == 200 && statusCode < 400) {
			isHealthy = true
		} else {
			errMsg = fmt.Sprintf("HTTP Status %d", statusCode)
		}
	}

	// Calculate new state
	newState := m.CurrentState
	newFailures := m.ConsecutiveFailures
	newSuccesses := m.ConsecutiveSuccesses

	if isHealthy {
		newSuccesses++
		newFailures = 0
		if newSuccesses >= m.ConsecutiveRecoveryThreshold {
			newState = "HEALTHY"
		}
	} else {
		newFailures++
		newSuccesses = 0
		if newFailures >= m.ConsecutiveFailureThreshold {
			newState = "UNHEALTHY"
		} else {
			newState = "DEGRADED"
		}
	}

	// Update Monitor in DB
	_, _ = w.pool.Exec(ctx, `
		UPDATE monitors
		SET current_state = $1, consecutive_failures = $2, consecutive_successes = $3, last_check_at = NOW()
		WHERE id = $4
	`, newState, newFailures, newSuccesses, m.ID)

	// Log health check
	_, _ = w.pool.Exec(ctx, `
		INSERT INTO health_check_logs (monitor_id, status_code, latency_ms, is_healthy, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, m.ID, statusCode, latencyMS, isHealthy, errMsg)
}

func (w *Worker) evaluateAlerts(ctx context.Context) {
	// Evaluate unhealthy monitors and trigger alert events if rules match
	rows, err := w.pool.Query(ctx, `
		SELECT ar.id, ar.organization_id, ar.service_id, ar.name, m.current_state
		FROM alert_rules ar
		JOIN monitors m ON m.service_id = ar.service_id
		WHERE ar.enabled = true AND m.current_state = 'UNHEALTHY'
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ruleID, orgID, serviceID, ruleName, state string
		if err := rows.Scan(&ruleID, &orgID, &serviceID, &ruleName, &state); err == nil {
			msg := fmt.Sprintf("Alert Triggered [%s]: Service in %s state", ruleName, state)
			_, _ = w.pool.Exec(ctx, `
				INSERT INTO alert_events (organization_id, alert_rule_id, service_id, triggered_value, status, message)
				VALUES ($1, $2, $3, 1.0, 'TRIGGERED', $4)
			`, orgID, ruleID, serviceID, msg)
		}
	}
}
