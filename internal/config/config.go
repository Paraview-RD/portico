// Package config loads runtime configuration from environment variables.
//
// Every setting has a usable default so that running the binary with no
// environment at all starts a working single-node instance. The only
// exception is PORTICO_JWT_SECRET: a random secret is generated at startup
// when it is unset, which is fine for a first run but invalidates all
// tokens on restart, so production deployments must set it explicitly.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"
)

// MinJWTSecretLength is the shortest signing secret the server will accept.
// 32 bytes matches the output of the `openssl rand -hex 32` the docs
// recommend and leaves HS256 well outside offline brute-force range.
const MinJWTSecretLength = 32

// Config holds all runtime settings.
type Config struct {
	// Addr is the TCP address the HTTP server listens on.
	Addr string

	// DatabaseDriver selects the storage backend ("postgres").
	DatabaseDriver string

	// DatabaseDSN is the PostgreSQL connection string, in URL form
	// (postgres://user:pass@host:5432/db?sslmode=disable) or keyword form.
	DatabaseDSN string

	// JWTSecret signs and verifies access tokens.
	JWTSecret []byte

	// JWTSecretGenerated reports whether JWTSecret was randomly generated
	// because none was configured. Callers should warn when true.
	JWTSecretGenerated bool

	// TokenTTL is how long an issued access token stays valid. This is the
	// startup default; it can be overridden at runtime via system settings.
	TokenTTL time.Duration

	// LogLevel is one of "debug", "info", "warn", "error".
	LogLevel string

	// ShutdownTimeout bounds how long in-flight requests may finish during
	// graceful shutdown.
	ShutdownTimeout time.Duration

	// InitialAdminUsername and InitialAdminPassword bootstrap the first
	// administrator on an empty database (§3.10). When the password is
	// empty a random one is generated and logged once.
	InitialAdminUsername string
	InitialAdminPassword string

	// TrustProxyHeaders makes the server believe X-Forwarded-For and
	// X-Real-Ip. Enable it only when a proxy you control sits in front and
	// rewrites those headers; otherwise callers can forge their own audit
	// log IP.
	TrustProxyHeaders bool
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:            envString("PORTICO_ADDR", ":8410"),
		DatabaseDriver:  envString("PORTICO_DB_DRIVER", "postgres"),
		DatabaseDSN:     os.Getenv("PORTICO_DB_DSN"),
		LogLevel:        envString("PORTICO_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,

		InitialAdminUsername: envString("PORTICO_INITIAL_ADMIN_USERNAME", "admin"),
		InitialAdminPassword: os.Getenv("PORTICO_INITIAL_ADMIN_PASSWORD"),
		TrustProxyHeaders:    os.Getenv("PORTICO_TRUST_PROXY_HEADERS") == "true",
	}

	ttl, err := envDuration("PORTICO_TOKEN_TTL", 2*time.Hour)
	if err != nil {
		return nil, err
	}
	cfg.TokenTTL = ttl

	secret := os.Getenv("PORTICO_JWT_SECRET")
	if secret == "" {
		generated, err := randomSecret()
		if err != nil {
			return nil, fmt.Errorf("generate jwt secret: %w", err)
		}
		cfg.JWTSecret = []byte(generated)
		cfg.JWTSecretGenerated = true
	} else {
		// A short secret is the difference between "signed" and "signable by
		// anyone with a captured token": HS256 with a low-entropy key is
		// brute-forceable offline, and recovering it lets an attacker mint a
		// token claiming any user and any role. Refuse to start rather than
		// silently accepting one, and do not fall back to a generated secret
		// — that would hide the misconfiguration instead of surfacing it.
		if len(secret) < MinJWTSecretLength {
			return nil, fmt.Errorf(
				"PORTICO_JWT_SECRET is %d bytes; it must be at least %d. Generate one with: openssl rand -hex 32",
				len(secret), MinJWTSecretLength)
		}
		cfg.JWTSecret = []byte(secret)
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	// Accept both a Go duration ("30m") and a bare number of seconds.
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: want a duration like %q or a number of seconds, got %q", key, "30m", v)
	}
	return time.Duration(secs) * time.Second, nil
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
