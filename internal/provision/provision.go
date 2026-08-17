// Package provision performs the operations available from the command
// line.
//
// Tenant provisioning is the one that has no HTTP surface at all. Tenants
// are created, listed, and disabled from the command line rather than
// through the API. That is a deliberate consequence of V0.1 having no
// cross-tenant role (docs/requirements/v0.1-requirements.md §3.1): if no
// account can act outside its own tenant, then no account can create one,
// and the capability has to live somewhere a signed-in user is not. Whoever
// can run the binary against the database can provision; that is the same
// group who could do it with psql anyway.
package provision

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
)

// Provisioner holds the services the command-line operations need. It opens
// the database directly and never builds an HTTP stack.
type Provisioner struct {
	store   *store.Store
	tenants *service.TenantService
	users   *service.UserService
	clients *service.OAuthClientService
	keys    *service.SigningKeyService

	serviceProviders *service.SAMLServiceProviderService
	samlKeys         *service.SAMLKeyService
	casServices      *service.CASService
}

// Open connects to the database named by cfg and applies any pending
// migrations, exactly as starting the server would.
func Open(cfg *config.Config) (*Provisioner, error) {
	st, err := store.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	audit := service.NewAuditService(st)
	settings := service.NewSettingsService(st, cfg.TokenTTL)
	// A token service is required to construct the user service but is never
	// exercised here: provisioning issues no sessions.
	tokens := auth.NewTokenService(cfg.JWTSecret)

	// No metrics registry: this is a CLI process that exits, and a counter
	// nothing will ever scrape is not worth the allocation. The service
	// tolerates nil for exactly this case.
	users := service.NewUserService(st, audit, settings, tokens, nil)

	return &Provisioner{
		store:   st,
		tenants: service.NewTenantService(st),
		users:   users,
		clients: service.NewOAuthClientService(st, audit),
		keys:    service.NewSigningKeyService(st),

		serviceProviders: service.NewSAMLServiceProviderService(st, audit),
		samlKeys:         service.NewSAMLKeyService(st),
		casServices:      service.NewCASService(st, users, audit),
	}, nil
}

// Close releases the connection pool.
func (p *Provisioner) Close() error { return p.store.Close() }

// NewTenant is the outcome of creating a tenant.
type NewTenant struct {
	Tenant model.Tenant
	// AdminUsername is the administrator created alongside the tenant.
	AdminUsername string
	// AdminPassword is set only when the caller chose none, in which case
	// the account took the documented default and must replace it before it
	// can be signed into. When the caller supplied one this is empty —
	// repeating a password back to whoever just typed it says nothing.
	AdminPassword string
}

// CreateTenant creates a tenant and its first administrator.
//
// The two go together on purpose: a tenant with no administrator cannot be
// signed into and cannot be given one through the API, so creating it alone
// would produce something inert that looks like it should work.
func (p *Provisioner) CreateTenant(ctx context.Context, code, name, adminUsername, adminPassword string) (NewTenant, error) {
	// No deadline. A tenant somebody provisions by hand is not on trial.
	tenant, err := p.tenants.Create(ctx, code, name, nil)
	if err != nil {
		return NewTenant{}, err
	}

	if adminUsername == "" {
		adminUsername = "admin"
	}

	_, tookTheDefault, err := p.users.EnsureInitialAdmin(ctx, tenant.ID, adminUsername, adminPassword)
	if err != nil {
		// The tenant row survives. Removing it here would need a delete path
		// that exists for no other reason, and re-running with the same code
		// is not a fix either — so say plainly what state things are in.
		return NewTenant{}, fmt.Errorf(
			"tenant %q was created but its administrator was not (%w); "+
				"create the account manually or drop the tenant row", code, err)
	}

	password := ""
	if tookTheDefault {
		password = service.DefaultInitialAdminPassword
	}

	return NewTenant{
		Tenant:        tenant,
		AdminUsername: adminUsername,
		AdminPassword: password,
	}, nil
}

// ListTenants returns every tenant.
func (p *Provisioner) ListTenants(ctx context.Context) ([]model.Tenant, error) {
	return p.tenants.List(ctx)
}

// SetTenantStatus enables or disables a tenant by code. Disabling refuses
// sign-in but keeps every record, so it is reversible.
func (p *Provisioner) SetTenantStatus(ctx context.Context, code string, status model.Status) (model.Tenant, error) {
	return p.tenants.SetStatus(ctx, code, status)
}

