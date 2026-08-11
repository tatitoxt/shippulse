.PHONY: all build dev test lint clean docker-up docker-down seed

all: build

build:
	@echo "==> Building all Go services..."
	cd services/control-plane && go build -o ../../bin/control-plane ./cmd/server
	cd services/ingest && go build -o ../../bin/ingest ./cmd/ingest
	cd services/worker && go build -o ../../bin/worker ./cmd/worker
	cd services/realtime && go build -o ../../bin/realtime ./cmd/realtime
	cd agent/shippulse-agent && go build -o ../../bin/shippulse-agent ./cmd/agent
	@echo "==> Build complete."

dev:
	docker compose up -d postgres redis nats clickhouse otel-collector
	@echo "==> Infrastructure started. Running control-plane in background..."

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down -v

test:
	@echo "==> Running Go unit and integration tests..."
	cd packages/contracts && go test -v ./...
	cd services/control-plane && go test -v ./...
	cd services/ingest && go test -v ./...
	cd services/worker && go test -v ./...
	cd agent/shippulse-agent && go test -v ./...

lint:
	@echo "==> Running Go vet..."
	cd services/control-plane && go vet ./...
	cd agent/shippulse-agent && go vet ./...

seed:
	@echo "==> Seeding database with demo data..."
	cd services/control-plane && go run ./cmd/seed

clean:
	rm -rf bin/
