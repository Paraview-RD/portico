package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/model"
)

var testSecret = []byte("test-secret-not-used-anywhere-real")

func testUser() model.User {
	return model.User{
		ID:               "user-1",
		TenantID:         "tenant-1",
		Username:         "alice",
		DisplayName:      "Alice",
		Role:             model.RoleUser,
		OrganizationID:   "org-1",
		OrganizationName: "Engineering",
	}
}

func TestIssueThenVerifyRoundTrip(t *testing.T) {
	svc := auth.NewTokenService(testSecret)
	user := testUser()

	token, expiresAt, err := svc.Issue(user, "acme", 7, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned an empty token")
	}
	if time.Until(expiresAt) <= 0 {
		t.Errorf("expiry %v is not in the future", expiresAt)
	}

	claims, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.Subject != user.ID {
		t.Errorf("subject = %q, want %q", claims.Subject, user.ID)
	}
	if claims.Username != user.Username {
		t.Errorf("username = %q, want %q", claims.Username, user.Username)
	}
	if claims.Role != user.Role {
		t.Errorf("role = %q, want %q", claims.Role, user.Role)
	}
	// The organization travels in the token so a downstream system can read
	// it without calling back (§3.6.1).
	if claims.OrganizationID != user.OrganizationID {
		t.Errorf("organizationId = %q, want %q", claims.OrganizationID, user.OrganizationID)
	}
	if claims.OrganizationName != user.OrganizationName {
		t.Errorf("organizationName = %q, want %q", claims.OrganizationName, user.OrganizationName)
	}
	if claims.TokenVersion != 7 {
		t.Errorf("tokenVersion = %d, want 7", claims.TokenVersion)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	svc := auth.NewTokenService(testSecret)

	token, _, err := svc.Issue(testUser(), "acme", 1, -time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = svc.Verify(token)
	if !errors.Is(err, auth.ErrTokenExpired) {
		t.Errorf("error = %v, want ErrTokenExpired", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	issuer := auth.NewTokenService(testSecret)
	verifier := auth.NewTokenService([]byte("a-completely-different-secret"))

	token, _, err := issuer.Issue(testUser(), "acme", 1, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := verifier.Verify(token); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("error = %v, want ErrTokenInvalid", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	svc := auth.NewTokenService(testSecret)

	token, _, err := svc.Issue(testUser(), "acme", 1, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Flip a character in the payload segment; the signature must no longer
	// match.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	payload := []byte(parts[1])
	if payload[0] == 'a' {
		payload[0] = 'b'
	} else {
		payload[0] = 'a'
	}
	tampered := parts[0] + "." + string(payload) + "." + parts[2]

	if _, err := svc.Verify(tampered); err == nil {
		t.Error("a tampered token was accepted")
	}
}

// The "alg":"none" attack: a token that claims to need no signature must be
// rejected outright.
func TestVerifyRejectsUnsignedToken(t *testing.T) {
	svc := auth.NewTokenService(testSecret)

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, auth.Claims{
		Username: "attacker",
		Role:     model.RoleSuperAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    auth.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	raw, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned token: %v", err)
	}

	if _, err := svc.Verify(raw); err == nil {
		t.Fatal("an unsigned token was accepted; the alg check is not working")
	}
}

func TestVerifyRejectsForeignIssuer(t *testing.T) {
	svc := auth.NewTokenService(testSecret)

	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "some-other-system",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	raw, err := foreign.SignedString(testSecret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := svc.Verify(raw); err == nil {
		t.Error("a token from another issuer was accepted")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	svc := auth.NewTokenService(testSecret)

	for _, raw := range []string{"", "not-a-token", "a.b.c", "...."} {
		if _, err := svc.Verify(raw); err == nil {
			t.Errorf("Verify(%q) succeeded, want an error", raw)
		}
	}
}
