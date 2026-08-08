package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// PATCH, RFC 7644 §3.5.2.
//
// This is the operation both Okta and Entra lean on hardest, and the one
// most often implemented as "return 501 and hope". It is not: a 501 tells an
// administrator the server is broken, while the 400 with scimType invalidPath
// that the RFC asks for tells them which attribute nobody can set, in the
// sync log, where they are already looking.
//
// The implemented subset is the deprovisioning path — `active`, plus the
// attributes a directory routinely corrects — and the boundary is stated in
// the error rather than left to be discovered.

// PatchRequest is SCIM's patch body.
type PatchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []PatchOperation `json:"Operations"`
}

// PatchOperation is one change.
type PatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

func (h *Handler) patchUser(w http.ResponseWriter, r *http.Request) {
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
	userID := chi.URLParam(r, "id")

	current, err := h.users.Get(r.Context(), tenantID, userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Applied to a copy first, and written once at the end. A PATCH is a
	// single operation as far as the client is concerned: applying operations
	// one at a time would leave a request that fails on its third operation
	// having already committed the first two, and the client — which will
	// retry the whole body — cannot know that.
	desired := service.ProvisionUserInput{
		Username:    current.Username,
		DisplayName: current.DisplayName,
		Email:       current.Email,
		Phone:       current.Phone,
		ExternalID:  current.ExternalID,
		Active:      current.Status == model.StatusActive,
	}

	for _, op := range body.Operations {
		if err := applyOperation(&desired, op); err != nil {
			WriteError(w, r, http.StatusBadRequest, err.scimType, err.detail)
			return
		}
	}

	updated, err := h.users.UpdateProvisionedUser(r.Context(), tenantID, userID, desired)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	WriteResource(w, http.StatusOK, FromModel(updated, h.baseURL()))
}

// patchError carries what the client needs to act.
type patchError struct {
	scimType string
	detail   string
}

func (e *patchError) Error() string { return e.detail }

// applyOperation folds one operation into the desired state.
//
// "remove" is accepted only for the attributes that can be absent. Removing
// userName is refused rather than ignored: an account with no username
// cannot sign in and cannot be found, and a silently ignored removal would
// leave the directory and this server disagreeing about what happened.
func applyOperation(desired *service.ProvisionUserInput, op PatchOperation) *patchError {
	action := strings.ToLower(strings.TrimSpace(op.Op))
	switch action {
	case "add", "replace", "remove":
	default:
		return &patchError{TypeInvalidSyntax,
			"Unknown patch operation " + op.Op + "; expected add, replace, or remove."}
	}

	// A path-less operation carries an object of attributes, which is how
	// Entra sends most changes.
	if strings.TrimSpace(op.Path) == "" {
		return applyPathless(desired, op)
	}

	path := normalizePath(op.Path)
	switch path {
	case "active":
		if action == "remove" {
			return &patchError{TypeInvalidValue,
				"active cannot be removed; set it to false to deprovision."}
		}
		var active bool
		if err := json.Unmarshal(op.Value, &active); err != nil {
			return &patchError{TypeInvalidValue, "active must be true or false."}
		}
		desired.Active = active
	case "displayname":
		desired.DisplayName = stringValue(op, action)
	case "username":
		if action == "remove" {
			return &patchError{TypeInvalidValue, "userName cannot be removed."}
		}
		desired.Username = stringValue(op, action)
	case "externalid":
		desired.ExternalID = stringValue(op, action)
	case "emails", "emails[type eq \"work\"].value", "emails.value":
		desired.Email = multiValue(op, action)
	case "phonenumbers", "phonenumbers[type eq \"work\"].value", "phonenumbers.value":
		desired.Phone = multiValue(op, action)
	case "password":
		// Refused rather than accepted-and-dropped. ServiceProviderConfig
		// says changePassword is not supported; this is the same answer at
		// the point a client tries anyway.
		return &patchError{TypeInvalidPath,
			"This server does not accept passwords over SCIM; " +
				"see changePassword in /ServiceProviderConfig."}
	default:
		return &patchError{TypeInvalidPath,
			"This server does not support patching " + op.Path +
				". Supported paths: active, userName, displayName, externalId, " +
				"emails, phoneNumbers."}
	}
	return nil
}

// applyPathless handles the {"op":"replace","value":{...}} form.
func applyPathless(desired *service.ProvisionUserInput, op PatchOperation) *patchError {
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &attrs); err != nil {
		return &patchError{TypeInvalidValue,
			"A patch operation with no path must carry an object of attributes."}
	}

	for name, raw := range attrs {
		// Reuses the same switch, so a path-less change and a pathed one can
		// never diverge on which attributes are supported — the divergence
		// being how "active works from Okta but not from Entra" happens.
		if err := applyOperation(desired, PatchOperation{
			Op: op.Op, Path: name, Value: raw,
		}); err != nil {
			return err
		}
	}
	return nil
}

// normalizePath lowercases and strips the schema URN prefix some clients
// send, so `urn:...:User:active` and `active` are one path.
func normalizePath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	if idx := strings.LastIndex(path, ":"); idx >= 0 && strings.HasPrefix(path, "urn:") {
		path = path[idx+1:]
	}
	return path
}

// stringValue reads a string attribute, treating remove as clearing it.
func stringValue(op PatchOperation, action string) string {
	if action == "remove" {
		return ""
	}
	var s string
	if err := json.Unmarshal(op.Value, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// multiValue reads either a bare string or SCIM's multi-valued form, because
// clients send both for the same attribute.
func multiValue(op PatchOperation, action string) string {
	if action == "remove" {
		return ""
	}
	var single string
	if err := json.Unmarshal(op.Value, &single); err == nil {
		return strings.TrimSpace(single)
	}
	var many []Multi
	if err := json.Unmarshal(op.Value, &many); err == nil {
		return PrimaryValue(many)
	}
	return ""
}
