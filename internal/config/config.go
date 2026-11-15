// Package config loads process settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime settings for the API and worker processes.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	WorkerPollInterval time.Duration
	WorkerLease        time.Duration
	CORSOrigins        []string
	SeedDemo           bool
}

// Load reads configuration from the environment with sensible local defaults.
func Load() Config {
	return Config{
		DatabaseURL:        getenv("DATABASE_URL", "postgres://attest:attest@localhost:5432/attest?sslmode=disable"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		JWTSecret:          getenv("JWT_SECRET", "dev-only-change-me-attest-secret"),
		AccessTokenTTL:     durationEnv("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:    durationEnv("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		WorkerPollInterval: durationEnv("WORKER_POLL_INTERVAL", time.Second),
		WorkerLease:        durationEnv("WORKER_LEASE_SECONDS", 30*time.Second),
		CORSOrigins:        splitCSV(getenv("CORS_ORIGINS", "http://localhost:3000")),
		SeedDemo:           boolEnv("SEED_DEMO", true),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	// Allow plain seconds for WORKER_LEASE_SECONDS style values.
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func boolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
