package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
)

// Account is the subset of a user record the middleware needs in order to
// decide whether a presented token is still valid.
type Account struct {
	ID               string
	TenantID         string
	Username         string
	DisplayName      string
	Role             model.Role
	Status           model.Status
	OrganizationID   string
	OrganizationName string
	TokenVersion     int64
}

// ErrUserNotFound is returned by a UserLookup when no such account exists.
var ErrUserNotFound = errors.New("user not found")

// UserLookup fetches the current state of an account. The middleware calls
// it on every authenticated request, which is what makes revocation take
// effect immediately rather than at token expiry.
type UserLookup interface {
	LookupForAuth(ctx context.Context, userID string) (Account, error)
}

// Middleware authenticates requests using bearer tokens.
type Middleware struct {
	tokens *TokenService
	users  UserLookup
}

// NewMiddleware returns a Middleware that verifies tokens with tokens and
// resolves accounts with users.
func NewMiddleware(tokens *TokenService, users UserLookup) *Middleware {
	return &Middleware{tokens: tokens, users: users}
}

// RequireAuth rejects requests without a valid, unrevoked token, and puts
// the resulting Principal in the request context.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := m.authenticate(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

// RequireAdmin rejects callers without the administrator role. It must be
// chained after RequireAuth.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok {
			// The route is misconfigured. Fail closed rather than letting
			// the request through.
			httpx.Fail(w, r, httpx.Unauthorized("UNAUTHENTICATED", "Authentication is required."))
			return
		}
		if !principal.IsAdmin() {
			httpx.Fail(w, r, httpx.Forbidden("ADMIN_REQUIRED", "This action requires administrator privileges."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) authenticate(r *http.Request) (Principal, error) {
	raw, err := bearerToken(r)
	if err != nil {
		return Principal{}, err
	}

	claims, err := m.tokens.Verify(raw)
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			return Principal{}, httpx.Unauthorized("TOKEN_EXPIRED", "Your session has expired. Please sign in again.")
		}
		return Principal{}, httpx.Unauthorized("INVALID_TOKEN", "The provided token is not valid.")
	}

	// Re-read the account so a disable, password change, or logout that
	// happened after the token was issued takes effect right away.
	user, err := m.users.LookupForAuth(r.Context(), claims.Subject)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return Principal{}, httpx.Unauthorized("INVALID_TOKEN", "The provided token is not valid.")
		}
		return Principal{}, httpx.Internal(err)
	}

	if user.Status != model.StatusActive {
		return Principal{}, httpx.Unauthorized("ACCOUNT_DISABLED", "This account has been disabled.")
	}
	if user.TokenVersion != claims.TokenVersion {
		return Principal{}, httpx.Unauthorized("TOKEN_REVOKED", "Your session is no longer valid. Please sign in again.")
	}

	// The account lookup is the one read in the system that cannot be scoped
	// by tenant, because it is what establishes the tenant. Comparing the
	// stored tenant against the one the token was minted for closes the gap:
	// a token can only ever act inside the tenant it was issued in, whatever
	// else about the account has changed since.
	//
	// A mismatch is not a state this system produces — a user's tenant never
	// changes — so it means a forged or tampered token, and is reported as
	// one rather than distinguished for the caller's benefit.
	if user.TenantID != claims.TenantID {
		return Principal{}, httpx.Unauthorized("INVALID_TOKEN", "The provided token is not valid.")
	}

	// Prefer the stored values over the token's copy: role or organization
	// may have changed since the token was minted.
	return Principal{
		UserID:           user.ID,
		TenantID:         user.TenantID,
		Username:         user.Username,
		DisplayName:      user.DisplayName,
		Role:             user.Role,
		OrganizationID:   user.OrganizationID,
		OrganizationName: user.OrganizationName,
	}, nil
}

func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", httpx.Unauthorized("MISSING_TOKEN", "Authentication is required.")
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", httpx.Unauthorized("MALFORMED_AUTHORIZATION", "Authorization header must be 'Bearer <token>'.")
	}
	return strings.TrimSpace(token), nil
}
