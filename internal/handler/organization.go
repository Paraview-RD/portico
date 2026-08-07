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
	Name      string `json:"name"`
	Code      string `json:"code"`
	Remark    string `json:"remark"`
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
		SortOrder: req.SortOrder,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, org)
}

type updateOrganizationRequest struct {
	Name      string `json:"name"`
	Remark    string `json:"remark"`
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
