package handler

import (
	"errors"
	"net/http"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/oidcp"
	"github.com/Paraview-RD/portico/internal/samlp"
)

type authorizeRequest struct {
	// AuthRequestID is the value the sign-in screen was handed in its
	// `auth_request` query parameter.
	AuthRequestID string `json:"authRequestId"`
}

// Authorize completes an OAuth authorization request on behalf of the
// signed-in caller.
//
// This is the seam between Portico's own sign-in and the protocol: the
// OpenID Provider redirects a browser here to find out who is at the
// keyboard, and everything after this point is the protocol library's again.
//
// It is an authenticated endpoint and takes the person from the token, never
// from the request body. A subject supplied by the caller would be an
// endpoint for issuing tokens for other people.
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req authorizeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if req.AuthRequestID == "" {
		httpx.Fail(w, r, httpx.BadRequest("AUTH_REQUEST_REQUIRED",
			"No sign-in request was named."))
		return
	}

	authorization, err := h.oidc.Complete(r.Context(), principal, req.AuthRequestID, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, authorizeError(err))
		return
	}
	httpx.OK(w, authorization)
}

// authorizeError turns the ways completing can fail into answers somebody
// can act on. The tenant mismatch in particular has a remedy, and saying
// "unknown request" to someone who just signed in successfully would send
// them looking for a fault that is not there.
func authorizeError(err error) error {
	switch {
	case errors.Is(err, oidcp.ErrWrongTenant):
		return httpx.Forbidden("AUTH_REQUEST_WRONG_TENANT",
			"This sign-in request belongs to a different tenant. Sign out and sign in to the tenant the application asked for.")
	case errors.Is(err, oidcp.ErrAuthRequestTaken):
		return httpx.Conflict("AUTH_REQUEST_TAKEN",
			"This sign-in request has already been completed by another account. Start again from the application.")
	case errors.Is(err, oidcp.ErrAuthRequestNotFound):
		return httpx.NotFound("AUTH_REQUEST_NOT_FOUND",
			"This sign-in request has expired or was already used. Start again from the application.")
	case errors.Is(err, oidcp.ErrClientNotFound):
		return httpx.NotFound("OAUTH_CLIENT_NOT_FOUND",
			"The application this sign-in was for is no longer registered.")
	case errors.Is(err, oidcp.ErrClientDisabled):
		return httpx.Forbidden("OAUTH_CLIENT_DISABLED",
			"The application this sign-in was for has been disabled.")
	default:
		return err
	}
}

type samlAuthenticateRequest struct {
	// SAMLRequestID is the value the sign-in screen was handed in its
	// `saml_request` query parameter.
	SAMLRequestID string `json:"samlRequestId"`
}

// Authenticate completes a SAML authentication request on behalf of the
// signed-in caller.
//
// The SAML counterpart of Authorize, and the same seam: the Identity
// Provider parked the request and sent the browser here to find out who is
// at the keyboard.
func (h *Handler) Authenticate(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req samlAuthenticateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if req.SAMLRequestID == "" {
		httpx.Fail(w, r, httpx.BadRequest("AUTH_REQUEST_REQUIRED",
			"No sign-in request was named."))
		return
	}

	authentication, err := h.saml.Complete(r.Context(), principal, req.SAMLRequestID, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, authenticateError(err))
		return
	}
	httpx.OK(w, authentication)
}

// authenticateError mirrors authorizeError. The codes are shared with the
// OAuth path because the remedies are identical and the sign-in screen shows
// one message for each — a second set of codes meaning the same things would
// only be a second set of translations to keep in step.
func authenticateError(err error) error {
	switch {
	case errors.Is(err, samlp.ErrWrongTenant):
		return httpx.Forbidden("AUTH_REQUEST_WRONG_TENANT",
			"This sign-in request belongs to a different tenant. Sign out and sign in to the tenant the application asked for.")
	case errors.Is(err, samlp.ErrAuthRequestTaken):
		return httpx.Conflict("AUTH_REQUEST_TAKEN",
			"This sign-in request has already been completed by another account. Start again from the application.")
	case errors.Is(err, samlp.ErrAuthRequestNotFound):
		return httpx.NotFound("AUTH_REQUEST_NOT_FOUND",
			"This sign-in request has expired or was already used. Start again from the application.")
	case errors.Is(err, samlp.ErrProviderDisabled):
		return httpx.Forbidden("OAUTH_CLIENT_DISABLED",
			"The application this sign-in was for has been disabled.")
	default:
		return err
	}
}

type casAuthorizeRequest struct {
	// Service is the CAS `service` parameter the sign-in screen was handed.
	Service string `json:"service"`
}

// CASAuthorize issues a CAS service ticket for the signed-in caller.
//
// The third of these seams, and the simplest: CAS parks nothing, because a
// service URL is the whole request. It is checked against the tenant's
// registrations here rather than trusted, so the sign-in screen carrying it
// in a query parameter cannot turn into a ticket for somewhere else.
func (h *Handler) CASAuthorize(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req casAuthorizeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if req.Service == "" {
		httpx.Fail(w, r, httpx.BadRequest("CAS_SERVICE_REQUIRED",
			"No service was named."))
		return
	}

	authorization, err := h.cas.Complete(r.Context(), principal, req.Service, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, authorization)
}
