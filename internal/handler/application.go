package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/casp"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/oidcp"
	"github.com/paraview/portico/internal/samlp"
	"github.com/paraview/portico/internal/service"
)

// Application management: the console's half of registering the systems that
// sign in through Portico.
//
// Every endpoint here is administrator-only and tenant-scoped from the
// principal, so an administrator can only ever register an application in
// their own tenant. Registration used to be command-line-only; it is not any
// more, because a tenant administrator can already reset any password in
// their tenant and so already holds more power than a registration confers.
// The command-line equivalents remain, for a first deployment and for when
// the console cannot be reached.
//
// There is no delete anywhere in this file. Disabling stops an application
// working immediately and leaves the audit trail pointing at something that
// still exists; deleting would break the trail to save a row.

// --- OAuth 2.1 / OpenID Connect relying parties ----------------------------

// ListClients returns every relying party in the tenant.
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	clients, err := h.clients.List(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, clients)
}

// GetClient returns one relying party.
func (h *Handler) GetClient(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	client, err := h.clients.Get(r.Context(), principal.TenantID, chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, client)
}

type createClientRequest struct {
	ClientID               string   `json:"clientId"`
	Name                   string   `json:"name"`
	Public                 bool     `json:"public"`
	ApplicationType        string   `json:"applicationType"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	Scopes                 []string `json:"scopes"`
}

// registeredClientResponse carries the generated secret alongside the client.
//
// The secret is present on exactly two responses — registration and rotation
// — and is never readable afterwards, because only its hash is stored. The
// console says so at the point it shows the value; this is the reason it has
// to.
type registeredClientResponse struct {
	Client model.OAuthClient `json:"client"`
	Secret string            `json:"secret,omitempty"`
}

// CreateClient registers a relying party.
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req createClientRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	registered, err := h.clients.Register(r.Context(), principal, service.RegisterClientInput{
		ClientID:               req.ClientID,
		Name:                   req.Name,
		Public:                 req.Public,
		ApplicationType:        req.ApplicationType,
		RedirectURIs:           req.RedirectURIs,
		PostLogoutRedirectURIs: req.PostLogoutRedirectURIs,
		Scopes:                 req.Scopes,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, registeredClientResponse{
		Client: registered.Client,
		Secret: registered.Secret,
	})
}

type updateClientRequest struct {
	Name                   string   `json:"name"`
	ApplicationType        string   `json:"applicationType"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	Scopes                 []string `json:"scopes"`
}

// UpdateClient changes a relying party's settings.
func (h *Handler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateClientRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	client, err := h.clients.Update(r.Context(), principal,
		chi.URLParam(r, "clientID"), service.UpdateClientInput{
			Name:                   req.Name,
			ApplicationType:        req.ApplicationType,
			RedirectURIs:           req.RedirectURIs,
			PostLogoutRedirectURIs: req.PostLogoutRedirectURIs,
			Scopes:                 req.Scopes,
		})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, client)
}

// EnableClient puts a relying party back into service.
func (h *Handler) EnableClient(w http.ResponseWriter, r *http.Request) {
	h.setClientStatus(w, r, model.StatusActive)
}

