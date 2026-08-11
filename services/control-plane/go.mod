module github.com/tatitoxt/shippulse/services/control-plane

go 1.22

replace github.com/tatitoxt/shippulse/packages/contracts => ../../packages/contracts
replace github.com/tatitoxt/shippulse/packages/sdk => ../../packages/sdk

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.5.5
	github.com/nats-io/nats.go v1.34.1
	github.com/redis/go-redis/v9 v9.5.1
	github.com/tatitoxt/shippulse/packages/contracts v0.0.0
	golang.org/x/crypto v0.22.0
)
