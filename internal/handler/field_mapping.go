package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/service"
)

// What each recipient receives, and under what name.
//
// One pair of handlers for all four kinds rather than four pairs. The rules are
// the same rules and the refusals are the same refusals; what differs is which
// table the id is looked up in, and that is a parameter rather than a copy. The
// repository has the receipt for the alternative — the tile-picture field was
// written out once per registration form, and that is how the three went out of
// step with each other.

type fieldMappingRequest struct {
	Mappings []fieldMappingRule `json:"mappings"`
}

type fieldMappingRule struct {
	SourceKey string `json:"sourceKey"`
	// TargetName is the name this recipient expects. Required unless the rule
	// is suppressing the field.
	TargetName string `json:"targetName"`
	// FriendlyName is SAML's second, human-readable name. Ignored elsewhere.
	FriendlyName string `json:"friendlyName"`
	// Suppressed removes a name the defaults would have sent. A flag rather
	// than an empty target: "send nothing" and "send under a name I have not
	// chosen yet" are different intentions.
	Suppressed bool `json:"suppressed"`
}

func (r fieldMappingRequest) inputs() []service.FieldMappingInput {
	out := make([]service.FieldMappingInput, 0, len(r.Mappings))
	for _, rule := range r.Mappings {
		out = append(out, service.FieldMappingInput{
			SourceKey: rule.SourceKey, TargetName: rule.TargetName,
			FriendlyName: rule.FriendlyName, Suppressed: rule.Suppressed,
		})
	}
	return out
}

// ListFieldMappings returns one recipient's rules.
//
// An empty list is the ordinary answer and means the recipient receives the
// documented defaults. It is not the same as a recipient that does not exist,
// which is why the id is resolved rather than trusted.
func (h *Handler) ListFieldMappings(kind service.RecipientKind, param string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := auth.MustPrincipal(r.Context())

		ref, err := h.fieldMappings.Recipient(r.Context(), principal.TenantID, kind, chi.URLParam(r, param))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}

		mappings, err := h.fieldMappings.Mappings(r.Context(), principal.TenantID, ref)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.OK(w, mappings)
	}
}

// ReplaceFieldMappings writes one recipient's whole set.
//
// PUT rather than POST, and a replacement rather than a merge, because what is
// being sent is a table somebody edited: merging would leave the rows the form
// deleted still in place, which is the one outcome nobody expects from a save.
func (h *Handler) ReplaceFieldMappings(kind service.RecipientKind, param string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := auth.MustPrincipal(r.Context())

		ref, err := h.fieldMappings.Recipient(r.Context(), principal.TenantID, kind, chi.URLParam(r, param))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}

		var req fieldMappingRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.Fail(w, r, err)
			return
		}

		mappings, err := h.fieldMappings.Replace(r.Context(), principal, ref, req.inputs())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.OK(w, mappings)
	}
}
