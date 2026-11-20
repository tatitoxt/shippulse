GO ?= go
WEB := apps/web
COMPOSE ?= docker compose
export TEST_DATABASE_URL ?= postgres://shippulse:shippulse@localhost:5432/shippulse_test?sslmode=disable
export TEST_NATS_URL ?= nats://localhost:4222

.PHONY: build test test-integration lint vuln deps-up up down demo demo-reset web-install web-check e2e ci

build: ## Build the shippulse binary into bin/
	$(GO) build -o bin/shippulse ./cmd/shippulse

test: ## Go unit tests (no external services)
	$(GO) test -race ./...

deps-up: ## Start only PostgreSQL and NATS (for integration tests)
	$(COMPOSE) up -d --wait postgres nats
	$(COMPOSE) exec -T postgres sh -c "psql -U shippulse -tc \"SELECT 1 FROM pg_database WHERE datname='shippulse_test'\" | grep -q 1 || createdb -U shippulse shippulse_test"

test-integration: ## Go integration tests against real PostgreSQL and NATS
	$(GO) test -race -count=1 -tags=integration ./...

lint: ## gofmt + go vet + web lint/type check
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	$(GO) vet ./...
	$(GO) vet -tags=integration ./...

vuln: ## govulncheck + npm audit (high/critical)
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
	cd $(WEB) && npm audit --audit-level=high

up: ## Build and start the full stack (API :8080, console :3000)
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

demo: up ## Scripted demo: two healthy deploys and one failed deploy that is rolled back
	$(COMPOSE) exec -T -e SHIPPULSE_API_URL=http://localhost:8080 api /shippulse demo

demo-reset: ## Drop all demo data (volumes) and stop the stack
	$(COMPOSE) down -v

web-install:
	cd $(WEB) && npm ci

web-check: ## Lint, type-check and build the console
	cd $(WEB) && npm run lint && npm run typecheck && npm run build

e2e: up ## Playwright E2E against the running compose stack
	cd $(WEB) && npx playwright test

ci: lint test test-integration web-check
