package auth

import (
	"context"

	"github.com/paraview/keylite/internal/model"
)

type contextKey string

const principalKey contextKey = "principal"

// Principal is the authenticated caller behind the current request.
type Principal struct {
	UserID           string
	Username         string
	DisplayName      string
	Role             model.Role
	OrganizationID   string
	OrganizationName string
}

// IsAdmin reports whether the caller holds the administrator role.
func (p Principal) IsAdmin() bool { return p.Role.IsAdmin() }

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFrom returns the authenticated caller in ctx. The boolean is
// false on routes that do not require authentication.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// MustPrincipal returns the authenticated caller, panicking if there is
// none. Handlers behind RequireAuth can use this: reaching them without a
// principal means the route is misconfigured, which is a bug rather than a
// runtime condition to handle.
func MustPrincipal(ctx context.Context) Principal {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		panic("auth: no principal in context; is this route behind RequireAuth?")
	}
	return p
}
