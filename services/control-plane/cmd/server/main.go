package main

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/config"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/handlers"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
)

func main() {
	log.Println("Starting ShipPulse Control Plane API Server...")

	cfg := config.Load()
	if cfg.JWTSecret == "" || cfg.GithubSecret == "" {
		log.Fatal("JWT_SECRET and GITHUB_WEBHOOK_SECRET are required")
	}

	// 1. Initialize Postgres
	postgresDB, err := db.NewPostgresDB(cfg.DatabaseURL)
	if err != nil {
		log.Printf("Warning: Postgres connection failed (%v). Continuing in standalone/offline mode...", err)
	} else {
		defer postgresDB.Close()
		log.Println("Connected to PostgreSQL successfully.")
	}

	// 2. Initialize Redis
	redisClient, err := db.NewRedisClient(cfg.RedisURL)
	if err != nil {
		log.Printf("Warning: Redis connection failed (%v)...", err)
	} else {
		defer redisClient.Close()
		log.Println("Connected to Redis successfully.")
	}

	// 3. Initialize NATS
	natsClient, err := db.NewNatsClient(cfg.NatsURL)
	if err != nil {
		log.Printf("Warning: NATS connection failed (%v)...", err)
	} else {
		defer natsClient.Close()
		log.Println("Connected to NATS JetStream successfully.")
	}

	// 4. Initialize Handlers
	authH := handlers.NewAuthHandler(cfg, postgresDB)
	servicesH := handlers.NewServicesHandler(postgresDB, natsClient)
	deploymentsH := handlers.NewDeploymentsHandler(postgresDB, natsClient)
	incidentsH := handlers.NewIncidentsHandler(postgresDB, natsClient)
	agentsH := handlers.NewAgentsHandler(postgresDB, redisClient)
	webhooksH := handlers.NewWebhooksHandler(cfg, postgresDB, redisClient, natsClient)
	slosH := handlers.NewSLOsHandler(postgresDB)
	apiKeysH := handlers.NewAPIKeysHandler(postgresDB)
	monitorsH := handlers.NewMonitorsHandler(postgresDB)
	projectsH := handlers.NewProjectsHandler(postgresDB)
	healthAuditH := handlers.NewHealthAndAuditHandler(postgresDB)

	authMiddleware := middleware.NewAuthMiddleware(cfg.JWTSecret, postgresDB)

	// 5. Setup Chi Router
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(30 * time.Second))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.WebOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Organization-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public Routes
	r.Get("/health", healthAuditH.HealthCheck)
	r.Post("/api/v1/auth/register", authH.Register)
	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/agents/register", agentsH.RegisterAgent)
	r.Post("/api/v1/webhooks/github", webhooksH.HandleGitHubWebhook)

	// Authenticated Routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.Authenticate)

		r.Get("/api/v1/auth/me", authH.Me)

		// Projects & Environments
		r.Get("/api/v1/projects", projectsH.ListProjects)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/projects", projectsH.CreateProject)
		r.Get("/api/v1/environments", projectsH.ListEnvironments)

		// Services Catalog & Dependency Graph
		r.Get("/api/v1/services", servicesH.ListServices)
		r.Get("/api/v1/services/graph", servicesH.GetDependencyGraph)
		r.Get("/api/v1/services/{id}", servicesH.GetServiceByID)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/services", servicesH.CreateService)

		// Deployments
		r.Get("/api/v1/deployments", deploymentsH.ListDeployments)
		r.Get("/api/v1/deployments/{id}", deploymentsH.GetDeploymentByID)
		r.With(middleware.RequireRole("ENGINEER")).Post("/api/v1/deployments", deploymentsH.CreateDeployment)
		r.With(middleware.RequireRole("ENGINEER")).Post("/api/v1/deployments/{id}/rollback", deploymentsH.TriggerRollback)

		// Incidents
		r.Get("/api/v1/incidents", incidentsH.ListIncidents)
		r.Get("/api/v1/incidents/{id}", incidentsH.GetIncidentByID)
		r.With(middleware.RequireRole("ENGINEER")).Post("/api/v1/incidents", incidentsH.CreateIncident)
		r.With(middleware.RequireRole("ENGINEER")).Put("/api/v1/incidents/{id}/status", incidentsH.UpdateStatus)

		// Monitors & SLOs
		r.Get("/api/v1/monitors", monitorsH.ListMonitors)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/monitors", monitorsH.CreateMonitor)
		r.Get("/api/v1/slos", slosH.ListSLOs)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/slos", slosH.CreateSLO)

		// Agents & Tokens
		r.Get("/api/v1/agents", agentsH.ListAgents)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/agents/registration-tokens", agentsH.CreateRegistrationToken)
		r.Post("/api/v1/agents/heartbeat", agentsH.Heartbeat)

		// API Keys & Audit
		r.Get("/api/v1/api-keys", apiKeysH.ListAPIKeys)
		r.With(middleware.RequireRole("ADMIN")).Post("/api/v1/api-keys", apiKeysH.CreateAPIKey)
		r.With(middleware.RequireRole("ADMIN")).Delete("/api/v1/api-keys/{id}", apiKeysH.RevokeAPIKey)
		r.Get("/api/v1/audit-logs", healthAuditH.ListAuditLogs)
	})

	log.Printf("ShipPulse Control Plane listening on port :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
}
