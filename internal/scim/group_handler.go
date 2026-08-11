package scim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// GroupProvisioner is the slice of the group service SCIM needs.
type GroupProvisioner interface {
	Create(ctx context.Context, tenantID string, in service.GroupInput, source model.GroupSource, actor auth.Principal) (model.Group, error)
	Get(ctx context.Context, tenantID, id string) (model.Group, error)
	List(ctx context.Context, tenantID string) ([]model.Group, error)
	Update(ctx context.Context, tenantID, id string, in service.GroupInput, actor auth.Principal) (model.Group, error)
	Delete(ctx context.Context, tenantID, id string, actor auth.Principal) error
	Members(ctx context.Context, tenantID, groupID string) ([]model.GroupMember, error)
	// GroupsForUser is here rather than on Provisioner because it is the
	// group side of the relationship, and putting it there would make the
	// user service responsible for something it does not own.
	GroupsForUser(ctx context.Context, tenantID, userID string) ([]model.GroupRef, error)
	FindByExternalID(ctx context.Context, tenantID, externalID string) (model.Group, error)
	FindByDisplayName(ctx context.Context, tenantID, name string) (model.Group, error)
	AddMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error
	RemoveMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error
	ReplaceMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error
}

// scimActor is the principal group operations are attributed to.
//
// Empty rather than a synthetic account: there is no person here, and the
// service names the provisioning marker in the audit trail when the group's
// source says a directory owns it.
var scimActor = auth.Principal{}

func (h *Handler) getGroup(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantOf(r)
	group, err := h.groups.Get(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	h.writeGroup(w, r, http.StatusOK, tenantID, group)
}

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantOf(r)

	if filter := strings.TrimSpace(r.URL.Query().Get("filter")); filter != "" {
		h.listGroupsFiltered(w, r, tenantID, filter)
		return
	}

	groups, err := h.groups.List(r.Context(), tenantID)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}

	resources, err := h.renderGroups(r.Context(), tenantID, groups)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	WriteResource(w, http.StatusOK,
		NewGroupListResponse(resources, len(resources), 1))
}

func (h *Handler) listGroupsFiltered(w http.ResponseWriter, r *http.Request, tenantID, filter string) {
	attribute, value, err := parseEqualityFilter(filter)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidValue, err.Error())
		return
	}

	var found model.Group
	switch attribute {
	case "displayname":
		found, err = h.groups.FindByDisplayName(r.Context(), tenantID, value)
	case "externalid":
		found, err = h.groups.FindByExternalID(r.Context(), tenantID, value)
	default:
		WriteError(w, r, http.StatusBadRequest, TypeInvalidPath,
			"Groups are filterable on displayName and externalId only; "+
				attribute+" is not.")
		return
	}

	switch {
	case err == nil:
	case errors.Is(err, service.ErrGroupNotFound):
		// An empty result rather than a 404: "no such group" is the answer a
		// reconciling client is asking for, and the one that tells it to
		// create the group.
		WriteResource(w, http.StatusOK, NewGroupListResponse(nil, 0, 1))
		return
	default:
		h.failGroup(w, r, err)
		return
	}

	resources, err := h.renderGroups(r.Context(), tenantID, []model.Group{found})
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	WriteResource(w, http.StatusOK, NewGroupListResponse(resources, 1, 1))
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var body Group
	if err := decode(r, &body); err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidSyntax, err.Error())
		return
	}

	tenantID := tenantOf(r)
	in := service.GroupInput{
		DisplayName: strings.TrimSpace(body.DisplayName),
		ExternalID:  strings.TrimSpace(body.ExternalID),
	}

	// Reconciliation, on the same terms as users: a client that lost track
	// and re-creates a group must not end up with two. externalId first
	// because it survives a rename, then the name.
	if existing, ok := h.existingGroup(r.Context(), tenantID, in); ok {
		if err := h.applyGroupMembers(r.Context(), tenantID, existing.ID,
			MemberIDs(body.Members), replaceMembers); err != nil {
			h.failGroup(w, r, err)
			return
		}
		// The description comes from the group as it stands. SCIM has no
		// attribute for it, so a directory cannot have meant to clear it,
		// and an update built from the request alone would.
		in.Description = existing.Description

		updated, err := h.groups.Update(r.Context(), tenantID, existing.ID, in, scimActor)
		if err != nil {
			h.failGroup(w, r, err)
			return
		}
		h.writeGroup(w, r, http.StatusOK, tenantID, updated)
		return
	}

	group, err := h.groups.Create(r.Context(), tenantID, in, model.GroupSourceSCIM, scimActor)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}

	if err := h.applyGroupMembers(r.Context(), tenantID, group.ID,
		MemberIDs(body.Members), replaceMembers); err != nil {
		h.failGroup(w, r, err)
		return
	}

	w.Header().Set("Location", h.baseURL()+"/Groups/"+group.ID)
	h.writeGroup(w, r, http.StatusCreated, tenantID, group)
}

