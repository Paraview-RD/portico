// Package samlp adapts Portico to the SAML 2.0 Identity Provider role.
//
// Everything here is glue, on the same terms as internal/oidcp: the protocol
// library owns the wire format, the XML canonicalization, and the
// signatures, and this package answers the questions it asks about accounts,
// service providers, and keys. Signature construction and verification are
// never implemented here — that is the rule this whole stage is built
// around, because a hand-rolled XML signature check is the single most
// reliable way to ship a SAML implementation that accepts forged
// assertions.
//
// One instance is bound to one tenant, for the same reason store.Scoped is:
// each tenant is its own issuer with its own certificate, and an adapter
// that could reach two of them would make cross-tenant assertion confusion a
// matter of getting a parameter right.
package samlp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/crewjam/saml"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Paths a tenant's SAML endpoints hang off, relative to its issuer.
const (
	MetadataPath = "/saml/metadata"
	SSOPath      = "/saml/sso"
	// CallbackPath is where the sign-in screen returns, which resumes the
	// flow it interrupted.
	CallbackPath = "/saml/sso/callback"
)

// TenantPathPrefix mirrors the OpenID Provider's, so a tenant reaches all
// its protocols under one prefix.
const TenantPathPrefix = "/t/"

// TenantMount is the path a tenant's endpoints hang off.
func TenantMount(tenantCode string) string { return TenantPathPrefix + tenantCode }

// The ways completing an authentication request can fail, as values: this
// package sits beside Portico's API rather than inside it, so the handler
// that calls Complete is where a status code belongs.
var (
	ErrWrongTenant         = errors.New("samlp: the request belongs to another tenant")
	ErrAuthRequestNotFound = errors.New("samlp: no such authentication request")
	ErrAuthRequestTaken    = errors.New("samlp: the request was completed by somebody else")
	ErrProviderDisabled    = errors.New("samlp: the service provider is disabled")
)

// Providers builds and caches one Identity Provider per mount.
type Providers struct {
	publicURL string
	store     *store.Store
	tenants   *service.TenantService
	users     *service.UserService
	providers *service.SAMLServiceProviderService
	keys      *service.SAMLKeyService
	audit     *service.AuditService
}

// NewProviders wires the factory.
func NewProviders(
	publicURL string,
	st *store.Store,
	tenants *service.TenantService,
	users *service.UserService,
	providers *service.SAMLServiceProviderService,
	keys *service.SAMLKeyService,
	audit *service.AuditService,
) *Providers {
	return &Providers{
		publicURL: strings.TrimSuffix(publicURL, "/"),
		store:     st,
		tenants:   tenants,
		users:     users,
		providers: providers,
		keys:      keys,
		audit:     audit,
	}
}

// Issuer is the entity id a tenant's assertions are issued by. It is the
// metadata URL, which is what SAML conventionally uses and what a service
// provider will already have on file after being handed the document.
func (p *Providers) Issuer(mount string) string {
	return p.publicURL + mount + MetadataPath
}

// LoginURL is where a browser is sent to sign in for an authentication
// request.
func (p *Providers) LoginURL(tenantCode, authRequestID string) string {
	query := url.Values{}
	if tenantCode != model.DefaultTenantCode {
		query.Set("tenant", tenantCode)
	}
	query.Set("saml_request", authRequestID)
	return p.publicURL + "/login?" + query.Encode()
}

// For returns the Identity Provider serving a mount. An empty mount is the
// root alias, which serves the default tenant.
//
// Built per request rather than cached, unlike the OpenID Provider, which
// carries a compiled router worth keeping. This one is a struct with two
// parsed URLs and a key the key service already holds parsed — so building
// it costs almost nothing, and caching it would mean a certificate rotated
// from the command line did not take effect until somebody restarted the
// server. An identity provider that keeps signing with a certificate an
// operator has retired is worse than an allocation.
func (p *Providers) For(ctx context.Context, mount string) (*saml.IdentityProvider, model.Tenant, error) {
	code := model.DefaultTenantCode
	if mount != "" {
		code = strings.TrimPrefix(mount, TenantPathPrefix)
	}

	tenant, err := p.tenants.Resolve(ctx, code)
	if err != nil {
		return nil, model.Tenant{}, err
	}

	idp, err := p.build(ctx, tenant, mount)
	if err != nil {
		return nil, model.Tenant{}, err
	}
	return idp, tenant, nil
}

