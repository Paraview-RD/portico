package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Directory connectors: the administrative side of reading accounts out of
// an AD or OpenLDAP. The synchronization itself is in the service; this
// layer registers, edits, and triggers.

type directoryRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`

	BindDN string `json:"bindDn"`
	// BindPassword is a pointer so that "not sent" and "sent as empty" stay
	// different requests. An edit form cannot display the stored credential,
	// so submitting it unchanged must leave it alone; sending an explicit
	// empty string is how an operator moves to an anonymous bind.
	//
	// It is write-only. No response carries it back, and the model type does
	// not have a field for it at all, so there is no shape in which somebody
	// forgetting a `json:"-"` would leak it.
	BindPassword *string `json:"bindPassword"`

	BaseDN     string `json:"baseDn"`
	UserFilter string `json:"userFilter"`

	AttrUsername    string `json:"attrUsername"`
	AttrDisplayName string `json:"attrDisplayName"`
	AttrEmail       string `json:"attrEmail"`
	AttrPhone       string `json:"attrPhone"`
	AttrExternalID  string `json:"attrExternalId"`

	OrganizationID string `json:"organizationId"`

	// SyncIntervalMinutes turns automatic synchronization on. Omitting it
	// means zero, which means off — the same as it has always been, so an
	// integration written against the previous version keeps working and does
	// not start reading a directory on a timer because it was upgraded.
	SyncIntervalMinutes int `json:"syncIntervalMinutes"`
}

func (r directoryRequest) input() service.LDAPSourceInput {
	return service.LDAPSourceInput{
		Name: r.Name, Host: r.Host, Port: r.Port, Encryption: r.Encryption,
		BindDN: r.BindDN, BindPassword: r.BindPassword,
		BaseDN: r.BaseDN, UserFilter: r.UserFilter,
		AttrUsername: r.AttrUsername, AttrDisplayName: r.AttrDisplayName,
		AttrEmail: r.AttrEmail, AttrPhone: r.AttrPhone,
		AttrExternalID: r.AttrExternalID,
		OrganizationID: r.OrganizationID,

		SyncIntervalMinutes: r.SyncIntervalMinutes,
	}
}

// ListDirectories returns the tenant's directory connectors.
func (h *Handler) ListDirectories(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	sources, err := h.directories.List(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, sources)
}

// GetDirectory returns one connector.
func (h *Handler) GetDirectory(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	source, err := h.directories.Get(r.Context(), principal.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, source)
}

// CreateDirectory registers a directory to read accounts out of.
func (h *Handler) CreateDirectory(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req directoryRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	source, err := h.directories.Register(r.Context(), principal, req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, source)
}

// UpdateDirectory changes a connector's settings.
func (h *Handler) UpdateDirectory(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req directoryRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	source, err := h.directories.Update(r.Context(), principal, chi.URLParam(r, "id"), req.input())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, source)
}

// EnableDirectory puts a connector back into service.
func (h *Handler) EnableDirectory(w http.ResponseWriter, r *http.Request) {
	h.setDirectoryStatus(w, r, model.StatusActive)
}

// DisableDirectory stops a connector synchronizing.
//
// It does not touch the accounts that came from it. The connector and the
// people are different things, and making one control both would fight with
// reactivation — which is what treating a directory as the source of truth
// has to mean when somebody comes back from leave.
func (h *Handler) DisableDirectory(w http.ResponseWriter, r *http.Request) {
	h.setDirectoryStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setDirectoryStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	source, err := h.directories.SetStatus(r.Context(), principal, chi.URLParam(r, "id"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, source)
}

// SyncDirectory reads the directory now and reconciles what it returns.
//
// Synchronous, and deliberately so: an administrator who presses the button is
// watching, and a background job would mean the counts arrive somewhere they
// are not looking.
//
// This stays the manual path now that a schedule exists. The unattended runs
// go through service.SyncDue, which shares everything below this line — the
// same reconciliation, the same run record, the same refusal to act on an
// empty result — and differs only in having no actor to record and nobody to
// answer to.
func (h *Handler) SyncDirectory(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	run, err := h.directories.SyncNow(r.Context(), principal, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// A failed run is a successful request: the sync ran, it did not work,
	// and the reason is in the body. Returning 500 would make the console
	// show a generic failure instead of the count and the message.
	httpx.OK(w, run)
}

// ListDirectoryRuns returns a connector's recent synchronizations.
func (h *Handler) ListDirectoryRuns(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	runs, err := h.directories.Runs(r.Context(), principal.TenantID, chi.URLParam(r, "id"), limit)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, runs)
}
