// Package config reads process configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Int(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func Duration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

const (
	DefaultDatabaseURL = "postgres://shippulse:shippulse@localhost:5432/shippulse?sslmode=disable"
	DefaultNATSURL     = "nats://localhost:4222"
	DefaultRuntimeURL  = "http://localhost:8090"
)

func DatabaseURL() string { return String("DATABASE_URL", DefaultDatabaseURL) }
func NATSURL() string     { return String("NATS_URL", DefaultNATSURL) }
func RuntimeURL() string  { return String("SIMRUNTIME_URL", DefaultRuntimeURL) }