// --- relying parties ------------------------------------------------------
//
// Also available to a tenant administrator through the API and the console;
// these are the command-line equivalents, which is what a first deployment
// needs before anybody has signed in, and what remains reachable if the
// console cannot be. Both paths go through the same service, so the rules
// and the audit trail are the same either way.
//
// Dynamic client registration (RFC 7591) — registration by an anonymous
// caller, with no administrator in the loop — is deliberately absent.

// RegisterClient adds a relying party to a tenant.
func (p *Provisioner) RegisterClient(ctx context.Context, tenantCode string, in service.RegisterClientInput) (service.RegisteredClient, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return service.RegisteredClient{}, err
	}
	return p.clients.Register(ctx, service.CommandLineActor(tenant.ID), in)
}

// ListClients returns every relying party in a tenant.
func (p *Provisioner) ListClients(ctx context.Context, tenantCode string) ([]model.OAuthClient, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return nil, err
	}
	return p.clients.List(ctx, tenant.ID)
}

// SetClientStatus enables or disables a relying party.
func (p *Provisioner) SetClientStatus(ctx context.Context, tenantCode, clientID string, status model.Status) (model.OAuthClient, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return model.OAuthClient{}, err
	}
	return p.clients.SetStatus(ctx, service.CommandLineActor(tenant.ID), clientID, status)
}

// RotateSigningKey retires a tenant's current signing key and generates a
// replacement. The old key stays in the published key set until the tokens
// it signed have expired.
func (p *Provisioner) RotateSigningKey(ctx context.Context, tenantCode string) (string, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return "", err
	}
	key, err := p.keys.Rotate(ctx, tenant.ID)
	if err != nil {
		return "", err
	}
	return key.ID, nil
}

// --- SAML service providers -----------------------------------------------
//
// On the same terms as relying parties: the console can do this too, and
// both paths go through the same service.

// RegisterServiceProvider adds a SAML service provider to a tenant.
func (p *Provisioner) RegisterServiceProvider(ctx context.Context, tenantCode string, in service.RegisterSPInput) (model.SAMLServiceProvider, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}
	return p.serviceProviders.Register(ctx, service.CommandLineActor(tenant.ID), in)
}

// ListServiceProviders returns every SAML service provider in a tenant.
func (p *Provisioner) ListServiceProviders(ctx context.Context, tenantCode string) ([]model.SAMLServiceProvider, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return nil, err
	}
	return p.serviceProviders.List(ctx, tenant.ID)
}

// SetServiceProviderStatus enables or disables a SAML service provider.
func (p *Provisioner) SetServiceProviderStatus(ctx context.Context, tenantCode, entityID string, status model.Status) (model.SAMLServiceProvider, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}
	return p.serviceProviders.SetStatus(ctx, service.CommandLineActor(tenant.ID), entityID, status)
}

// SAMLCertificate returns a tenant's active SAML signing certificate, in
// PEM, generating one if the tenant has none.
func (p *Provisioner) SAMLCertificate(ctx context.Context, tenantCode string) (service.SAMLKey, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return service.SAMLKey{}, err
	}
	return p.samlKeys.Active(ctx, tenant.ID)
}

// RotateSAMLCertificate retires a tenant's SAML certificate and generates a
// replacement.
//
// Nothing is deleted and nothing is automatic. Every service provider has to
// be reconfigured with the new certificate by hand, so the previous one stays
// listed until an operator says otherwise.
func (p *Provisioner) RotateSAMLCertificate(ctx context.Context, tenantCode string) (service.SAMLKey, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return service.SAMLKey{}, err
	}
	return p.samlKeys.Rotate(ctx, tenant.ID)
}

// --- CAS services ---------------------------------------------------------

// RegisterCASService adds a CAS service to a tenant.
func (p *Provisioner) RegisterCASService(ctx context.Context, tenantCode string, in service.RegisterCASInput) (model.CASService, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return model.CASService{}, err
	}
	return p.casServices.Register(ctx, service.CommandLineActor(tenant.ID), in)
}

// ListCASServices returns every CAS service in a tenant.
func (p *Provisioner) ListCASServices(ctx context.Context, tenantCode string) ([]model.CASService, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return nil, err
	}
	return p.casServices.List(ctx, tenant.ID)
}

// SetCASServiceStatus enables or disables a CAS service.
func (p *Provisioner) SetCASServiceStatus(ctx context.Context, tenantCode, prefix string, status model.Status) (model.CASService, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return model.CASService{}, err
	}
	return p.casServices.SetStatus(ctx, service.CommandLineActor(tenant.ID), prefix, status)
}
