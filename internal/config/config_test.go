package config_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/paraview/portico/internal/config"
)

// A weak signing secret is the difference between "signed" and "forgeable":
// HS256 with a short key can be brute-forced offline from one captured
// token, and the forged token can claim any user and any role.
func TestLoadRejectsShortJWTSecret(t *testing.T) {
	tooShort := []string{
		"x",
		"ci-secret",
		"password",
		strings.Repeat("a", config.MinJWTSecretLength-1),
	}

	for _, secret := range tooShort {
		t.Run(secret, func(t *testing.T) {
			t.Setenv("PORTICO_JWT_SECRET", secret)

			_, err := config.Load()
			if err == nil {
				t.Fatalf("a %d-byte secret was accepted", len(secret))
			}
			// The error has to say how to fix it; "invalid config" would
			// send the operator to the source.
			if !strings.Contains(err.Error(), "openssl rand") {
				t.Errorf("error does not say how to generate one: %v", err)
			}
		})
	}
}

func TestLoadAcceptsSufficientJWTSecret(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("a", config.MinJWTSecretLength))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("a secret at the minimum length was rejected: %v", err)
	}
	if cfg.JWTSecretGenerated {
		t.Error("an explicitly supplied secret was reported as generated")
	}
}

// An unset secret still starts — first-run friction matters — but must be
// randomly generated and flagged, never silently weak.
func TestLoadGeneratesSecretWhenUnset(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", "")

	first, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !first.JWTSecretGenerated {
		t.Error("generated secret was not flagged, so nothing will warn the operator")
	}
	if len(first.JWTSecret) < config.MinJWTSecretLength {
		t.Errorf("generated secret is %d bytes, want at least %d",
			len(first.JWTSecret), config.MinJWTSecretLength)
	}

	second, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(first.JWTSecret) == string(second.JWTSecret) {
		t.Error("two generated secrets are identical; they are not random")
	}
}

// Proxy headers must be opt-in: believing them by default lets any caller
// forge the IP recorded in the audit log.
func TestTrustProxyHeadersDefaultsOff(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("a", config.MinJWTSecretLength))
	t.Setenv("PORTICO_TRUST_PROXY_HEADERS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TrustProxyHeaders {
		t.Error("proxy headers are trusted by default")
	}
}

func TestTrustProxyHeadersOptIn(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("a", config.MinJWTSecretLength))
	t.Setenv("PORTICO_TRUST_PROXY_HEADERS", "true")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.TrustProxyHeaders {
		t.Error("explicit opt-in did not take effect")
	}
}

func TestTokenTTLParsing(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("a", config.MinJWTSecretLength))

	tests := []struct {
		value       string
		wantMinutes float64
		wantError   bool
	}{
		{"30m", 30, false},
		{"2h", 120, false},
		{"3600", 60, false}, // bare seconds
		{"not-a-duration", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("PORTICO_TOKEN_TTL", tt.value)

			cfg, err := config.Load()
			if tt.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.TokenTTL.Minutes() != tt.wantMinutes {
				t.Errorf("TTL = %v, want %v minutes", cfg.TokenTTL, tt.wantMinutes)
			}
		})
	}
}

// The data key and the signing key must be different values.
//
// Reusing one for both is the shortcut somebody takes when a deployment will
// not start, and it means a captured token that reveals the signing secret
// also decrypts every stored credential.
func TestEncryptionKeyMayNotBeTheSigningSecret(t *testing.T) {
	shared := strings.Repeat("a", 32)
	t.Setenv("PORTICO_JWT_SECRET", shared)
	t.Setenv("PORTICO_ENCRYPTION_KEY", hex.EncodeToString([]byte(shared)))

	if _, err := config.Load(); err == nil {
		t.Fatal("the same value was accepted as both the signing secret and " +
			"the data key; one leak would then cost both")
	}
}

func TestEncryptionKeyMustBeThirtyTwoHexBytes(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("j", 32))

	for _, value := range []string{
		"not-hex-at-all",
		hex.EncodeToString([]byte(strings.Repeat("k", 16))), // too short
		hex.EncodeToString([]byte(strings.Repeat("k", 64))), // too long
	} {
		t.Setenv("PORTICO_ENCRYPTION_KEY", value)
		if _, err := config.Load(); err == nil {
			t.Errorf("PORTICO_ENCRYPTION_KEY=%q was accepted", value)
		}
	}
}

// And an unset key starts normally. Encryption is not a prerequisite for
// running; it is a prerequisite for storing a credential, which is refused
// at that point with a reason.
func TestNoEncryptionKeyStartsAnyway(t *testing.T) {
	t.Setenv("PORTICO_JWT_SECRET", strings.Repeat("j", 32))
	t.Setenv("PORTICO_ENCRYPTION_KEY", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load with no encryption key: %v", err)
	}
	if cfg.EncryptionKey != nil {
		t.Error("an unset key produced a key")
	}
}
