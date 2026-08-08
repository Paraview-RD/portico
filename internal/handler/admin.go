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
	principal := auth.MustPrincipal(r.Context())
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

	logs, total, err := h.audit.List(r.Context(), principal.TenantID, service.AuditQuery{
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
	principal := auth.MustPrincipal(r.Context())

	settings, err := h.settings.Get(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, settings)
}

// updateSettingsRequest is every setting, all optional.
//
// Pointers rather than values, and this is a security property rather than
// a convenience. Settings grow: lockout arrived after this endpoint
// existed, the password policy after that. With plain values, a client
// written against the older shape omits the new fields, Go decodes them as
// zero, and the update silently switches lockout off — a downgrade nobody
// asked for, from a request that looks like it only changed the system
// name. Absent has to mean "leave it alone", and only a pointer can say
// that.
type updateSettingsRequest struct {
	TokenTTLMinutes     *int    `json:"tokenTtlMinutes"`
	RegistrationEnabled *bool   `json:"registrationEnabled"`
	SystemName          *string `json:"systemName"`

	// Zero threshold switches lockout off, which a deployment that trusts
	// its reverse proxy's throttling may well want — but it has to be sent
	// deliberately, not arrived at by omission.
	LockoutThreshold       *int `json:"lockoutThreshold"`
	LockoutDurationMinutes *int `json:"lockoutDurationMinutes"`

	// Composition rules and expiry are off by default and stay that way
	// unless somebody asks for them; see service/password_policy.go.
	PasswordMinLength        *int  `json:"passwordMinLength"`
	PasswordRequireUppercase *bool `json:"passwordRequireUppercase"`
	PasswordRequireLowercase *bool `json:"passwordRequireLowercase"`
	PasswordRequireDigit     *bool `json:"passwordRequireDigit"`
	PasswordRequireSymbol    *bool `json:"passwordRequireSymbol"`
	PasswordHistoryDepth     *int  `json:"passwordHistoryDepth"`
	PasswordMaxAgeDays       *int  `json:"passwordMaxAgeDays"`
}

// applyTo overlays whatever the request actually carried onto the settings
// as they stand.
func (req updateSettingsRequest) applyTo(current service.Settings) service.Settings {
	overlayInt(&current.TokenTTLMinutes, req.TokenTTLMinutes)
	overlayBool(&current.RegistrationEnabled, req.RegistrationEnabled)
	if req.SystemName != nil {
		current.SystemName = *req.SystemName
	}

	overlayInt(&current.LockoutThreshold, req.LockoutThreshold)
	overlayInt(&current.LockoutDurationMinutes, req.LockoutDurationMinutes)

	overlayInt(&current.PasswordMinLength, req.PasswordMinLength)
	overlayBool(&current.PasswordRequireUppercase, req.PasswordRequireUppercase)
	overlayBool(&current.PasswordRequireLowercase, req.PasswordRequireLowercase)
	overlayBool(&current.PasswordRequireDigit, req.PasswordRequireDigit)
	overlayBool(&current.PasswordRequireSymbol, req.PasswordRequireSymbol)
	overlayInt(&current.PasswordHistoryDepth, req.PasswordHistoryDepth)
	overlayInt(&current.PasswordMaxAgeDays, req.PasswordMaxAgeDays)

	return current
}

func overlayInt(target *int, supplied *int) {
	if supplied != nil {
		*target = *supplied
	}
}

func overlayBool(target *bool, supplied *bool) {
	if supplied != nil {
		*target = *supplied
	}
}

// UpdateSettings writes the runtime settings.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateSettingsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	current, err := h.settings.Get(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	settings, err := h.settings.Update(r.Context(), principal.TenantID,
		req.applyTo(current))
	if err != nil {
		// Update returns a typed error for validation failures; anything
		// else is a storage problem and must surface as a 500 rather than
		// reflecting the database's error text back to the caller.
		httpx.Fail(w, r, err)
		return
	}

	h.audit.Log(r.Context(), principal.TenantID, service.AuditEntry{
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
