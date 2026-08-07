package handler

import (
	"net/http"
	"time"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// ListAuditLogs returns a filtered page of the audit trail.
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	pagination := httpx.ParsePagination(r)
	query := r.URL.Query()

	kind := model.LogKind(query.Get("kind"))
	if kind != "" && !kind.Valid() {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_LOG_KIND",
			"Log kind must be one of LOGIN, OPERATION, AUTH, REGISTRATION, ORGANIZATION."))
		return
	}

	from, err := parseTimeParam(query.Get("from"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	to, err := parseTimeParam(query.Get("to"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	logs, total, err := h.audit.List(r.Context(), service.AuditQuery{
		Kind:    kind,
		Action:  query.Get("action"),
		Keyword: query.Get("keyword"),
		From:    from,
		To:      to,
	}, service.Page{Limit: pagination.Limit(), Offset: pagination.Offset()})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, httpx.NewPageResult(logs, total, pagination))
}

// GetSettings returns the runtime settings.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, settings)
}

type updateSettingsRequest struct {
	TokenTTLMinutes     int    `json:"tokenTtlMinutes"`
	RegistrationEnabled bool   `json:"registrationEnabled"`
	SystemName          string `json:"systemName"`
}

// UpdateSettings writes the runtime settings.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateSettingsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	settings, err := h.settings.Update(r.Context(), service.Settings{
		TokenTTLMinutes:     req.TokenTTLMinutes,
		RegistrationEnabled: req.RegistrationEnabled,
		SystemName:          req.SystemName,
	})
	if err != nil {
		// Update returns a typed error for validation failures; anything
		// else is a storage problem and must surface as a 500 rather than
		// reflecting the database's error text back to the caller.
		httpx.Fail(w, r, err)
		return
	}

	h.audit.Log(r.Context(), service.AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSettingsUpdate,
		ActorID: principal.UserID, ActorName: principal.Username,
		IP: httpx.ClientIP(r),
	})

	httpx.OK(w, settings)
}

// parseTimeParam reads an optional RFC 3339 query parameter.
func parseTimeParam(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, httpx.BadRequest("INVALID_TIMESTAMP",
			"Timestamps must be RFC 3339, for example 2026-08-06T14:25:33Z.")
	}
	return parsed, nil
}
