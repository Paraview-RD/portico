// Package config loads runtime configuration from environment variables.
//
// Every setting has a usable default except PORTICO_DB_DSN, which has none
// and cannot: a connection string for somebody else's database is not
// something to guess. Unset, the server reports it and exits at startup.
//
// PORTICO_JWT_SECRET is the other one to set deliberately, though it does
// not stop a start: a random secret is generated when it is unset, which is
// fine for a first run and invalidates every token on restart.
//
// An earlier version of this comment said an empty environment started a
// working instance. That was true while storage was a file; it stopped being
// true when it moved to PostgreSQL.
package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/secrets"
)

// MinJWTSecretLength is the shortest signing secret the server will accept.
// 32 bytes matches the output of the `openssl rand -hex 32` the docs
// recommend and leaves HS256 well outside offline brute-force range.
const MinJWTSecretLength = 32

// Config holds all runtime settings.
type Config struct {
	// Addr is the TCP address the HTTP server listens on.
	Addr string

	// MetricsAddr is the address a Prometheus endpoint listens on, or "" to
	// publish no metrics at all.
	//
	// A second listener rather than a route on the first, and off unless
	// asked for, because /metrics is not an authenticated endpoint anywhere
	// in this ecosystem — Prometheus does not authenticate, and every
	// exporter assumes the address is reachable only from inside. Serving it
	// on the public port would make it exactly as reachable as the login
	// page, and the mistake would be invisible: a scrape config that works.
	//
	// Bind it to a private interface (127.0.0.1:9410, or the pod's own
	// address), and never publish this port through the same proxy that
	// serves the application.
	MetricsAddr string

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

	// TrialSignup registers the self-service trial routes, which let somebody
	// with no account at all create a tenant by proving an email address.
	//
	// Off, and it has to stay off anywhere holding real staff. Every other
	// write in this API is authorized inside a tenant by somebody who already
	// signed in; these three are the only ones reachable by a stranger, and
	// the only ones that create a tenant — which the rest of this system
	// deliberately leaves to the command line, on the grounds that no
	// cross-tenant role exists for an API to authorize. Enabling this is
	// declaring the deployment a demonstration.
	//
	// It also needs SMTP: the address is the only thing being proven, so a
	// server that cannot send mail cannot run this, and says so at startup
	// rather than accepting requests it will not finish.
	TrialSignup bool

	// TrialMaxTenants caps how many trial tenants may exist at once. Reached,
	// the signup form says so rather than queueing. A demonstration database
	// is small and shared, and the failure without a cap is not a bill — it
	// is the demonstration becoming unusably slow for everybody.
	TrialMaxTenants int

	// TrustProxyHeaders makes the server believe X-Forwarded-For and
	// X-Real-Ip. Enable it only when a proxy you control sits in front and
	// rewrites those headers; otherwise callers can forge their own audit
	// log IP.
	TrustProxyHeaders bool

	// AuthRateLimit is how many writes one client address may make under
	// /api/v1/auth/ per minute, and AuthRateLimitBurst how many of that
	// allowance may arrive at once. Zero disables the limiter entirely.
	//
	// On by default, which is the point: the throttle that matters belongs in
	// the reverse proxy, but the proxy is a deployment decision and this is
	// not. A first run, a demonstration, a container somebody exposed to try
	// it out — all of them reach the sign-in endpoint with nothing in front,
	// and signing in costs a password hash whatever the answer.
	//
	// The burst is the number that was measured rather than chosen. Ten was
	// the first guess and the browser suite refused it: forty-odd sign-ins
	// from one address inside a minute, which is also what one office behind
	// one NAT address looks like at nine in the morning. The sustained rate
	// is what bounds the CPU an attacker can buy; the burst only decides how
	// many people may arrive together, so raising it costs little and being
	// wrong about it costs a working deployment.
	AuthRateLimit      int
	AuthRateLimitBurst int

	// PublicURL is where people reach this deployment, used to build the
	// links in password-recovery messages.
	//
	// It cannot be derived from a request: behind a reverse proxy the Host
	// header is whatever that proxy sends, and building a link from it would
	// let anyone who can reach the server choose the domain a reset link
	// points at. So it is configuration, and recovery links are wrong rather
	// than dangerous when it is unset.
	PublicURL string

	// SMTP is the mail relay password-recovery messages go through. An empty
	// Host means email recovery is not available on this deployment, which
	// is the default — the binary must run with no environment at all.
	SMTP notify.SMTPConfig

	// DefaultLocale is the language of a message sent to somebody whose own
	// preference and whose tenant's default both say nothing.
	//
	// It is the last stop before English, and it exists because a deployment
	// serving one country should not have to set a preference on every
	// account to stop sending English mail. A tenant that disagrees
	// overrides it in its own settings; an account that disagrees overrides
	// that.
	//
	// An unshipped tag is refused at startup rather than ignored. A
	// deployment that set this meant something by it, and silently
	// continuing in English is how a setting gets called broken years later.
	DefaultLocale string

	// EncryptionKey protects the few credentials the server has to store and
	// later use, rather than merely verify — today that is a directory
	// connector's bind password.
	//
	// Nil means no key was configured, which is the default and starts
	// normally. Nothing silently falls back to storing such a credential in
	// the clear; the request to save one is refused, with the reason.
	//
	// Deliberately not derived from JWTSecret. They protect different things
	// and leak through different accidents — a signing key ends up in a JWT
	// debugging session, a data key ends up in a database dump — and one
	// value doing both jobs means either leak costs both.
	EncryptionKey []byte
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:            envString("PORTICO_ADDR", ":8410"),
		MetricsAddr:     os.Getenv("PORTICO_METRICS_ADDR"),
		DatabaseDriver:  envString("PORTICO_DB_DRIVER", "postgres"),
		DatabaseDSN:     os.Getenv("PORTICO_DB_DSN"),
		LogLevel:        envString("PORTICO_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,

		InitialAdminUsername: envString("PORTICO_INITIAL_ADMIN_USERNAME", "admin"),
		InitialAdminPassword: os.Getenv("PORTICO_INITIAL_ADMIN_PASSWORD"),
		TrustProxyHeaders:    os.Getenv("PORTICO_TRUST_PROXY_HEADERS") == "true",
		TrialSignup:          os.Getenv("PORTICO_TRIAL_SIGNUP") == "true",
	}

	rateLimit, err := envInt("PORTICO_AUTH_RATE_LIMIT", 60)
	if err != nil {
		return nil, err
	}
	rateLimitBurst, err := envInt("PORTICO_AUTH_RATE_LIMIT_BURST", 30)
	if err != nil {
		return nil, err
	}
	cfg.AuthRateLimit, cfg.AuthRateLimitBurst = rateLimit, rateLimitBurst

	trialMax, err := envInt("PORTICO_TRIAL_MAX_TENANTS", 50)
	if err != nil {
		return nil, err
	}
	cfg.TrialMaxTenants = trialMax

	cfg.PublicURL = envString("PORTICO_PUBLIC_URL", "http://localhost:8410")

	cfg.DefaultLocale = envString("PORTICO_DEFAULT_LOCALE", string(i18n.Default))
	if _, ok := i18n.Parse(cfg.DefaultLocale); !ok {
		return nil, fmt.Errorf(
			"PORTICO_DEFAULT_LOCALE is %q, which this build has no messages for. Available: %s",
			cfg.DefaultLocale, joinLocales(i18n.Supported()))
	}

	smtpPort, err := envInt("PORTICO_SMTP_PORT", 587)
	if err != nil {
		return nil, err
	}
	cfg.SMTP = notify.SMTPConfig{
		Host:       os.Getenv("PORTICO_SMTP_HOST"),
		Port:       smtpPort,
		Username:   os.Getenv("PORTICO_SMTP_USERNAME"),
		Password:   os.Getenv("PORTICO_SMTP_PASSWORD"),
		From:       os.Getenv("PORTICO_SMTP_FROM"),
		Encryption: envString("PORTICO_SMTP_ENCRYPTION", notify.EncryptionSTARTTLS),
	}
	switch cfg.SMTP.Encryption {
	case notify.EncryptionSTARTTLS, notify.EncryptionTLS, notify.EncryptionNone:
	default:
		return nil, fmt.Errorf(
			"PORTICO_SMTP_ENCRYPTION is %q; it must be one of starttls, tls, none",
			cfg.SMTP.Encryption)
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

	if err := cfg.loadEncryptionKey(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadEncryptionKey reads PORTICO_ENCRYPTION_KEY, which is 32 bytes of hex.
//
// Hex rather than raw bytes because this is a key and not a passphrase:
// `openssl rand -hex 32` produces one, and accepting arbitrary text would
// invite a memorable phrase where 256 bits of entropy is the whole point.
// There is no stretching here to rescue a weak input, deliberately — adding
// one would make a passphrase look acceptable.
func (c *Config) loadEncryptionKey() error {
	encoded := os.Getenv("PORTICO_ENCRYPTION_KEY")
	if encoded == "" {
		return nil
	}

	key, err := hex.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf(
			"PORTICO_ENCRYPTION_KEY is not hexadecimal. Generate one with: openssl rand -hex %d",
			secrets.KeyLength)
	}
	if len(key) != secrets.KeyLength {
		return fmt.Errorf(
			"PORTICO_ENCRYPTION_KEY decodes to %d bytes; it must be exactly %d. Generate one with: openssl rand -hex %d",
			len(key), secrets.KeyLength, secrets.KeyLength)
	}

	// Refusing this is worth a line of code: reusing the signing secret as
	// the data key means a captured token that reveals one reveals both, and
	// it is the shortcut somebody takes at 2am when a deployment will not
	// start.
	if subtle.ConstantTimeCompare(key, c.JWTSecret) == 1 {
		return errors.New(
			"PORTICO_ENCRYPTION_KEY is the same value as PORTICO_JWT_SECRET. " +
				"They protect different things and must not share a value; " +
				"generate a second one with: openssl rand -hex 32")
	}

	c.EncryptionKey = key
	return nil
}

// joinLocales renders the available locales for an error message, so
// somebody who set an unshipped tag is told what they could have set.
func joinLocales(locales []i18n.Locale) string {
	out := make([]string, len(locales))
	for i, locale := range locales {
		out[i] = string(locale)
	}
	return strings.Join(out, ", ")
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number, got %q", key, v)
	}
	return n, nil
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
