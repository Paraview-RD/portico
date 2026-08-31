package handler

import (
	"net/http"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
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
		Actor:   query.Get("actor"),
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
	// The console's own session. Named apart from the three below because the
	// two were conflated for a whole version: this one governs how often
	// somebody signing in here is asked again, and has nothing to do with
	// what registered applications receive.
	TokenTTLMinutes *int `json:"tokenTtlMinutes"`

	// The OIDC lifetimes. Pointers like everything else here, and for the
	// session cap that is not a formality — a plain value would arrive as 0
	// on every save from an unrelated part of the page, and 0 switches the
	// cap off. Renaming the system would quietly disable it.
	OIDCAccessTokenTTLMinutes *int `json:"oidcAccessTokenTtlMinutes"`
	OIDCRefreshTokenTTLDays   *int `json:"oidcRefreshTokenTtlDays"`
	OIDCSessionMaxAgeDays     *int `json:"oidcSessionMaxAgeDays"`

	RegistrationEnabled *bool `json:"registrationEnabled"`
	ShowGuides          *bool `json:"showGuides"`
	// Requiring a self-registered account to prove its address. Refused
	// where this deployment has no way to send one, rather than accepted and
	// then stranding every registration on a message that never arrives.
	RegistrationVerification *bool   `json:"registrationVerification"`
	SystemName               *string `json:"systemName"`

	// Branding, all optional. Empty string is a real value here too — it
	// means "not customized" and falls back to the default — so omitting
	// the field and sending "" are different, same reasoning as
	// DefaultLocale below.
	BrandingLogoURL          *string `json:"brandingLogoUrl"`
	BrandingProductName      *string `json:"brandingProductName"`
	BrandingColorPrimary     *string `json:"brandingColorPrimary"`
	BrandingFontFamily       *string `json:"brandingFontFamily"`
	BrandingBgImageURL       *string `json:"brandingBgImageUrl"`
	BrandingFooterPrivacyURL *string `json:"brandingFooterPrivacyUrl"`
	BrandingFooterTermsURL   *string `json:"brandingFooterTermsUrl"`
	BrandingFooterSupportURL *string `json:"brandingFooterSupportUrl"`
	BrandingLoginHeading     *string `json:"brandingLoginHeading"`

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

	// 0 keeps everything, which is the default and the only safe ship.
	AuditRetentionDays *int `json:"auditRetentionDays"`

	// The language of messages this tenant sends to somebody who has stated
	// no preference. An empty string is a real value here — it means "follow
	// the deployment" — which is why omitting the field and sending "" have
	// to be different things, and why this is a pointer like the rest.
	DefaultLocale *string `json:"defaultLocale"`
}

// applyTo overlays whatever the request actually carried onto the settings
// as they stand.
func (req updateSettingsRequest) applyTo(current service.Settings) service.Settings {
	overlayInt(&current.TokenTTLMinutes, req.TokenTTLMinutes)
	overlayInt(&current.OIDCAccessTokenTTLMinutes, req.OIDCAccessTokenTTLMinutes)
	overlayInt(&current.OIDCRefreshTokenTTLDays, req.OIDCRefreshTokenTTLDays)
	overlayInt(&current.OIDCSessionMaxAgeDays, req.OIDCSessionMaxAgeDays)
	overlayBool(&current.RegistrationEnabled, req.RegistrationEnabled)
	overlayBool(&current.ShowGuides, req.ShowGuides)
	overlayBool(&current.RegistrationVerification, req.RegistrationVerification)
	if req.SystemName != nil {
		current.SystemName = *req.SystemName
	}
	if req.DefaultLocale != nil {
		current.DefaultLocale = *req.DefaultLocale
	}

	overlayString(&current.BrandingLogoURL, req.BrandingLogoURL)
	overlayString(&current.BrandingProductName, req.BrandingProductName)
	overlayString(&current.BrandingColorPrimary, req.BrandingColorPrimary)
	overlayString(&current.BrandingFontFamily, req.BrandingFontFamily)
	overlayString(&current.BrandingBgImageURL, req.BrandingBgImageURL)
	overlayString(&current.BrandingFooterPrivacyURL, req.BrandingFooterPrivacyURL)
	overlayString(&current.BrandingFooterTermsURL, req.BrandingFooterTermsURL)
	overlayString(&current.BrandingFooterSupportURL, req.BrandingFooterSupportURL)
	overlayString(&current.BrandingLoginHeading, req.BrandingLoginHeading)

	overlayInt(&current.LockoutThreshold, req.LockoutThreshold)
	overlayInt(&current.LockoutDurationMinutes, req.LockoutDurationMinutes)

	overlayInt(&current.PasswordMinLength, req.PasswordMinLength)
	overlayBool(&current.PasswordRequireUppercase, req.PasswordRequireUppercase)
	overlayBool(&current.PasswordRequireLowercase, req.PasswordRequireLowercase)
	overlayBool(&current.PasswordRequireDigit, req.PasswordRequireDigit)
	overlayBool(&current.PasswordRequireSymbol, req.PasswordRequireSymbol)
	overlayInt(&current.PasswordHistoryDepth, req.PasswordHistoryDepth)
	overlayInt(&current.PasswordMaxAgeDays, req.PasswordMaxAgeDays)
	overlayInt(&current.AuditRetentionDays, req.AuditRetentionDays)

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

func overlayString(target *string, supplied *string) {
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
