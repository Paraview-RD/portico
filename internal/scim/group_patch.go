package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// PATCH /Groups/{id}.
//
// This is the operation group provisioning actually runs on, and the one
// with the most shapes in the wild. All of these mean "take this person out
// of this group":
//
//	{"op":"remove","path":"members","value":[{"value":"<id>"}]}
//	{"op":"remove","path":"members[value eq \"<id>\"]"}
//	{"op":"remove","path":"members[value eq \"<id>\"].value"}
//
// The second is what Okta sends, and it is why the path cannot simply be
// lowercased and looked up: the member id is inside the path, in a filter
// expression, and normalizePath would mangle the quoting. It is parsed
// explicitly here.
//
// And these mean "this is now the whole membership", which is how Entra
// reconciles:
//
//	{"op":"replace","path":"members","value":[…]}
//	{"op":"replace","value":{"members":[…]}}

func (h *Handler) patchGroup(w http.ResponseWriter, r *http.Request) {
	var body PatchRequest
	if err := decode(r, &body); err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidSyntax, err.Error())
		return
	}
	if len(body.Operations) == 0 {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidValue,
			"A PATCH must carry at least one operation.")
		return
	}

	tenantID := tenantOf(r)
	groupID := chi.URLParam(r, "id")

	current, err := h.groups.Get(r.Context(), tenantID, groupID)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}

	// Attribute changes are collected and written once; membership changes
	// are applied in order, because add-then-remove and remove-then-add are
	// different requests and a client may send both.
	desired := groupAttributes{
		displayName: current.DisplayName,
		externalID:  current.ExternalID,
	}

	for _, op := range body.Operations {
		change, patchErr := parseGroupOperation(op)
		if patchErr != nil {
			WriteError(w, r, http.StatusBadRequest, patchErr.scimType, patchErr.detail)
			return
		}

		if change.members != nil {
			err := h.applyGroupMembers(r.Context(), tenantID, groupID, change.members, change.op)
			if err != nil {
				h.failGroup(w, r, err)
				return
			}
			continue
		}
		change.apply(&desired)
	}

	if desired.changed(current) {
		if _, err := h.groups.Update(r.Context(), tenantID, groupID, desired.input(), scimActor); err != nil {
			h.failGroup(w, r, err)
			return
		}
	}

	updated, err := h.groups.Get(r.Context(), tenantID, groupID)
	if err != nil {
		h.failGroup(w, r, err)
		return
	}
	h.writeGroup(w, r, http.StatusOK, tenantID, updated)
}

// groupAttributes is the non-membership part of a group, as a patch builds
// it up.
type groupAttributes struct {
	displayName string
	externalID  string
}

func (a groupAttributes) input() service.GroupInput {
	return service.GroupInput{DisplayName: a.displayName, ExternalID: a.externalID}
}

func (a groupAttributes) changed(current model.Group) bool {
	return a.displayName != current.DisplayName || a.externalID != current.ExternalID
}

// groupChange is one parsed operation.
type groupChange struct {
	// members is non-nil for a membership operation, and op says which.
	members []string
	op      memberOp

	setDisplayName *string
	setExternalID  *string
}

func (c groupChange) apply(into *groupAttributes) {
	if c.setDisplayName != nil {
		into.displayName = *c.setDisplayName
	}
	if c.setExternalID != nil {
		into.externalID = *c.setExternalID
	}
}

// parseGroupOperation turns one SCIM operation into a change.
func parseGroupOperation(op PatchOperation) (groupChange, *patchError) {
	action := strings.ToLower(strings.TrimSpace(op.Op))
	switch action {
	case "add", "replace", "remove":
	default:
		return groupChange{}, &patchError{TypeInvalidSyntax,
			"Unknown patch operation " + op.Op + "; expected add, replace, or remove."}
	}

	path := strings.TrimSpace(op.Path)

	// The bracket form, where the member id is inside the path.
	if id, ok := memberIDFromPath(path); ok {
		if action != "remove" {
			// add and replace with a filtered path would mean "change this
			// member into something", which has no meaning here: a
			// membership is the pair, and there is nothing else to set.
			return groupChange{}, &patchError{TypeInvalidPath,
				"A filtered members path is only supported with remove."}
		}
		return groupChange{members: []string{id}, op: removeMembers}, nil
	}

	// A path-less operation carries an object of attributes.
	if path == "" {
		return parseGroupPathless(op, action)
	}

	switch normalizePath(path) {
	case "members":
		ids, ok := decodeMembers(op.Value)
		if !ok {
			// A remove with no value and no filter means "remove them all",
			// which RFC 7644 permits and a client occasionally sends before
			// re-adding.
			if action == "remove" {
				return groupChange{members: []string{}, op: replaceMembers}, nil
			}
			return groupChange{}, &patchError{TypeInvalidValue,
				"members must be an array of member objects."}
		}
		return groupChange{members: ids, op: memberOpFor(action)}, nil
	case "displayname":
		value := stringValue(op, action)
		return groupChange{setDisplayName: &value}, nil
	case "externalid":
		value := stringValue(op, action)
		return groupChange{setExternalID: &value}, nil
	default:
		return groupChange{}, &patchError{TypeInvalidPath,
			"This server does not support patching " + op.Path +
				" on a group. Supported paths: members, displayName, externalId."}
	}
}

func parseGroupPathless(op PatchOperation, action string) (groupChange, *patchError) {
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &attrs); err != nil {
		return groupChange{}, &patchError{TypeInvalidValue,
			"A patch operation with no path must carry an object of attributes."}
	}

	change := groupChange{}
	for name, raw := range attrs {
		switch strings.ToLower(name) {
		case "members":
			ids, ok := decodeMembers(raw)
			if !ok {
				return groupChange{}, &patchError{TypeInvalidValue,
					"members must be an array of member objects."}
			}
			change.members = ids
			change.op = memberOpFor(action)
		case "displayname":
			var value string
			if err := json.Unmarshal(raw, &value); err == nil {
				trimmed := strings.TrimSpace(value)
				change.setDisplayName = &trimmed
			}
		case "externalid":
			var value string
			if err := json.Unmarshal(raw, &value); err == nil {
				trimmed := strings.TrimSpace(value)
				change.setExternalID = &trimmed
			}
		default:
			return groupChange{}, &patchError{TypeInvalidPath,
				"This server does not support patching " + name +
					" on a group. Supported: members, displayName, externalId."}
		}
	}
	return change, nil
}

// memberIDFromPath reads the id out of `members[value eq "<id>"]`, with or
// without a trailing `.value`.
//
// Written as a small explicit parser rather than by extending the filter
// code: this is one shape, it appears in exactly one place, and a general
// filter parser applied to a path is how a path comes to mean something
// other than what it says.
func memberIDFromPath(path string) (string, bool) {
	open := strings.Index(path, "[")
	closing := strings.LastIndex(path, "]")
	if open < 0 || closing < open {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(path[:open]), "members") {
		return "", false
	}

	expression := strings.TrimSpace(path[open+1 : closing])
	fields := strings.Fields(expression)
	// value eq "<id>"
	if len(fields) < 3 || !strings.EqualFold(fields[0], "value") || !strings.EqualFold(fields[1], "eq") {
		return "", false
	}

	id := strings.Trim(strings.Join(fields[2:], " "), `"`)
	if id == "" {
		return "", false
	}
	return id, true
}

func memberOpFor(action string) memberOp {
	switch action {
	case "add":
		return addMembers
	case "remove":
		return removeMembers
	default:
		return replaceMembers
	}
}
