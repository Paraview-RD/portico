package oidcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
)

// Storage must satisfy what the protocol library drives. Asserted here so a
// signature drifting from the interface is a build failure rather than a nil
// panic on the first authorization request.
var (
	_ op.Storage     = (*Storage)(nil)
	_ op.AuthStorage = (*Storage)(nil)
	_ op.OPStorage   = (*Storage)(nil)
)

// TenantPathPrefix is where a tenant's endpoints live.
//
// The tenant is in the path because it is in the issuer, and it is in the
// issuer because that is what makes a token minted for one tenant unusable
// against another: a relying party checks `iss` and fetches the key set it
// names. Carrying the tenant in a claim instead would work only if every
// integrator wrote code to check that claim, and no standard library does.
const TenantPathPrefix = "/t/"

// Providers builds and caches one OpenID Provider per tenant.
//
// Lazily, because most tenants never federate and an RSA keygen for each one
// at startup would be a cost paid for nothing. Cached, because the provider
// carries a compiled router and a key set that should not be rebuilt on
// every request.
type Providers struct {
	publicURL string
	store     *store.Store
	tenants   *service.TenantService
	users     *service.UserService
	clients   *service.OAuthClientService
	keys      *service.SigningKeyService
	// cryptoKey encrypts the codes the library hands to clients. It is
	// derived from the deployment's signing secret so it survives restarts —
	// a random one would invalidate every authorization in flight whenever
	// the process bounced.
	cryptoKey [32]byte

	mu    sync.RWMutex
	cache map[string]*op.Provider
}

// NewProviders wires the factory.
func NewProviders(
	publicURL string,
	cryptoKey [32]byte,
	st *store.Store,
	tenants *service.TenantService,
	users *service.UserService,
	clients *service.OAuthClientService,
	keys *service.SigningKeyService,
) *Providers {
	return &Providers{
		publicURL: strings.TrimSuffix(publicURL, "/"),
		cryptoKey: cryptoKey,
		store:     st,
		tenants:   tenants,
		users:     users,
		clients:   clients,
		keys:      keys,
		cache:     map[string]*op.Provider{},
	}
}

// Issuer is the issuer identifier for a tenant.
func (p *Providers) Issuer(tenantCode string) string {
	return p.publicURL + TenantPathPrefix + tenantCode
}

// LoginURL is where a browser is sent to sign in for an authorization
// request. It is Portico's own sign-in screen, told which request it is
// completing and which tenant it belongs to.
func (p *Providers) LoginURL(tenantCode, authRequestID string) string {
	query := url.Values{}
	query.Set("tenant", tenantCode)
	query.Set("auth_request", authRequestID)
	return p.publicURL + "/login?" + query.Encode()
}

// CallbackURL is where the sign-in screen returns once somebody has
// authenticated, which is what resumes the protocol flow.
func (p *Providers) CallbackURL(tenantCode, authRequestID string) string {
	return p.Issuer(tenantCode) + "/authorize/callback?id=" + url.QueryEscape(authRequestID)
}

// For returns the provider serving a tenant, building it on first use.
func (p *Providers) For(ctx context.Context, tenantCode string) (*op.Provider, model.Tenant, error) {
	tenant, err := p.tenants.Resolve(ctx, tenantCode)
	if err != nil {
		return nil, model.Tenant{}, err
	}

	p.mu.RLock()
	cached, ok := p.cache[tenant.ID]
	p.mu.RUnlock()
	if ok {
		return cached, tenant, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if cached, ok := p.cache[tenant.ID]; ok {
		return cached, tenant, nil
	}

	provider, err := p.build(tenant)
	if err != nil {
		return nil, model.Tenant{}, err
	}
	p.cache[tenant.ID] = provider
	return provider, tenant, nil
}

// Storage returns the adapter for a tenant, which the login bridge needs in
// order to mark a request authorized.
func (p *Providers) Storage(tenant model.Tenant) *Storage {
	return NewStorage(tenant, p.store, p.users, p.clients, p.keys,
		func(authRequestID string) string { return p.LoginURL(tenant.Code, authRequestID) })
}

func (p *Providers) build(tenant model.Tenant) (*op.Provider, error) {
	issuer := p.Issuer(tenant.Code)

	config := &op.Config{
		CryptoKey: p.cryptoKey,

		// S256 only. "plain" gives no protection against an attacker who can
		// read the authorization request, which is the attacker PKCE exists
		// for, and OAuth 2.1 removes it.
		CodeMethodS256: true,

		// Both ways of presenting a client secret, since which one a library
		// uses is not something the deployment controls.
		AuthMethodPost: true,
		// private_key_jwt is not implemented; advertising it would invite a
		// client to configure something that then fails.
		AuthMethodPrivateKeyJWT: false,

		GrantTypeRefreshToken:  true,
		RequestObjectSupported: false,

		SupportedScopes: []string{
			"openid", "profile", "email", "phone", "offline_access",
		},
		SupportedClaims: []string{
			"sub", "aud", "exp", "iat", "iss", "auth_time", "nonce", "azp",
			"name", "preferred_username", "updated_at",
			"email", "email_verified", "phone_number", "phone_number_verified",
			// Portico's own, which §3.8.2 asks the token to carry.
			"tenant_id", "tenant_code", "role", "organization_id", "organization_name",
		},

		// Portico serves plain HTTP behind a proxy that terminates TLS, and
		// says so everywhere else; the issuer is whatever PORTICO_PUBLIC_URL
		// declares. Allowing an insecure issuer here would let a
		// misconfigured deployment advertise http:// to relying parties,
		// which the library is right to refuse.
		DefaultLogoutRedirectURI: p.publicURL + "/login",
	}

	// StaticIssuer rather than deriving it from the request host: behind a
	// proxy the Host header is whatever the proxy sends, and an issuer taken
	// from it would let a caller choose the domain that appears in tokens.
	// PORTICO_PUBLIC_URL is the declared answer.
	provider, err := op.NewProvider(config, p.Storage(tenant), op.StaticIssuer(issuer),
		// Portico serves plain HTTP and documents that it must sit behind a
		// TLS-terminating proxy; without this a http:// public URL — which is
		// what every local run uses — refuses to start.
		op.WithAllowInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("build provider for tenant %s: %w", tenant.Code, err)
	}
	return provider, nil
}

// Handler serves a tenant's federation endpoints, with the tenant taken from
// the path.
//
// It is a plain http.Handler rather than a router entry per endpoint,
// because the protocol library owns its own routing and the set of paths is
// its business, not this application's.
func (p *Providers) Handler(tenantCode string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider, _, err := p.For(r.Context(), tenantCode)
		if err != nil {
			http.Error(w, "unknown tenant", http.StatusNotFound)
			return
		}
		provider.ServeHTTP(w, r)
	})
}