func (p *Providers) build(ctx context.Context, tenant model.Tenant, mount string) (*saml.IdentityProvider, error) {
	key, err := p.keys.Active(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("SAML signing key for tenant %s: %w", tenant.Code, err)
	}

	metadataURL, err := url.Parse(p.publicURL + mount + MetadataPath)
	if err != nil {
		return nil, fmt.Errorf("build metadata URL: %w", err)
	}
	ssoURL, err := url.Parse(p.publicURL + mount + SSOPath)
	if err != nil {
		return nil, fmt.Errorf("build SSO URL: %w", err)
	}

	return &saml.IdentityProvider{
		Key:         key.Private,
		Certificate: key.Certificate,
		MetadataURL: *metadataURL,
		SSOURL:      *ssoURL,
		Logger:      discardLogger{},
		// RSA-SHA256. SHA-1 is still what several service providers default
		// to and is not offered: a signature algorithm nobody should accept
		// is not one to make available in case somebody wants it.
		SignatureMethod: signatureMethodRSASHA256,
		// Portico's own POST-binding page, so that the inline script the
		// Content-Security-Policy names by hash is one this repository
		// controls rather than one a patch release could change.
		ResponseFormTemplate: postFormTemplate,
		ServiceProviderProvider: &providerLookup{
			tenantID:  tenant.ID,
			providers: p.providers,
		},
		SessionProvider: &interruptToSignIn{
			providers: p,
			tenant:    tenant,
			mount:     mount,
		},
	}, nil
}

// providerLookup answers the library's question "who is this request from"
// out of the tenant's own registrations.
type providerLookup struct {
	tenantID  string
	providers *service.SAMLServiceProviderService
}

func (l *providerLookup) GetServiceProvider(r *http.Request, entityID string) (*saml.EntityDescriptor, error) {
	descriptor, err := l.providers.Descriptor(r.Context(), l.tenantID, entityID)
	if err != nil {
		// The library treats os.ErrNotExist as "unknown service provider"
		// and anything else as a fault of ours. A disabled provider is the
		// former as far as the protocol is concerned: it must not receive
		// assertions, and saying more than that to an unauthenticated caller
		// would confirm which entity ids are registered here.
		return nil, os.ErrNotExist
	}
	return descriptor, nil
}

// interruptToSignIn is the SessionProvider, and it never returns a session.
//
// That is deliberate rather than incomplete. A SAML authentication request
// arrives as a plain browser navigation with no credential on it — Portico's
// own session lives in a token the single-page application holds, not in a
// cookie the protocol endpoint could read. So every request is parked and
// the browser sent to sign in, exactly as the OpenID Provider does; a person
// who is already signed in is returned immediately without seeing a form.
type interruptToSignIn struct {
	providers *Providers
	tenant    model.Tenant
	mount     string
}

func (s *interruptToSignIn) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	id, err := s.providers.park(r.Context(), s.tenant, s.mount, req)
	if err != nil {
		http.Error(w, "could not record the authentication request", http.StatusInternalServerError)
		return nil
	}
	http.Redirect(w, r, s.providers.LoginURL(s.tenant.Code, id), http.StatusFound)
	return nil
}

// park stores an authentication request while somebody signs in.
func (p *Providers) park(ctx context.Context, tenant model.Tenant, mount string, req *saml.IdpAuthnRequest) (string, error) {
	now := store.Now()
	id := newRequestID()

	err := p.store.ForTenant(tenant.ID).CreateSAMLAuthRequest(ctx, sqlcgen.CreateSAMLAuthRequestParams{
		ID:     id,
		Issuer: p.publicURL + mount,
		// The document as received, not a re-encoding of the parsed form.
		// It is re-validated on resume by the same library that validated it
		// here, and anything this package reconstructed would be a second
		// implementation of the parts it reconstructed.
		RequestXml: string(req.RequestBuffer),
		RelayState: req.RelayState,
		SpEntityID: req.Request.Issuer.Value,
		CreatedAt:  now,
		ExpiresAt:  now.Add(model.SAMLAuthRequestLifetime),
	})
	if err != nil {
		return "", fmt.Errorf("record authentication request: %w", err)
	}
	return id, nil
}