// DisableClient stops a relying party without deleting it.
func (h *Handler) DisableClient(w http.ResponseWriter, r *http.Request) {
	h.setClientStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setClientStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	client, err := h.clients.SetStatus(r.Context(), principal, chi.URLParam(r, "clientID"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, client)
}

// RotateClientSecret issues a new secret and invalidates the old one.
func (h *Handler) RotateClientSecret(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	rotated, err := h.clients.RotateSecret(r.Context(), principal, chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, registeredClientResponse{
		Client: rotated.Client,
		Secret: rotated.Secret,
	})
}

// --- SAML 2.0 service providers --------------------------------------------

// ListServiceProviders returns every SAML service provider in the tenant.
func (h *Handler) ListServiceProviders(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	providers, err := h.serviceProviders.List(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, providers)
}

// GetServiceProvider returns one SAML service provider.
func (h *Handler) GetServiceProvider(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	entityID, err := pathEntityID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.serviceProviders.Get(r.Context(), principal.TenantID, entityID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

type serviceProviderRequest struct {
	// MetadataXML is the service provider's own metadata document, pasted or
	// uploaded. Portico never fetches it from a URL: doing so would make the
	// server issue a request to an address an administrator names, which is
	// a server-side request forgery against whatever else that server can
	// reach.
	MetadataXML string `json:"metadataXml"`
	Name        string `json:"name"`
}

// CreateServiceProvider registers a SAML service provider.
func (h *Handler) CreateServiceProvider(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req serviceProviderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.serviceProviders.Register(r.Context(), principal, service.RegisterSPInput{
		MetadataXML: req.MetadataXML,
		Name:        req.Name,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

// UpdateServiceProvider replaces a service provider's name and metadata,
// which is how its certificate gets rotated.
func (h *Handler) UpdateServiceProvider(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	entityID, err := pathEntityID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var req serviceProviderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.serviceProviders.Update(r.Context(), principal, entityID, service.UpdateSPInput{
		MetadataXML: req.MetadataXML,
		Name:        req.Name,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

// EnableServiceProvider puts a service provider back into service.
func (h *Handler) EnableServiceProvider(w http.ResponseWriter, r *http.Request) {
	h.setServiceProviderStatus(w, r, model.StatusActive)
}

// DisableServiceProvider stops a service provider without deleting it.
func (h *Handler) DisableServiceProvider(w http.ResponseWriter, r *http.Request) {
	h.setServiceProviderStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setServiceProviderStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	entityID, err := pathEntityID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.serviceProviders.SetStatus(r.Context(), principal, entityID, status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

// --- CAS services -----------------------------------------------------------

// ListCASServices returns every CAS service in the tenant.
func (h *Handler) ListCASServices(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	services, err := h.casServices.List(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, services)
}

type casServiceRequest struct {
	Name string `json:"name"`
	// URLPrefix is a prefix, not a pattern. There are no wildcards; see
	// service.MatchCASService for why the boundary matters.
	URLPrefix string `json:"urlPrefix"`
}

// CreateCASService registers a CAS service.
func (h *Handler) CreateCASService(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req casServiceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	svc, err := h.casServices.Register(r.Context(), principal, service.RegisterCASInput{
		Name:      req.Name,
		URLPrefix: req.URLPrefix,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, svc)
}

// UpdateCASService changes a CAS registration's name and URL prefix.
func (h *Handler) UpdateCASService(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	prefix, err := pathURLPrefix(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var req casServiceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	svc, err := h.casServices.Update(r.Context(), principal, prefix, service.UpdateCASInput{
		Name:      req.Name,
		URLPrefix: req.URLPrefix,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, svc)
}

// EnableCASService puts a CAS service back into service.
func (h *Handler) EnableCASService(w http.ResponseWriter, r *http.Request) {
	h.setCASServiceStatus(w, r, model.StatusActive)
}

// DisableCASService stops a CAS service without deleting it.
func (h *Handler) DisableCASService(w http.ResponseWriter, r *http.Request) {
	h.setCASServiceStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setCASServiceStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	prefix, err := pathURLPrefix(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	svc, err := h.casServices.SetStatus(r.Context(), principal, prefix, status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, svc)
}

// --- what to give the other side -------------------------------------------

// integrationEndpoints is everything an administrator has to paste into the
// application at the other end of an integration.
//
// It exists because a registration screen that only takes input is half a
// tool: having registered something, the next question is always "and what
// do I configure over there". Every value is derived from the running
// deployment's public URL and this tenant's code, so it cannot drift from
// what the server actually serves.
type integrationEndpoints struct {
	TenantCode string `json:"tenantCode"`
	// Issuer is the base every other address below hangs off, and is itself
	// the `iss` claim in tokens.
	Issuer string `json:"issuer"`

	OIDC struct {
		Discovery  string `json:"discovery"`
		Authorize  string `json:"authorize"`
		Token      string `json:"token"`
		UserInfo   string `json:"userinfo"`
		JWKS       string `json:"jwks"`
		EndSession string `json:"endSession"`
		// Introspect and Revoke are for a resource server rather than the
		// application being registered, but they are on the same issuer and
		// the person configuring one usually has to configure the other.
		Introspect string `json:"introspect"`
		Revoke     string `json:"revoke"`
	} `json:"oidc"`

	SAML struct {
		EntityID string `json:"entityId"`
		Metadata string `json:"metadata"`
		SSO      string `json:"sso"`
		// CertificatePEM is the current signing certificate, for a service
		// provider that wants the certificate rather than the whole
		// metadata document.
		CertificatePEM string `json:"certificatePem"`
	} `json:"saml"`

	CAS struct {
		// BaseURL is what a CAS client calls the "CAS server URL": the part
		// before /login.
		BaseURL         string `json:"baseUrl"`
		Login           string `json:"login"`
		Logout          string `json:"logout"`
		ServiceValidate string `json:"serviceValidate"`
	} `json:"cas"`
}

// IntegrationEndpoints reports the addresses to configure at the other end.
func (h *Handler) IntegrationEndpoints(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	tenant, err := h.tenants.Get(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// A tenant is always reachable at /t/<code>. The default tenant is also
	// reachable at the root, but the tenant-qualified form works for both
	// and is the one that keeps working if the deployment later stops being
	// single-tenant.
	mount := oidcp.TenantMount(tenant.Code)
	issuer := h.oidc.Issuer(mount)

	var out integrationEndpoints
	out.TenantCode = tenant.Code
	out.Issuer = issuer

	out.OIDC.Discovery = issuer + "/.well-known/openid-configuration"
	out.OIDC.Authorize = issuer + "/authorize"
	out.OIDC.Token = issuer + "/oauth/token"
	out.OIDC.UserInfo = issuer + "/userinfo"
	out.OIDC.JWKS = issuer + "/keys"
	out.OIDC.EndSession = issuer + "/end_session"
	out.OIDC.Introspect = issuer + "/oauth/introspect"
	out.OIDC.Revoke = issuer + "/revoke"

	out.SAML.EntityID = h.saml.Issuer(samlp.TenantMount(tenant.Code))
	out.SAML.Metadata = issuer + samlp.MetadataPath
	out.SAML.SSO = issuer + samlp.SSOPath

	// Best effort. A tenant that has never served SAML has no certificate
	// yet, and generating one as a side effect of opening a settings screen
	// would be a surprising place for a key to come into existence.
	if key, err := h.samlKeys.Active(r.Context(), principal.TenantID); err == nil {
		out.SAML.CertificatePEM = key.CertificatePEM
	}

	out.CAS.BaseURL = issuer + "/cas"
	out.CAS.Login = issuer + casp.LoginPath
	out.CAS.Logout = issuer + casp.LogoutPath
	out.CAS.ServiceValidate = issuer + casp.ValidatePath

	httpx.OK(w, out)
}

// --- path parameters --------------------------------------------------------

// pathEntityID reads a SAML entity id from the path.
//
// An entity id is a URI, so it arrives percent-encoded and has to be decoded
// before it can be matched against what was registered. chi hands over the
// raw segment; decoding it here rather than in each handler means a caller
// cannot reach the service layer with an id that is still escaped, which
// would simply report the registration as missing.
func pathEntityID(r *http.Request) (string, error) {
	return decodePathParam(r, "entityID", "ENTITY_ID_REQUIRED",
		"A service provider entity id is required.")
}

// pathURLPrefix reads a CAS service URL prefix from the path, on the same
// terms as pathEntityID.
func pathURLPrefix(r *http.Request) (string, error) {
	return decodePathParam(r, "prefix", "CAS_SERVICE_REQUIRED",
		"A service URL prefix is required.")
}

func decodePathParam(r *http.Request, name, missingCode, missingMessage string) (string, error) {
	raw := chi.URLParam(r, name)
	if strings.TrimSpace(raw) == "" {
		return "", httpx.BadRequest(missingCode, missingMessage)
	}

	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", httpx.BadRequest("INVALID_PATH_PARAMETER",
			"That path parameter is not valid percent-encoding.")
	}
	return decoded, nil
}
