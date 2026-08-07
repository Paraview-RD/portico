package oidcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/paraview/portico/internal/auth"
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

// Providers builds and caches one OpenID Provider per mount.
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
	audit     *service.AuditService
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
	audit *service.AuditService,
) *Providers {
	return &Providers{
		publicURL: strings.TrimSuffix(publicURL, "/"),
		cryptoKey: cryptoKey,
		store:     st,
		tenants:   tenants,
		users:     users,
		clients:   clients,
		keys:      keys,
		audit:     audit,
		cache:     map[string]*op.Provider{},
	}
}

// TenantMount is the path a tenant's endpoints hang off.
func TenantMount(tenantCode string) string { return TenantPathPrefix + tenantCode }

// Issuer is the issuer identifier for a mount.
func (p *Providers) Issuer(mount string) string { return p.publicURL + mount }

// LoginURL is where a browser is sent to sign in for an authorization
// request. It is Portico's own sign-in screen, told which request it is
// completing and which tenant it belongs to.
//
// Where the browser goes afterwards is not in this URL: it comes from the
// stored request's issuer, so that a mount cannot be chosen by whoever holds
// the link.
func (p *Providers) LoginURL(tenantCode, authRequestID string) string {
	query := url.Values{}
	if tenantCode != model.DefaultTenantCode {
		query.Set("tenant", tenantCode)
	}
	query.Set("auth_request", authRequestID)
	return p.publicURL + "/login?" + query.Encode()
}

// For returns the provider serving a mount, building it on first use. An
// empty mount is the root alias, which serves the default tenant.
func (p *Providers) For(ctx context.Context, mount string) (*op.Provider, error) {
	code := model.DefaultTenantCode
	if mount != "" {
		code = strings.TrimPrefix(mount, TenantPathPrefix)
	}

	tenant, err := p.tenants.Resolve(ctx, code)
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	cached, ok := p.cache[mount]
	p.mu.RUnlock()
	if ok {
		return cached, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if cached, ok := p.cache[mount]; ok {
		return cached, nil
	}

	provider, err := p.build(tenant, mount)
	if err != nil {
		return nil, err
	}
	p.cache[mount] = provider
	return provider, nil
}

// storage returns the adapter for a tenant at a mount.
func (p *Providers) storage(tenant model.Tenant, mount string) *Storage {
	return NewStorage(tenant, p.Issuer(mount), p.store, p.users, p.clients, p.keys,
		func(authRequestID string) string { return p.LoginURL(tenant.Code, authRequestID) })
}

func (p *Providers) build(tenant model.Tenant, mount string) (*op.Provider, error) {
	issuer := p.Issuer(mount)

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

		// Where a relying party's end-session request lands when it names no
		// redirect of its own: Portico's own sign-in screen, which is the
		// only page a just-signed-out person can use.
		DefaultLogoutRedirectURI: p.publicURL + "/login",
	}

	// StaticIssuer rather than deriving it from the request host: behind a
	// proxy the Host header is whatever the proxy sends, and an issuer taken
	// from it would let a caller choose the domain that appears in tokens.
	// PORTICO_PUBLIC_URL is the declared answer.
	provider, err := op.NewProvider(config, p.storage(tenant, mount), op.StaticIssuer(issuer),
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

// Handler serves the federation endpoints under a mount.
//
// It is a plain http.Handler rather than a router entry per endpoint,
// because the protocol library owns its own routing and the set of paths is
// its business, not this application's.
func (p *Providers) Handler(mount string) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider, err := p.For(r.Context(), mount)
		if err != nil {
			http.Error(w, "unknown tenant", http.StatusNotFound)
			return
		}
		provider.ServeHTTP(w, r)
	})
	if mount == "" {
		return inner
	}
	// The provider routes relative to its own issuer, so the mount has to
	// come off the path before it sees the request.
	return http.StripPrefix(mount, inner)
}

// The ways completing an authorization request can fail, as values rather
// than as HTTP errors: this package sits beside Portico's API rather than
// inside it, and the handler that calls Complete is where a status code
// belongs.
//
// ErrWrongTenant is separate from ErrAuthRequestNotFound on purpose. It is
// what somebody sees after signing in perfectly successfully to the wrong
// tenant, and "unknown or expired request" would read as a Portico fault
// rather than as something they can act on.
var (
	ErrWrongTenant         = errors.New("oidcp: the authorization request belongs to another tenant")
	ErrAuthRequestNotFound = errors.New("oidcp: no such authorization request")
	ErrClientNotFound      = errors.New("oidcp: the client is no longer registered")
	ErrClientDisabled      = errors.New("oidcp: the client is disabled")
)

