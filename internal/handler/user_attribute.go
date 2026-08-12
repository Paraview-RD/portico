package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// The attributes a tenant defines for itself, and the catalogue of everything
// that may be mapped.
//
// Two surfaces that look similar and are not. The catalogue is read-only and
// covers both the built-in vocabulary and the tenant's own, and it is what a
// mapping form draws its picker from. The definitions below are the tenant's
// half of that list, and are the only half anybody can edit.

// ListFields returns the field catalogue: everything that may be mapped, in
// either direction, built-in and tenant-defined together.
func (h *Handler) ListFields(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	fields, err := h.fields.Fields(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, fields)
}

type userAttributeRequest struct {
	// Key is read on creation and ignored on update. It is what a mapping
	// stores, so renaming it would silently stop a mapping that names it.
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	Kind          string   `json:"kind"`
	AllowedValues []string `json:"allowedValues"`
	Required      bool     `json:"required"`
	SortOrder     int      `json:"sortOrder"`
}

func (r userAttributeRequest) input() service.UserAttributeInput {
	return service.UserAttributeInput{
		Key: r.Key, Label: r.Label, Description: r.Description,
		Kind: r.Kind, AllowedValues: r.AllowedValues,
		Required: r.Required, SortOrder: r.SortOrder,
	}
}

// ListUserAttributes returns the tenant's own attribute definitions.
func (h *Handler) ListUserAttributes(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	definitions, err := h.attributes.Definitions(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, definitions)
}

// DefineUserAttribute adds one.
func (h *Handler) DefineUserAttribute(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req userAttributeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	definition, err := h.attributes.Define(r.Context(), principal, req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, definition)
}

// UpdateUserAttribute changes the editable parts of one.
func (h *Handler) UpdateUserAttribute(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req userAttributeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	definition, err := h.attributes.Update(r.Context(), principal, chi.URLParam(r, "id"), req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, definition)
}

// EnableUserAttribute brings a retired attribute back.
func (h *Handler) EnableUserAttribute(w http.ResponseWriter, r *http.Request) {
	h.setUserAttributeStatus(w, r, model.StatusActive)
}

// DisableUserAttribute retires one, keeping every value already recorded.
//
// This is the ordinary way to stop using an attribute. The values are often the
// answer to a question somebody asks later, which is why retiring and deleting
// are different actions rather than one with a flag.
func (h *Handler) DisableUserAttribute(w http.ResponseWriter, r *http.Request) {
	h.setUserAttributeStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setUserAttributeStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	definition, err := h.attributes.SetStatus(r.Context(), principal, chi.URLParam(r, "id"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, definition)
}

// DeleteUserAttribute removes an attribute and every value recorded against it.
func (h *Handler) DeleteUserAttribute(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	if err := h.attributes.Delete(r.Context(), principal, chi.URLParam(r, "id")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"status": "deleted"})
}

// GetUserAttributeValues returns one account's custom values, keyed by
// attribute key rather than by definition id: the key is what the rest of the
// system refers to, and an id would make every caller resolve it.
func (h *Handler) GetUserAttributeValues(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	values, err := h.attributes.Values(r.Context(), principal.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, values)
}

// SetUserAttributeValues records values for an account.
//
// A key absent from the body is left alone and an empty value clears it, which
// is the same contract the profile endpoint has: a form that shows three of an
// account's attributes must not blank the ones it did not show.
func (h *Handler) SetUserAttributeValues(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var values map[string]string
	if err := httpx.DecodeJSON(w, r, &values); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	userID := chi.URLParam(r, "id")
	if err := h.attributes.SetValues(r.Context(), principal, userID, values); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	stored, err := h.attributes.Values(r.Context(), principal.TenantID, userID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, stored)
}