// existingGroup finds the group a create request is really an update to.
func (h *Handler) existingGroup(ctx context.Context, tenantID string, in service.GroupInput) (model.Group, bool) {
	if in.ExternalID != "" {
		if found, err := h.groups.FindByExternalID(ctx, tenantID, in.ExternalID); err == nil {
			return found, true
		}
	}
	if found, err := h.groups.FindByDisplayName(ctx, tenantID, in.DisplayName); err == nil {
		return found, true
	}
	return model.Group{}, false
}

// replaceGroup is PUT: the body is the whole resource, including membership.
func (h *Handler) replaceGroup(w http.ResponseWriter, r *http.Request) {
	var body Group
	if err := decode(r, &body); err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidSyntax, err.Error())
		return
	}

	tenantID := tenantOf(r)
	groupID := chi.URLParam(r, "id")

	// Read first, for the description alone. PUT replaces the resource, and
	// the resource is what SCIM defines — a field the schema has no way to
	// carry is not part of what was replaced, so it is read across rather
	// than written over.
	current, err := h.groups.Get(r.Context(), tenantID, groupID)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}

	updated, err := h.groups.Update(r.Context(), tenantID, groupID, service.GroupInput{
		DisplayName: strings.TrimSpace(body.DisplayName),
		ExternalID:  strings.TrimSpace(body.ExternalID),
		Description: current.Description,
	}, scimActor)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}

	// PUT replaces the resource, so it replaces the membership too. A PUT
	// that left members alone would make the two verbs disagree about what
	// "the whole resource" means, and Entra reconciles with exactly this.
	if err := h.applyGroupMembers(r.Context(), tenantID, groupID,
		MemberIDs(body.Members), replaceMembers); err != nil {
		h.failGroup(w, r, err)
		return
	}

	h.writeGroup(w, r, http.StatusOK, tenantID, updated)
}

func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	// Deleted, not deactivated — unlike an account. A group is a set rather
	// than a party the audit trail refers to, and a directory that deletes
	// and recreates one, which they do, would otherwise accumulate them.
	err := h.groups.Delete(r.Context(), tenantOf(r), chi.URLParam(r, "id"), scimActor)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderGroups(ctx context.Context, tenantID string, groups []model.Group) ([]Group, error) {
	out := make([]Group, 0, len(groups))
	for _, group := range groups {
		members, err := h.groups.Members(ctx, tenantID, group.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupFromModel(group, members, h.baseURL()))
	}
	return out, nil
}

func (h *Handler) writeGroup(w http.ResponseWriter, r *http.Request, status int, tenantID string, group model.Group) {
	members, err := h.groups.Members(r.Context(), tenantID, group.ID)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	WriteResource(w, status, GroupFromModel(group, members, h.baseURL()))
}

// failGroup maps a service error onto SCIM's error shape.
func (h *Handler) failGroup(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrGroupNotFound):
		WriteError(w, r, http.StatusNotFound, "", "No such group.")
	case errors.Is(err, service.ErrGroupNameTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"A group with that displayName already exists.")
	case errors.Is(err, service.ErrGroupExternalIDTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"That externalId is already bound to another group.")
	case errors.Is(err, service.ErrMemberNotFound):
		// Reported rather than skipped: a group that looks synchronized and
		// quietly lost a member is the failure a directory cannot see.
		WriteError(w, r, http.StatusBadRequest, TypeInvalidValue,
			"One of the members does not exist in this tenant.")
	default:
		h.fail(w, r, err)
	}
}

// memberOp is how a membership change should be applied.
type memberOp int

const (
	addMembers memberOp = iota
	removeMembers
	replaceMembers
)

func (h *Handler) applyGroupMembers(ctx context.Context, tenantID, groupID string, userIDs []string, op memberOp) error {
	switch op {
	case addMembers:
		return h.groups.AddMembers(ctx, tenantID, groupID, userIDs, scimActor)
	case removeMembers:
		return h.groups.RemoveMembers(ctx, tenantID, groupID, userIDs, scimActor)
	default:
		return h.groups.ReplaceMembers(ctx, tenantID, groupID, userIDs, scimActor)
	}
}

// decodeMembers reads the value of a members operation.
//
// Both shapes: an array of member objects, which is what a client sends for
// add and replace, and a bare string, which some send for a single removal.
func decodeMembers(raw json.RawMessage) ([]string, bool) {
	var members []Member
	if err := json.Unmarshal(raw, &members); err == nil {
		return MemberIDs(members), true
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && strings.TrimSpace(single) != "" {
		return []string{strings.TrimSpace(single)}, true
	}
	return nil, false
}
