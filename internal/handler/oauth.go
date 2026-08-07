package handler

import (
	"errors"
	"net/http"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/oidcp"
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
