package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Groups: sets of people, as distinct from the organization chart.
//
// Both exist because they answer different questions. An organization is
// where somebody sits — one of them, in a tree, with a code downstream
// systems store. A group is a set they belong to — any number of them, flat,
// usually maintained by a directory. Membership grants nothing.

// ListGroups returns the tenant's groups with member counts.
func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	groups, err := h.groups.List(r.Context(), actor.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, groups)
}

// ListOwnGroups returns the groups the caller belongs to.
//
// Separate from ListUserGroups, which is administrator-only because asking
// about somebody else is an administrative act. Asking about yourself is not,
// and the home screen — the one screen an ordinary user has — needs the
// answer. Reading it through the administrative route meant every
// non-administrator's portal made a request that answered 403, caught it, and
// showed an empty list that looked like membership of nothing.
//
// The id is taken from the token and never from the path, so there is no
// version of this that can be asked about another account.
func (h *Handler) ListOwnGroups(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	groups, err := h.groups.GroupsForUser(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, groups)
}

// GetGroup returns one group.
func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	group, err := h.groups.Get(r.Context(), actor.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, group)
}

type groupRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// CreateGroup adds one.
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req groupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Source ADMIN: created here, by a person. A directory's groups are
	// marked SCIM so an administrator can see that the next sync may
	// overwrite what they are about to edit.
	group, err := h.groups.Create(r.Context(), actor.TenantID, service.GroupInput{
		DisplayName: req.DisplayName, Description: req.Description,
	}, model.GroupSourceAdmin, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, group)
}

// UpdateGroup renames one or changes its description.
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req groupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	group, err := h.groups.Update(r.Context(), actor.TenantID, chi.URLParam(r, "id"),
		service.GroupInput{DisplayName: req.DisplayName, Description: req.Description}, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, group)
}

// DeleteGroup removes a group and its memberships.
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.groups.Delete(r.Context(), actor.TenantID, chi.URLParam(r, "id"), actor); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// ListGroupMembers returns who is in a group.
func (h *Handler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	members, err := h.groups.Members(r.Context(), actor.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, members)
}

type membersRequest struct {
	UserIDs []string `json:"userIds"`
}

// AddGroupMembers puts accounts into a group.
func (h *Handler) AddGroupMembers(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req membersRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err := h.groups.AddMembers(r.Context(), actor.TenantID, chi.URLParam(r, "id"), req.UserIDs, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// RemoveGroupMember takes one account out.
func (h *Handler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	err := h.groups.RemoveMembers(r.Context(), actor.TenantID,
		chi.URLParam(r, "id"), []string{chi.URLParam(r, "userID")}, actor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// ListUserGroups returns the groups an account belongs to, for the user
// detail screen — otherwise a group is data nobody in the console can see.
func (h *Handler) ListUserGroups(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	groups, err := h.groups.GroupsForUser(r.Context(), actor.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, groups)
}
