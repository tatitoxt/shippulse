package config

import (
	"os"
)

type Config struct {
	Port         string
	DatabaseURL  string
	RedisURL     string
	NatsURL      string
	JWTSecret    string
	Environment  string
	GithubSecret string
	WebOrigin    string
}

func Load() *Config {
	return &Config{
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		RedisURL:     getEnv("REDIS_URL", "redis:6379"),
		NatsURL:      getEnv("NATS_URL", "nats://nats:4222"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		Environment:  getEnv("ENV", "development"),
		GithubSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		WebOrigin:    getEnv("WEB_ORIGIN", "https://example.invalid"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
