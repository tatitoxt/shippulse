package main

import (
	"context"
	"log"
	"os"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/auth"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/config"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
)

func main() {
	log.Println("==> Seeding ShipPulse database with realistic demo data...")

	cfg := config.Load()
	postgres, err := db.NewPostgresDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer postgres.Close()

	ctx := context.Background()
	demoEmail := os.Getenv("DEMO_EMAIL")
	demoPassword := os.Getenv("DEMO_PASSWORD")
	monitorURL := os.Getenv("DEMO_MONITOR_URL")
	if demoEmail == "" || demoPassword == "" || monitorURL == "" {
		log.Fatal("DEMO_EMAIL, DEMO_PASSWORD and DEMO_MONITOR_URL are required")
	}

	// 1. Create Demo User
	pwdHash, _ := auth.HashPassword(demoPassword)
	var userID string
	userQuery := `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, $2, 'Demo Operator')
		ON CONFLICT (email) DO UPDATE SET full_name = EXCLUDED.full_name
		RETURNING id
	`
	err = postgres.Pool.QueryRow(ctx, userQuery, demoEmail, pwdHash).Scan(&userID)
	if err != nil {
		log.Fatalf("Failed to seed user: %v", err)
	}

	// 2. Create Demo Organization
	var orgID string
	orgQuery := `
		INSERT INTO organizations (name, slug)
		VALUES ('ShipPulse Global', 'shippulse-global')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`
	err = postgres.Pool.QueryRow(ctx, orgQuery).Scan(&orgID)
	if err != nil {
		log.Fatalf("Failed to seed org: %v", err)
	}

	// 3. Create Membership
	_, _ = postgres.Pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'OWNER')
		ON CONFLICT (organization_id, user_id) DO NOTHING
	`, orgID, userID)

	// 4. Create Demo Project
	var projectID string
	projQuery := `
		INSERT INTO projects (organization_id, name, slug, description)
		VALUES ($1, 'Core Platform', 'core-platform', 'Main production cloud platform and microservices')
		ON CONFLICT (organization_id, slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`
	_ = postgres.Pool.QueryRow(ctx, projQuery, orgID).Scan(&projectID)

	// 5. Create Environments
	var prodEnvID, stagingEnvID string
	envQuery := `
		INSERT INTO environments (organization_id, project_id, name, slug, color)
		VALUES ($1, $2, 'Production', 'production', '#EF4444')
		ON CONFLICT (project_id, slug) DO UPDATE SET color = EXCLUDED.color
		RETURNING id
	`
	_ = postgres.Pool.QueryRow(ctx, envQuery, orgID, projectID).Scan(&prodEnvID)

	_ = postgres.Pool.QueryRow(ctx, `
		INSERT INTO environments (organization_id, project_id, name, slug, color)
		VALUES ($1, $2, 'Staging', 'staging', '#F59E0B')
		ON CONFLICT (project_id, slug) DO UPDATE SET color = EXCLUDED.color
		RETURNING id
	`, orgID, projectID).Scan(&stagingEnvID)

	// 6. Create Microservices
	services := []struct {
		name, slug, runtime, health, version string
	}{
		{"Web Dashboard", "web-dashboard", "docker", "/api/health", "v2.4.1"},
		{"API Gateway", "api-gateway", "kubernetes", "/healthz", "v1.9.3"},
		{"Payments Service", "payments-service", "docker", "/health", "v1.2.0"},
		{"Async Background Worker", "async-worker", "docker", "/health", "v3.0.1"},
	}

	serviceIDs := make(map[string]string)
	for _, s := range services {
		var sID string
		sQuery := `
			INSERT INTO services (organization_id, project_id, name, slug, runtime, health_endpoint, current_version, owner_team, tags)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'Core Ops', '{"microservice", "production"}')
			ON CONFLICT (project_id, slug) DO UPDATE SET current_version = EXCLUDED.current_version
			RETURNING id
		`
		if err := postgres.Pool.QueryRow(ctx, sQuery, orgID, projectID, s.name, s.slug, s.runtime, s.health, s.version).Scan(&sID); err == nil {
			serviceIDs[s.slug] = sID
		}
	}

	// 7. Create Service Dependencies
	if apiID, ok1 := serviceIDs["api-gateway"]; ok1 {
		if webID, ok2 := serviceIDs["web-dashboard"]; ok2 {
			_, _ = postgres.Pool.Exec(ctx, `INSERT INTO service_dependencies (organization_id, service_id, depends_on_service_id, dependency_type) VALUES ($1, $2, $3, 'HTTP') ON CONFLICT DO NOTHING`, webID, apiID)
		}
		if payID, ok3 := serviceIDs["payments-service"]; ok3 {
			_, _ = postgres.Pool.Exec(ctx, `INSERT INTO service_dependencies (organization_id, service_id, depends_on_service_id, dependency_type) VALUES ($1, $2, $3, 'gRPC') ON CONFLICT DO NOTHING`, apiID, payID)
		}
	}

	// 8. Create Demo Deployments
	if apiID, ok := serviceIDs["api-gateway"]; ok && prodEnvID != "" {
		_, _ = postgres.Pool.Exec(ctx, `
			INSERT INTO deployments (organization_id, project_id, environment_id, service_id, version, git_sha, git_branch, commit_message, author, source, status, started_at, finished_at)
			VALUES ($1, $2, $3, $4, 'v1.9.3', 'a7f92bc', 'main', 'feat(auth): add OAuth2 token rotation', 'Fausto Pastura', 'github_actions', 'SUCCEEDED', NOW() - INTERVAL '15 minutes', NOW() - INTERVAL '13 minutes')
		`, orgID, projectID, prodEnvID, apiID)
	}

	// 9. Create Demo Incident
	if payID, ok := serviceIDs["payments-service"]; ok && prodEnvID != "" {
		var incID string
		incQuery := `
			INSERT INTO incidents (organization_id, project_id, environment_id, service_id, title, summary, severity, status, started_at, detected_at)
			VALUES ($1, $2, $3, $4, 'Intermittent Payment Timeout Spikes', 'Elevated HTTP 504 gateway timeouts observed in billing service', 'SEV2', 'INVESTIGATING', NOW() - INTERVAL '10 minutes', NOW() - INTERVAL '8 minutes')
			RETURNING id
		`
		if err := postgres.Pool.QueryRow(ctx, incQuery, orgID, projectID, prodEnvID, payID).Scan(&incID); err == nil {
			_, _ = postgres.Pool.Exec(ctx, `INSERT INTO incident_events (incident_id, event_type, message, actor) VALUES ($1, 'incident_created', 'Alert triggered: p95 latency > 750ms', 'AlertEngine')`, incID)
			_, _ = postgres.Pool.Exec(ctx, `INSERT INTO incident_events (incident_id, event_type, message, actor) VALUES ($1, 'note_added', 'Investigating upstream gateway logs in ClickHouse', 'Fausto Pastura')`, incID)
		}
	}

	// 10. Create Demo Monitors
	for slug, sID := range serviceIDs {
		_, _ = postgres.Pool.Exec(ctx, `
			INSERT INTO monitors (organization_id, service_id, environment_id, name, type, endpoint_url, interval_seconds, current_state, last_check_at)
			VALUES ($1, $2, $3, $4, 'HTTP', $5, 15, 'HEALTHY', NOW())
		`, orgID, sID, prodEnvID, "HTTP Probe - "+slug, monitorURL)
	}

	// 11. Create Demo SLO
	if apiID, ok := serviceIDs["api-gateway"]; ok {
		_, _ = postgres.Pool.Exec(ctx, `
			INSERT INTO slos (organization_id, service_id, name, target_percentage, time_window_days, slo_type, current_sli, error_budget_remaining)
			VALUES ($1, $2, 'API Availability SLO', 99.9, 30, 'AVAILABILITY', 99.94, 88.5)
		`, orgID, apiID)
	}

	log.Println("==> Database seeding complete")
}
