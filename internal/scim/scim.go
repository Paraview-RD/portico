// Package scim implements SCIM 2.0 provisioning (RFC 7643, RFC 7644) for
// user accounts.
//
// It sits beside the API rather than inside it, like oidcp and samlp: SCIM
// has its own media type, its own error format, its own idea of what a
// resource looks like, and none of it should leak into Portico's own
// endpoints. What it shares is the service layer, so a directory sync and an
// administrator changing the same account go through the same rules.
//
// # Scope
//
// Users only. Groups are not implemented, and that is a decision rather than
// an omission: an account in Portico belongs to at most one organization,
// while a SCIM group membership is many-to-many and `PATCH /Groups` with
// `add members` is the operation both Okta and Entra lean on hardest.
// Mapping it onto a single-valued field would either silently reassign
// somebody's organization or fail halfway through a push, and silent
// reassignment in an IAM system is the worse of the two.
//
// So /ServiceProviderConfig and /ResourceTypes advertise exactly what is
// here — no Group resource type — which is what makes an identity provider's
// own configuration screen show users-only provisioning rather than letting
// an administrator discover it when a sync half-works. Both Okta and Entra
// support that configuration as a first-class option.
//
// # Deviations worth knowing
//
// DELETE /Users/{id} disables the account rather than deleting the row.
// Portico disables and never deletes, so that the audit trail keeps naming
// something that exists; a provisioning system asking for deletion is asking
// for deprovisioning, and gets it. This is common practice among SCIM
// implementations and is stated in ServiceProviderConfig's documentationUri.
package scim

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// MediaType is SCIM's own content type. A client that sends application/json
// is accommodated; responses always use this one.
const MediaType = "application/scim+json"

// Schema URNs, from RFC 7643.
const (
	SchemaUser          = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaListResponse  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError         = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaPatchOp       = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaServiceConfig = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	SchemaResourceType  = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
)

// Error is SCIM's error body. It is not Portico's envelope, deliberately:
// a provisioning client parses this shape and reports it to an operator, and
// giving it something else means the operator sees "unexpected response"
// instead of what went wrong.
type Error struct {
	Schemas []string `json:"schemas"`
	// ScimType is the machine-readable reason, from RFC 7644 §3.12. It is
	// what turns "400 Bad Request" in a sync log into a line naming the
	// attribute the identity provider sent that this server will not take.
	ScimType string `json:"scimType,omitempty"`
	Detail   string `json:"detail"`
	Status   string `json:"status"`
}

// SCIM error types used here, from RFC 7644 §3.12.
const (
	// TypeInvalidPath is the answer to a PATCH path this server does not
	// implement. RFC 7644 requires 400 with this type; returning 501 instead
	// is a common shortcut and a bad one, because a sync log then says the
	// server is broken rather than naming the attribute nobody can set.
	TypeInvalidPath = "invalidPath"
	// TypeInvalidValue is a syntactically fine request with a value this
	// server will not accept.
	TypeInvalidValue = "invalidValue"
	// TypeInvalidSyntax is a body that is not a valid SCIM request.
	TypeInvalidSyntax = "invalidSyntax"
	// TypeUniqueness is a conflict on an attribute declared unique.
	TypeUniqueness = "uniqueness"
	// TypeMutability is an attempt to change something read-only.
	TypeMutability = "mutability"
	// TypeNoTarget is a PATCH whose path matched nothing.
	TypeNoTarget = "noTarget"
)

// WriteError renders a SCIM error.
func WriteError(w http.ResponseWriter, r *http.Request, status int, scimType, detail string) {
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "scim request failed",
			"status", status, "detail", detail,
			"method", r.Method, "path", r.URL.Path)
		// The detail a client sees for a server fault is generic, on the same
		// reasoning as the rest of the API: a driver error can carry a host,
		// a port, and sometimes a user.
		detail = "An unexpected error occurred."
	}

	writeJSON(w, status, Error{
		Schemas: []string{SchemaError},
		// SCIM sends the status as a string in the body as well as on the
		// response line. Clients read one or the other; some read both and
		// complain if they disagree.
		Status:   strconv.Itoa(status),
		ScimType: scimType,
		Detail:   detail,
	})
}

// WriteResource renders a resource with the SCIM media type.
func WriteResource(w http.ResponseWriter, status int, body any) {
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", MediaType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line has already gone out; nothing else is possible.
		slog.Error("write scim response", "error", err)
	}
}

// BearerToken extracts the credential from an Authorization header.
//
// Case-insensitive on the scheme, because RFC 7235 says the scheme is a
// case-insensitive token and at least one provisioning client sends "bearer".
func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
