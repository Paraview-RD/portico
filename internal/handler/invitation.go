package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/service"
)

// Invitation codes: administrator-issued, quota-limited credentials that let
// self-registration stay closed to the public while still admitting
// specific people without an administrator creating each account by hand.
//
// There is no enable/re-enable endpoint here, deliberately — see
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md.
// Disabling is terminal; an administrator who wants the same access
// available again issues a new code.

// ListInvitations returns every invitation code issued in this tenant.
func (h *Handler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	invitations, err := h.invitations.List(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, invitations)
}

type createInvitationRequest struct {
	Code           string     `json:"code"`
	OrganizationID string     `json:"organizationId"`
	GroupIDs       []string   `json:"groupIds"`
	Quota          int        `json:"quota"`
	ExpiresAt      *time.Time `json:"expiresAt"`
}

// CreateInvitation issues a new invitation code.
func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req createInvitationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	invitation, err := h.invitations.Create(r.Context(), principal, service.CreateInvitationInput{
		Code:           req.Code,
		OrganizationID: req.OrganizationID,
		GroupIDs:       req.GroupIDs,
		Quota:          req.Quota,
		ExpiresAt:      req.ExpiresAt,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, invitation)
}

// DisableInvitation shuts off an invitation code. There is no path back to
// ACTIVE — see the package comment above.
func (h *Handler) DisableInvitation(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	invitation, err := h.invitations.Disable(r.Context(), principal, chi.URLParam(r, "invitationID"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, invitation)
}
