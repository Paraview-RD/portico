// Package provision performs the operations that have no HTTP surface.
//
// Tenants are created, listed, and disabled from the command line rather
// than through the API. That is a deliberate consequence of V0.1 having no
// cross-tenant role (docs/requirements/v0.1-requirements.md §3.1): if no
// account can act outside its own tenant, then no account can create one,
// and the capability has to live somewhere a signed-in user is not. Whoever
// can run the binary against the database can provision; that is the same
// group who could do it with psql anyway.
package provision

import (
	"context"
	"fmt"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
)

// Provisioner holds the services the command-line operations need. It opens
// the database directly and never builds an HTTP stack.
type Provisioner struct {
	store   *store.Store
	tenants *service.TenantService
	users   *service.UserService
	clients *service.OAuthClientService
	keys    *service.SigningKeyService
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

	return &Provisioner{
		store:   st,
		tenants: service.NewTenantService(st),
		users:   service.NewUserService(st, audit, settings, tokens),
		clients: service.NewOAuthClientService(st),
		keys:    service.NewSigningKeyService(st),
	}, nil
}

// Close releases the connection pool.
func (p *Provisioner) Close() error { return p.store.Close() }

// NewTenant is the outcome of creating a tenant.
type NewTenant struct {
	Tenant model.Tenant
	// AdminUsername is the administrator created alongside the tenant.
	AdminUsername string
	// AdminPassword is set only when one was generated, in which case this
	// is the only time it is ever available.
	AdminPassword string
}

// CreateTenant creates a tenant and its first administrator.
//
// The two go together on purpose: a tenant with no administrator cannot be
// signed into and cannot be given one through the API, so creating it alone
// would produce something inert that looks like it should work.
func (p *Provisioner) CreateTenant(ctx context.Context, code, name, adminUsername, adminPassword string) (NewTenant, error) {
	tenant, err := p.tenants.Create(ctx, code, name)
	if err != nil {
		return NewTenant{}, err
	}

	if adminUsername == "" {
		adminUsername = "admin"
	}

	_, generated, err := p.users.EnsureInitialAdmin(ctx, tenant.ID, adminUsername, adminPassword)
	if err != nil {
		// The tenant row survives. Removing it here would need a delete path
		// that exists for no other reason, and re-running with the same code
		// is not a fix either — so say plainly what state things are in.
		return NewTenant{}, fmt.Errorf(
			"tenant %q was created but its administrator was not (%w); "+
				"create the account manually or drop the tenant row", code, err)
	}

	return NewTenant{
		Tenant:        tenant,
		AdminUsername: adminUsername,
		AdminPassword: generated,
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
// Registered here for the same reason tenants are: deciding who may ask this
// server for tokens about its users is an administrative act, and V0.1 has
// no role that could be authorized to perform it over HTTP. Dynamic client
// registration is deliberately absent.

// RegisterClient adds a relying party to a tenant.
func (p *Provisioner) RegisterClient(ctx context.Context, tenantCode string, in service.RegisterClientInput) (service.RegisteredClient, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return service.RegisteredClient{}, err
	}
	return p.clients.Register(ctx, tenant.ID, in)
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
	return p.clients.SetStatus(ctx, tenant.ID, clientID, status)
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
