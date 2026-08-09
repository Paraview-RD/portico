// Package handler contains the HTTP handlers. Handlers parse and validate
// the wire format, delegate to a service, and render the result; business
// rules live in the service layer.
//
// Every handler is tenant-scoped. Authenticated handlers take the tenant
// from auth.Principal; the handful of public ones resolve it from the
// request through resolvePublicTenant. Nothing else may.
package handler

import (
	"github.com/paraview/portico/internal/casp"
	"github.com/paraview/portico/internal/oidcp"
	"github.com/paraview/portico/internal/samlp"
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
	sessions *service.SessionService

	// The three kinds of registered application, one per protocol, behind
	// the console's application management.
	clients          *service.OAuthClientService
	serviceProviders *service.SAMLServiceProviderService
	samlKeys         *service.SAMLKeyService
	casServices      *service.CASService
	// The credentials a directory authenticates with. The SCIM API itself
	// lives in internal/scim; this layer only issues and revokes.
	scimCredentials *service.SCIMCredentialService
	// Directories accounts are pulled out of, which is the opposite
	// direction from the SCIM credentials above: those let a directory push,
	// this reaches out and reads.
	directories *service.DirectoryService
	// Outbound event subscriptions.
	webhooks *service.WebhookService
	// Groups: sets of people, as distinct from the organization chart.
	groups *service.GroupService
	// oidc is here for one endpoint: the seam where Portico's own sign-in
	// hands a person back to the OpenID Provider. The provider's own
	// endpoints do not go through this layer at all.
	oidc *oidcp.Providers
	// saml is here for the same reason, one endpoint further along.
	saml *samlp.Providers
	// cas is the third, and the last.
	cas *casp.Server
}

// New returns a Handler backed by the given services.
func New(
	users *service.UserService,
	orgs *service.OrganizationService,
	audit *service.AuditService,
	settings *service.SettingsService,
	tenants *service.TenantService,
	recovery *service.RecoveryService,
	sessions *service.SessionService,
	clients *service.OAuthClientService,
	serviceProviders *service.SAMLServiceProviderService,
	samlKeys *service.SAMLKeyService,
	casServices *service.CASService,
	scimCredentials *service.SCIMCredentialService,
	directories *service.DirectoryService,
	webhooks *service.WebhookService,
	groups *service.GroupService,
	oidc *oidcp.Providers,
	samlProviders *samlp.Providers,
	casServer *casp.Server,
) *Handler {
	return &Handler{
		users: users, orgs: orgs, audit: audit,
		settings: settings, tenants: tenants, recovery: recovery,
		sessions: sessions,
		clients:  clients, serviceProviders: serviceProviders,
		samlKeys: samlKeys, casServices: casServices,
		scimCredentials: scimCredentials, directories: directories,
		webhooks: webhooks, groups: groups,
		oidc: oidc, saml: samlProviders, cas: casServer,
	}
}
