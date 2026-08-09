package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// ListOrganizations returns every organization.
//
// Any signed-in user may read this: the picker on their own profile needs
// it, and an organization name is not sensitive.
func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())
	activeOnly := r.URL.Query().Get("activeOnly") == "true"

	orgs, err := h.orgs.List(r.Context(), principal.TenantID, activeOnly)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, orgs)
}

// GetOrganization returns one organization.
func (h *Handler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	org, err := h.orgs.Get(r.Context(), principal.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

type createOrganizationRequest struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Remark string `json:"remark"`
	// Empty for a root.
	ParentID  string `json:"parentId"`
	SortOrder int    `json:"sortOrder"`
}

// CreateOrganization adds an organization.
func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req createOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	org, err := h.orgs.Create(r.Context(), principal, service.OrganizationInput{
		Name:      req.Name,
		Code:      req.Code,
		Remark:    req.Remark,
		ParentID:  req.ParentID,
		SortOrder: req.SortOrder,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

type updateOrganizationRequest struct {
	Name   string `json:"name"`
	Remark string `json:"remark"`
	// The move. Empty promotes to a root.
	ParentID  string `json:"parentId"`
	SortOrder int    `json:"sortOrder"`
}

// UpdateOrganization changes an organization's name, remark, and order. The
// code is immutable once created.
func (h *Handler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	org, err := h.orgs.Update(r.Context(), principal, chi.URLParam(r, "id"), service.OrganizationInput{
		Name:      req.Name,
		Remark:    req.Remark,
		ParentID:  req.ParentID,
		SortOrder: req.SortOrder,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

// EnableOrganization reactivates an organization.
func (h *Handler) EnableOrganization(w http.ResponseWriter, r *http.Request) {
	h.setOrganizationStatus(w, r, model.StatusActive)
}

// DisableOrganization stops new members from being assigned to an
// organization. Existing members are left in place.
func (h *Handler) DisableOrganization(w http.ResponseWriter, r *http.Request) {
	h.setOrganizationStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setOrganizationStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	org, err := h.orgs.SetStatus(r.Context(), principal, chi.URLParam(r, "id"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

type organizationManagerRequest struct {
	// Empty clears the nomination. A pointer would distinguish "not sent"
	// from "sent as empty", and here they should mean the same thing: this
	// endpoint exists to set exactly one field, so an empty body asking for
	// nobody is not ambiguous.
	ManagerID string `json:"managerId"`
}

// SetOrganizationManager nominates whoever is responsible for one.
//
// It grants nothing, which is worth knowing before reaching for it: this
// version has two fixed roles, and being named here confers none of them.
// The field answers "who runs this department" for a person reading the
// chart and for downstream systems that ask.
func (h *Handler) SetOrganizationManager(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req organizationManagerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	org, err := h.orgs.SetManager(r.Context(), principal, chi.URLParam(r, "id"), req.ManagerID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

type organizationAttachmentRequest struct {
	UserID string `json:"userId"`
}

// AttachUserToOrganization records that somebody is involved with an
// organization they do not primarily belong to.
//
// Their primary membership does not move, and this grants nothing — the same
// as group membership, and for the same reason: there is no permission model
// here to attach it to.
func (h *Handler) AttachUserToOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req organizationAttachmentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.orgs.AttachUser(r.Context(), principal, chi.URLParam(r, "id"), req.UserID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// DetachUserFromOrganization removes an attachment.
func (h *Handler) DetachUserFromOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	err := h.orgs.DetachUser(r.Context(), principal,
		chi.URLParam(r, "id"), chi.URLParam(r, "userID"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}
