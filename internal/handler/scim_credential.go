package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
)

// The administrative side of SCIM: issuing and revoking the credentials a
// directory authenticates with. The SCIM API itself is in internal/scim and
// shares nothing with these endpoints except the service beneath them.

// ListSCIMCredentials returns the tenant's provisioning credentials.
func (h *Handler) ListSCIMCredentials(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	credentials, err := h.scimCredentials.List(r.Context(), actor.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, credentials)
}

type createSCIMCredentialRequest struct {
	Name string `json:"name"`
}

// CreateSCIMCredential issues one, returning the token exactly once.
func (h *Handler) CreateSCIMCredential(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req createSCIMCredentialRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The response carries the token, and it is the only time it exists
	// anywhere outside the client that will use it: what is stored is a
	// digest. An operator who loses it issues another rather than recovering
	// this one, which is the property that makes a database dump not a set
	// of working credentials.
	credential, err := h.scimCredentials.Create(r.Context(), actor, req.Name)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, credential)
}

// EnableSCIMCredential lets a paused integration resume.
func (h *Handler) EnableSCIMCredential(w http.ResponseWriter, r *http.Request) {
	h.setSCIMCredentialStatus(w, r, model.StatusActive)
}

// DisableSCIMCredential stops a directory syncing without discarding the
// record of it. The reversible half of revocation: an operator who suspects
// a sync is misbehaving can stop it and turn it back on, which deleting
// would not allow.
func (h *Handler) DisableSCIMCredential(w http.ResponseWriter, r *http.Request) {
	h.setSCIMCredentialStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setSCIMCredentialStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	actor := auth.MustPrincipal(r.Context())

	err := h.scimCredentials.SetStatus(r.Context(), actor, chi.URLParam(r, "id"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// DeleteSCIMCredential revokes one permanently.
func (h *Handler) DeleteSCIMCredential(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.scimCredentials.Delete(r.Context(), actor, chi.URLParam(r, "id")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}
