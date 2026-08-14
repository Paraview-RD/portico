package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Signing in through somebody else's provider, and administering which ones.
//
// The two halves have different callers and different rules. Configuration
// is an administrator's, inside a tenant they already hold a session for.
// The sign-in half is public by necessity — its caller is somebody who
// cannot sign in yet, which is the whole point of it.

type externalSignInRequest struct {
	Tenant string `json:"tenant"`
	// Provider is the id from /auth/external/providers.
	Provider string `json:"provider"`
}

// StartExternalSignIn hands back the address to send a browser to.
//
// It returns the URL rather than redirecting. The caller is the sign-in
// screen, which is JavaScript holding a fetch response: a 302 here would be
// followed by the browser inside that fetch and the page would never move.
func (h *Handler) StartExternalSignIn(w http.ResponseWriter, r *http.Request) {
	var req externalSignInRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	url, err := h.externalIDP.StartExternalSignIn(r.Context(), tenant, req.Provider, "")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"authorizationUrl": url})
}

// ExternalSignInOptions lists the buttons a tenant's sign-in screen offers.
//
// Public, and it says only what a button says. The issuer, the client id and
// whether an address is trusted are configuration, and this answers before
// anybody has proved anything.
func (h *Handler) ExternalSignInOptions(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolvePublicTenant(r, r.URL.Query().Get("tenant"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	options, err := h.externalIDP.SignInOptions(r.Context(), tenant.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, options)
}

// CompleteExternalSignIn spends what a browser came back holding.
//
// The provider does not redirect here. It redirects to the console, which
// reads the `state` and `code` out of the address it landed on and calls
// this — so the caller is a fetch, and answering JSON is right. What it is
// spending, though, came from somewhere else entirely, and the caller holds
// nothing this server issued except that state. Everything that judges it
// was decided before the browser left; see service.CompleteExternalSignIn.
//
// Public for the same reason the sign-in endpoint is: whoever is calling
// cannot sign in yet, which is what they are here to fix.
func (h *Handler) CompleteExternalSignIn(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolvePublicTenant(r, "")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	query := r.URL.Query()
	// A provider reporting a failure sends `error` instead of `code`. Saying
	// so beats "the answer could not be accepted", which is what the
	// exchange would have said about an absent code.
	if reason := query.Get("error"); reason != "" {
		httpx.Fail(w, r, httpx.Unauthorized("EXTERNAL_PROVIDER_REFUSED",
			"The identity provider refused the sign-in: "+reason))
		return
	}

	outcome, err := h.externalIDP.CompleteExternalSignIn(r.Context(), tenant,
		query.Get("state"), query.Get("code"), httpx.ClientIP(r), userAgent(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if outcome.Session != nil {
		httpx.OK(w, outcome.Session)
		return
	}
	httpx.OK(w, outcome.Bound)
}

// StartExternalBinding begins linking an identity to the caller's own
// account.
//
// Authenticated, and the account is taken from the session rather than from
// the request. A caller-supplied account id here would be the whole
// vulnerability this journey is arranged to avoid.
func (h *Handler) StartExternalBinding(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	tenant, err := h.tenants.Get(r.Context(), actor.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	url, err := h.externalIDP.StartExternalSignIn(r.Context(), tenant,
		chi.URLParam(r, "id"), actor.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"authorizationUrl": url})
}

// ListMyExternalIdentities returns what the caller has linked.
func (h *Handler) ListMyExternalIdentities(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	identities, err := h.externalIDP.IdentitiesFor(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, identities)
}

// UnlinkMyExternalIdentity removes one of the caller's own.
func (h *Handler) UnlinkMyExternalIdentity(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.externalIDP.Unbind(r.Context(), actor.TenantID, actor.UserID,
		chi.URLParam(r, "id")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// --- Administration ------------------------------------------------------

type externalIDPRequest struct {
	Name        string `json:"name"`
	ButtonLabel string `json:"buttonLabel"`
	// Kind is OIDC when absent, so a caller written before WeChat and
	// DingTalk existed still means what it meant.
	Kind     string `json:"kind"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// ClientSecret blank on an edit keeps the stored one. The console never
	// receives it, so a blank field is what an administrator always sees.
	ClientSecret       string `json:"clientSecret"`
	Scopes             string `json:"scopes"`
	TrustVerifiedEmail bool   `json:"trustVerifiedEmail"`
}

func (req externalIDPRequest) input() service.ExternalIDPInput {
	return service.ExternalIDPInput{
		Name: req.Name, ButtonLabel: req.ButtonLabel,
		Kind: req.Kind, Issuer: req.Issuer,
		ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		Scopes: req.Scopes, TrustVerifiedEmail: req.TrustVerifiedEmail,
	}
}

// tenantCodeOf is what the redirect URI is built from.
//
// Looked up rather than carried on the principal: the code is a tenant's
// property and the token holds an id, and a redirect URI is the one value
// here that has to match another system's registration character for
// character.
func (h *Handler) tenantCodeOf(r *http.Request, actor auth.Principal) (string, error) {
	tenant, err := h.tenants.Get(r.Context(), actor.TenantID)
	if err != nil {
		return "", err
	}
	return tenant.Code, nil
}

// ListExternalIDPs returns a tenant's configured providers.
func (h *Handler) ListExternalIDPs(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	code, err := h.tenantCodeOf(r, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	providers, err := h.externalIDP.List(r.Context(), actor.TenantID, code)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, providers)
}

// CreateExternalIDP registers one.
func (h *Handler) CreateExternalIDP(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req externalIDPRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	code, err := h.tenantCodeOf(r, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.externalIDP.Create(r.Context(), actor, code, req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

// UpdateExternalIDP edits one.
func (h *Handler) UpdateExternalIDP(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req externalIDPRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	code, err := h.tenantCodeOf(r, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	provider, err := h.externalIDP.Update(r.Context(), actor, code,
		chi.URLParam(r, "id"), req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, provider)
}

// EnableExternalIDP puts the button back on the sign-in screen.
func (h *Handler) EnableExternalIDP(w http.ResponseWriter, r *http.Request) {
	h.setExternalIDPStatus(w, r, model.StatusActive)
}

// DisableExternalIDP takes it off, leaving every binding in place.
func (h *Handler) DisableExternalIDP(w http.ResponseWriter, r *http.Request) {
	h.setExternalIDPStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setExternalIDPStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.externalIDP.SetStatus(r.Context(), actor, chi.URLParam(r, "id"), status); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// DeleteExternalIDP removes a provider and every binding that named it.
func (h *Handler) DeleteExternalIDP(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.externalIDP.Delete(r.Context(), actor, chi.URLParam(r, "id")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}
