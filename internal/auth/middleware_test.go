package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
)

// stubLookup returns a fixed account, or ErrUserNotFound when absent.
type stubLookup struct {
	user    auth.Account
	missing bool
	err     error
}

func (s stubLookup) LookupForAuth(context.Context, string) (auth.Account, error) {
	if s.err != nil {
		return auth.Account{}, s.err
	}
	if s.missing {
		return auth.Account{}, auth.ErrUserNotFound
	}
	return s.user, nil
}

func activeUser() auth.Account {
	return auth.Account{
		ID:               "user-1",
		TenantID:         "tenant-1",
		Username:         "alice",
		DisplayName:      "Alice",
		Role:             model.RoleUser,
		Status:           model.StatusActive,
		OrganizationID:   "org-1",
		OrganizationName: "Engineering",
		TokenVersion:     1,
	}
}

// okHandler records that the request made it through the middleware.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		httpx.OK(w, nil)
	})
}

func doRequest(h http.Handler, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not the standard envelope: %v", err)
	}
	return env.Code
}

func TestRequireAuthAcceptsValidToken(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, err := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
	rec := doRequest(mw.RequireAuth(okHandler(&reached)), "Bearer "+raw)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (code=%s)", rec.Code, errorCode(t, rec))
	}
	if !reached {
		t.Error("handler was not reached")
	}
}

func TestRequireAuthPopulatesPrincipal(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)

	var got auth.Principal
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.MustPrincipal(r.Context())
		httpx.OK(w, nil)
	})

	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
	doRequest(mw.RequireAuth(handler), "Bearer "+raw)

	if got.UserID != "user-1" || got.Username != "alice" {
		t.Errorf("principal = %+v, want the looked-up user", got)
	}
}

// The principal must reflect stored state, not the token's snapshot: a role
// change since the token was minted has to take effect immediately.
func TestRequireAuthPrefersStoredRoleOverTokenClaim(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	user := testUser()
	user.Role = model.RoleUser
	raw, _, _ := tokens.Issue(user, "acme", testSessionID, 1, time.Hour)

	promoted := activeUser()
	promoted.Role = model.RoleSuperAdmin

	var got auth.Principal
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.MustPrincipal(r.Context())
		httpx.OK(w, nil)
	})

	mw := auth.NewMiddleware(tokens, stubLookup{user: promoted}, liveSessions{})
	doRequest(mw.RequireAuth(handler), "Bearer "+raw)

	if got.Role != model.RoleSuperAdmin {
		t.Errorf("role = %q, want the stored SUPER_ADMIN rather than the token's USER", got.Role)
	}
}

func TestRequireAuthRejectsBadRequests(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	validRaw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)
	expiredRaw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, -time.Minute)

	tests := []struct {
		name       string
		header     string
		lookup     stubLookup
		wantStatus int
		wantCode   string
	}{
		{
			name:       "no header",
			header:     "",
			lookup:     stubLookup{user: activeUser()},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "MISSING_TOKEN",
		},
		{
			name:       "wrong scheme",
			header:     "Basic dXNlcjpwYXNz",
			lookup:     stubLookup{user: activeUser()},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "MALFORMED_AUTHORIZATION",
		},
		{
			name:       "bearer with no token",
			header:     "Bearer ",
			lookup:     stubLookup{user: activeUser()},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "MALFORMED_AUTHORIZATION",
		},
		{
			name:       "garbage token",
			header:     "Bearer not-a-real-token",
			lookup:     stubLookup{user: activeUser()},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "INVALID_TOKEN",
		},
		{
			name:       "expired token",
			header:     "Bearer " + expiredRaw,
			lookup:     stubLookup{user: activeUser()},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "TOKEN_EXPIRED",
		},
		{
			name:       "deleted account",
			header:     "Bearer " + validRaw,
			lookup:     stubLookup{missing: true},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "INVALID_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			mw := auth.NewMiddleware(tokens, tt.lookup, liveSessions{})
			rec := doRequest(mw.RequireAuth(okHandler(&reached)), tt.header)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := errorCode(t, rec); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
			if reached {
				t.Error("handler was reached despite a rejected request")
			}
		})
	}
}

// Disabling an account must invalidate live sessions at once (§3.6), not at
// token expiry.
func TestDisabledAccountIsRejectedImmediately(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)

	disabled := activeUser()
	disabled.Status = model.StatusDisabled

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: disabled}, liveSessions{})
	rec := doRequest(mw.RequireAuth(okHandler(&reached)), "Bearer "+raw)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := errorCode(t, rec); got != "ACCOUNT_DISABLED" {
		t.Errorf("code = %q, want ACCOUNT_DISABLED", got)
	}
	if reached {
		t.Error("a disabled account reached the handler")
	}
}

