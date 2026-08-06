package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paraview/keylite/internal/auth"
	"github.com/paraview/keylite/internal/httpx"
	"github.com/paraview/keylite/internal/model"
)

// stubLookup returns a fixed account, or ErrUserNotFound when absent.
type stubLookup struct {
	user    auth.AuthUser
	missing bool
	err     error
}

func (s stubLookup) LookupForAuth(context.Context, string) (auth.AuthUser, error) {
	if s.err != nil {
		return auth.AuthUser{}, s.err
	}
	if s.missing {
		return auth.AuthUser{}, auth.ErrUserNotFound
	}
	return s.user, nil
}

func activeUser() auth.AuthUser {
	return auth.AuthUser{
		ID:               "user-1",
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	raw, _, err := tokens.Issue(testUser(), 1, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()})
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
	raw, _, _ := tokens.Issue(testUser(), 1, time.Hour)

	var got auth.Principal
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.MustPrincipal(r.Context())
		httpx.OK(w, nil)
	})

	mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()})
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
	raw, _, _ := tokens.Issue(user, 1, time.Hour)

	promoted := activeUser()
	promoted.Role = model.RoleSuperAdmin

	var got auth.Principal
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.MustPrincipal(r.Context())
		httpx.OK(w, nil)
	})

	mw := auth.NewMiddleware(tokens, stubLookup{user: promoted})
	doRequest(mw.RequireAuth(handler), "Bearer "+raw)

	if got.Role != model.RoleSuperAdmin {
		t.Errorf("role = %q, want the stored SUPER_ADMIN rather than the token's USER", got.Role)
	}
}

func TestRequireAuthRejectsBadRequests(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	validRaw, _, _ := tokens.Issue(testUser(), 1, time.Hour)
	expiredRaw, _, _ := tokens.Issue(testUser(), 1, -time.Minute)

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
			mw := auth.NewMiddleware(tokens, tt.lookup)
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
	raw, _, _ := tokens.Issue(testUser(), 1, time.Hour)

	disabled := activeUser()
	disabled.Status = model.StatusDisabled

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: disabled})
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
	raw, _, _ := tokens.Issue(testUser(), 1, time.Hour)

	rotated := activeUser()
	rotated.TokenVersion = 2 // the user logged out or changed their password

	var reached bool
	mw := auth.NewMiddleware(tokens, stubLookup{user: rotated})
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

func TestRequireAdmin(t *testing.T) {
	tokens := auth.NewTokenService(testSecret)
	raw, _, _ := tokens.Issue(testUser(), 1, time.Hour)

	t.Run("admin passes", func(t *testing.T) {
		admin := activeUser()
		admin.Role = model.RoleSuperAdmin

		var reached bool
		mw := auth.NewMiddleware(tokens, stubLookup{user: admin})
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
		mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()})
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
		mw := auth.NewMiddleware(tokens, stubLookup{user: activeUser()})
		rec := doRequest(mw.RequireAdmin(okHandler(&reached)), "Bearer "+raw)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if reached {
			t.Error("a misconfigured route let the request through")
		}
	})
}