// Authentication is where a browser goes once a person has signed in.
type Authentication struct {
	// RedirectTo is Portico's own callback, which mints the assertion and
	// posts it onward to the service provider.
	RedirectTo string `json:"redirectTo"`
	// ServiceProviderName is shown by the sign-in screen, so a person knows
	// what they are being signed in to.
	ServiceProviderName string `json:"serviceProviderName"`
}

// Complete marks an authentication request as belonging to a signed-in
// person and returns where to send the browser.
func (p *Providers) Complete(ctx context.Context, actor auth.Principal, requestID, ip string) (Authentication, error) {
	q := p.store.ForTenant(actor.TenantID)

	row, err := q.GetSAMLAuthRequest(ctx, requestID, store.Now())
	if err != nil {
		if store.IsNoRows(err) {
			if p.existsElsewhere(ctx, requestID) {
				return Authentication{}, ErrWrongTenant
			}
			return Authentication{}, ErrAuthRequestNotFound
		}
		return Authentication{}, fmt.Errorf("get authentication request: %w", err)
	}

	// An authentication request is a one-shot object, and re-completing one
	// is how it stops being about the person it was completed for. The id
	// travels in a URL, so anybody who has seen that URL has it.
	if row.Done && (row.Subject == nil || *row.Subject != actor.UserID) {
		return Authentication{}, ErrAuthRequestTaken
	}

	// A service provider disabled between the request arriving and the
	// sign-in finishing must fail here, rather than at the callback where
	// the person is looking at a bare error page.
	provider, err := p.providers.Get(ctx, actor.TenantID, row.SpEntityID)
	if err != nil {
		return Authentication{}, ErrAuthRequestNotFound
	}
	if provider.Status != model.StatusActive {
		return Authentication{}, ErrProviderDisabled
	}

	if err := q.CompleteSAMLAuthRequest(ctx, row.ID, actor.UserID); err != nil {
		return Authentication{}, fmt.Errorf("complete authentication request: %w", err)
	}

	p.audit.Log(ctx, actor.TenantID, service.AuditEntry{
		Kind: model.LogLogin, Action: model.ActionSAMLAuthenticate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "SAML_SERVICE_PROVIDER", TargetID: provider.EntityID, TargetName: provider.Name,
		IP: ip,
	})

	return Authentication{
		// From the row rather than from the caller: the same tenant is
		// reachable at two mounts, and a service provider that fetched
		// metadata from one has the other's entity id on file as a
		// stranger's.
		RedirectTo:          row.Issuer + CallbackPath + "?id=" + url.QueryEscape(row.ID),
		ServiceProviderName: provider.Name,
	}, nil
}

func (p *Providers) existsElsewhere(ctx context.Context, requestID string) bool {
	tenants, err := p.tenants.List(ctx)
	if err != nil {
		return false
	}
	for _, tenant := range tenants {
		if _, err := p.store.ForTenant(tenant.ID).GetSAMLAuthRequest(ctx, requestID, store.Now()); err == nil {
			return true
		}
	}
	return false
}

// SweepExpired deletes authentication requests nobody completed.
func (p *Providers) SweepExpired(ctx context.Context) error {
	tenants, err := p.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	now := store.Now()
	for _, tenant := range tenants {
		if err := p.store.ForTenant(tenant.ID).DeleteExpiredSAMLAuthRequests(ctx, now); err != nil {
			return fmt.Errorf("sweep tenant %s: %w", tenant.Code, err)
		}
	}
	return nil
}

// discardLogger silences the library's own logging. What matters here is
// already recorded: rejected requests answer the caller, and completed ones
// write an audit entry.
type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}
func (discardLogger) Print(...any)          {}
func (discardLogger) Println(...any)        {}
func (discardLogger) Fatalf(string, ...any) {}
func (discardLogger) Fatal(...any)          {}
func (discardLogger) Fatalln(...any)        {}
func (discardLogger) Panicf(string, ...any) {}
func (discardLogger) Panic(...any)          {}
func (discardLogger) Panicln(...any)        {}