// A password change or logout bumps token_version; every token minted before
// that must stop working.
func TestStaleTokenVersionIsRevoked(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)

	rotated := activeUser()
	rotated.TokenVersion = 2 // the user logged out or changed their password

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: rotated}, liveSessions{})
	rec := doRequest(mw.RequireAuth(okHandler(&reached)), "Bearer "+raw)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := errorCode(t, rec); got != "TOKEN_REVOKED" {
		t.Errorf("code = %q, want TOKEN_REVOKED", got)
	}
	if reached {
		t.Error("a revoked token reached the handler")
	}
}

// The account lookup behind this middleware is the one query in the system
// that cannot be scoped by tenant, since it is what establishes the tenant.
// Comparing the account's tenant against the token's claim is what stops
// that from widening what a token can reach, so it needs a test of its own:
// without the check, a token forged with another tenant's id would be
// accepted and every subsequent query would run against that tenant.
func TestTokenForAnotherTenantIsRejected(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)

	elsewhere := testUser()
	elsewhere.TenantID = "tenant-2"
	raw, _, _ := tokens.Issue(elsewhere, "other", testSessionID, 1, time.Hour)

	var reached bool
	// The stored account says tenant-1; the token claims tenant-2.
	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
	rec := doRequest(mw.RequireAuth(okHandler(&reached)), "Bearer "+raw)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := errorCode(t, rec); got != "INVALID_TOKEN" {
		t.Errorf("code = %q, want INVALID_TOKEN", got)
	}
	if reached {
		t.Error("a token naming another tenant reached the handler")
	}
}

// The principal handlers act on must carry the tenant, since it is what
// every query below them is scoped by. An empty one would silently match no
// rows and look like an empty account list.
func TestPrincipalCarriesTheTenant(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)

	var got auth.Principal
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.MustPrincipal(r.Context())
		httpx.OK(w, nil)
	})

	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
	doRequest(mw.RequireAuth(capture), "Bearer "+raw)

	if got.TenantID != "tenant-1" {
		t.Errorf("principal tenant = %q, want tenant-1", got.TenantID)
	}
}

func TestRequireAdmin(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)

	t.Run("admin passes", func(t *testing.T) {
		admin := activeUser()
		admin.Role = model.RoleSuperAdmin

		var reached bool
		mw := auth.NewMiddleware(tokens, stubLookup{user: admin}, liveSessions{})
		rec := doRequest(mw.RequireAuth(mw.RequireAdmin(okHandler(&reached))), "Bearer "+raw)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if !reached {
			t.Error("admin did not reach the handler")
		}
	})

	t.Run("normal user is forbidden", func(t *testing.T) {
		var reached bool
		mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
		rec := doRequest(mw.RequireAuth(mw.RequireAdmin(okHandler(&reached))), "Bearer "+raw)

		// 403, not 401: we know who they are, they are just not allowed.
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if got := errorCode(t, rec); got != "ADMIN_REQUIRED" {
			t.Errorf("code = %q, want ADMIN_REQUIRED", got)
		}
		if reached {
			t.Error("a normal user reached an admin-only handler")
		}
	})

	// If RequireAdmin is ever mounted without RequireAuth in front of it,
	// it must fail closed rather than let the request through.
	t.Run("fails closed without a principal", func(t *testing.T) {
		var reached bool
		mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
		rec := doRequest(mw.RequireAdmin(okHandler(&reached)), "Bearer "+raw)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if reached {
			t.Error("a misconfigured route let the request through")
		}
	})
}

// testSessionID is the session every token in these tests names.
const testSessionID = "11111111-1111-4111-8111-111111111111"

// liveSessions accepts any session.
//
// The session check has its own tests in internal/server, against a real
// database. Here it must not be the thing under test, or every case above
// would be asserting two properties at once and a failure would not say
// which.
type liveSessions struct{}

func (liveSessions) CheckSession(context.Context, string, string) error { return nil }

// deadSessions is the opposite, for the one case that is about the check.
type deadSessions struct{}

func (deadSessions) CheckSession(context.Context, string, string) error {
	return auth.ErrSessionNotLive
}

// A revoked session must stop the token, even though the account is fine and
// the signature verifies.
//
// That is the whole reason sessions exist here: the account-level checks —
// status, token_version, tenant — cannot express "this one sign-in and not
// the others", so before this the only revocation available was all of them.
func TestRevokedSessionRejectsAnOtherwiseValidToken(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, err := tokens.Issue(testUser(), "acme", testSessionID, 1, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	live := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, liveSessions{})
	if got := statusThrough(live, raw); got != http.StatusOK {
		t.Fatalf("with a live session, status = %d, want 200", got)
	}

	dead := auth.NewMiddleware(tokens, stubLookup{user: activeUser()}, deadSessions{})
	if got := statusThrough(dead, raw); got != http.StatusUnauthorized {
		t.Errorf("with a revoked session, status = %d, want 401 — the account "+
			"and the signature are both still good, so nothing else would "+
			"have stopped it", got)
	}
}

// statusThrough runs one authenticated request and returns the status.
func statusThrough(mw *auth.Middleware, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec.Code
}