// Authorization is where a browser goes once a person has signed in, which
// resumes the protocol flow the sign-in interrupted.
type Authorization struct {
	// RedirectTo is the provider's own callback, not the relying party's:
	// the library issues the authorization code there and redirects onward
	// itself.
	RedirectTo string `json:"redirectTo"`
	// ClientName is shown by the sign-in screen, so a person knows what
	// they are being signed in to.
	ClientName string `json:"clientName"`
}

// Complete marks an authorization request as belonging to a signed-in
// person, and returns where to send the browser.
//
// This is the one part of the flow the protocol library does not drive.
// Everything the library needs to hand a code to the relying party is in the
// stored request; what it cannot know is who is at the keyboard, because
// that is Portico's own sign-in, not the protocol's.
func (p *Providers) Complete(ctx context.Context, actor auth.Principal, authRequestID, ip string) (Authorization, error) {
	q := p.store.ForTenant(actor.TenantID)

	row, err := q.GetAuthRequest(ctx, authRequestID, store.Now())
	if err != nil {
		if store.IsNoRows(err) {
			// It may exist under a different tenant, which is a different
			// problem with a different remedy.
			if p.existsElsewhere(ctx, authRequestID) {
				return Authorization{}, ErrWrongTenant
			}
			return Authorization{}, ErrAuthRequestNotFound
		}
		return Authorization{}, fmt.Errorf("get authorization request: %w", err)
	}

	// A client disabled between /authorize and sign-in must fail here.
	// Redirecting onward would hand the relying party a code that dies at
	// the token endpoint, with an error nobody can trace back to this.
	client, err := p.clients.Get(ctx, actor.TenantID, row.ClientID)
	if err != nil {
		return Authorization{}, ErrClientNotFound
	}
	if client.Status != model.StatusActive {
		return Authorization{}, ErrClientDisabled
	}

	if err := q.CompleteAuthRequest(ctx, row.ID, actor.UserID, store.Now(), []string{"pwd"}); err != nil {
		return Authorization{}, fmt.Errorf("complete authorization request: %w", err)
	}

	// Who authorized what, and when. An identity provider that cannot answer
	// that is not one anybody should deploy.
	p.audit.Log(ctx, actor.TenantID, service.AuditEntry{
		Kind: model.LogLogin, Action: model.ActionAuthorize,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "OAUTH_CLIENT", TargetID: client.ClientID, TargetName: client.Name,
		Detail: "scopes: " + strings.Join(row.Scopes, " "),
		IP:     ip,
	})

	return Authorization{
		// The issuer comes from the row rather than from the caller: the same
		// tenant is reachable at two mounts, and only the request knows which
		// one it arrived at.
		RedirectTo: row.Issuer + "/authorize/callback?id=" + url.QueryEscape(row.ID),
		ClientName: client.Name,
	}, nil
}

// existsElsewhere reports whether an authorization request exists in some
// other tenant. It is only ever asked after a scoped lookup has missed, and
// only to tell two indistinguishable failures apart.
func (p *Providers) existsElsewhere(ctx context.Context, authRequestID string) bool {
	tenants, err := p.tenants.List(ctx)
	if err != nil {
		return false
	}
	for _, tenant := range tenants {
		if _, err := p.store.ForTenant(tenant.ID).GetAuthRequest(ctx, authRequestID, store.Now()); err == nil {
			return true
		}
	}
	return false
}

// SweepExpired deletes authorization requests nobody completed.
//
// Every arrival at /authorize writes a row, and most deployments will have
// far more abandoned sign-ins than finished ones, so this is the fastest
// growing table Portico has. The sweep is per tenant because the query is —
// there is deliberately no way to write across tenants.
func (p *Providers) SweepExpired(ctx context.Context) error {
	tenants, err := p.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	now := store.Now()
	for _, tenant := range tenants {
		if err := p.store.ForTenant(tenant.ID).DeleteExpiredAuthRequests(ctx, now); err != nil {
			return fmt.Errorf("sweep tenant %s: %w", tenant.Code, err)
		}
	}
	return nil
}
