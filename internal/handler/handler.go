// Package handler contains the HTTP handlers. Handlers parse and validate
// the wire format, delegate to a service, and render the result; business
// rules live in the service layer.
//
// Every handler is tenant-scoped. Authenticated handlers take the tenant
// from auth.Principal; the handful of public ones resolve it from the
// request through resolvePublicTenant. Nothing else may.
package handler

import (
	"github.com/paraview/portico/internal/oidcp"
	"github.com/paraview/portico/internal/service"
)

// Handler holds the services the HTTP layer delegates to.
type Handler struct {
	users    *service.UserService
	orgs     *service.OrganizationService
	audit    *service.AuditService
	settings *service.SettingsService
	tenants  *service.TenantService
	recovery *service.RecoveryService
	// oidc is here for one endpoint: the seam where Portico's own sign-in
	// hands a person back to the OpenID Provider. The provider's own
	// endpoints do not go through this layer at all.
	oidc *oidcp.Providers
}

// New returns a Handler backed by the given services.
func New(
	users *service.UserService,
	orgs *service.OrganizationService,
	audit *service.AuditService,
	settings *service.SettingsService,
	tenants *service.TenantService,
	recovery *service.RecoveryService,
	oidc *oidcp.Providers,
) *Handler {
	return &Handler{
		users: users, orgs: orgs, audit: audit,
		settings: settings, tenants: tenants, recovery: recovery,
		oidc: oidc,
	}
}
