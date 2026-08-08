package handler

import (
	"net/http"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

type loginRequest struct {
	// Tenant is the tenant code. Empty means the default tenant, so a
	// single-tenant deployment never has to send it.
	Tenant string `json:"tenant"`
	// Identifier is a username, email address, or phone number. It is one
	// field rather than three because they are three ways of naming an
	// account, not three kinds of sign-in — the caller should not have to
	// tell the server which one they typed.
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// Login authenticates a user within a tenant and returns a token.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	session, err := h.users.Login(r.Context(), tenant, req.Identifier, req.Password,
		httpx.ClientIP(r), userAgent(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, session)
}

type changeExpiredPasswordRequest struct {
	Tenant          string `json:"tenant"`
	Identifier      string `json:"identifier"`
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangeExpiredPassword lets somebody whose password has aged out replace it
// and sign in, in one step.
//
// Public by necessity, like password recovery: the caller cannot sign in,
// which is the whole problem. It is not a way around authentication — it
// takes the current password, applies the same lockout accounting a sign-in
// would, and refuses outright if the password has not actually expired.
func (h *Handler) ChangeExpiredPassword(w http.ResponseWriter, r *http.Request) {
	var req changeExpiredPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	session, err := h.users.ChangeExpiredPassword(r.Context(), tenant,
		req.Identifier, req.CurrentPassword, req.NewPassword,
		httpx.ClientIP(r), userAgent(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, session)
}

// Logout revokes every token held by the caller.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	if err := h.users.Logout(r.Context(), principal, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

type registerRequest struct {
	Tenant      string `json:"tenant"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
}

// Register creates an account from a public sign-up.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := h.users.Register(r.Context(), tenant.ID, toRegisterInput(req), httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

// RegistrationStatus tells an anonymous caller whether sign-up is open, so
// the login screen can show or hide the register link.
//
// Both answers are per tenant: one tenant may accept sign-ups while another
// does not, and each names itself.
func (h *Handler) RegistrationStatus(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolvePublicTenant(r, "")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	settings, err := h.settings.Get(r.Context(), tenant.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Only the fields a signed-out caller needs; the rest of the settings
	// are administrator-only.
	httpx.OK(w, map[string]any{
		"registrationEnabled": settings.RegistrationEnabled,
		"systemName":          settings.SystemName,
		"tenant":              tenant.Code,
		"tenantName":          tenant.Name,
	})
}

// Me returns the caller's own profile.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	user, err := h.users.Get(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

type updateProfileRequest struct {
	DisplayName string `json:"displayName"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
}

// UpdateOwnProfile lets the caller maintain their own details (§3.5).
//
// The request type has three fields and no more. Role, status, organization,
// and username are not editable here at any price — the first would be a
// privilege-escalation endpoint and the rest are administrative decisions.
func (h *Handler) UpdateOwnProfile(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateProfileRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := h.users.UpdateOwnProfile(r.Context(), principal, service.ProfileInput{
		DisplayName: req.DisplayName,
		Phone:       req.Phone,
		Email:       req.Email,
	}, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangeOwnPassword updates the caller's password.
func (h *Handler) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req changePasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err := h.users.ChangeOwnPassword(r.Context(), principal, req.CurrentPassword, req.NewPassword, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The password change revoked this token too, so the client must sign
	// in again; say so rather than letting the next call fail mysteriously.
	httpx.OK(w, map[string]any{"reauthenticationRequired": true})
}

// CheckPermission reports whether the caller holds the administrator role,
// and in which tenant. Downstream systems use this to gate their own admin
// screens (§3.7).
func (h *Handler) CheckPermission(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	httpx.OK(w, map[string]any{
		"userId":   principal.UserID,
		"tenantId": principal.TenantID,
		"username": principal.Username,
		"role":     principal.Role,
		"isAdmin":  principal.Role == model.RoleSuperAdmin,
	})
}

func toRegisterInput(req registerRequest) service.RegisterInput {
	return service.RegisterInput{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Phone:       req.Phone,
		Email:       req.Email,
	}
}

// LogoutEverywhere ends every session the caller holds, on every device.
func (h *Handler) LogoutEverywhere(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	if err := h.users.LogoutEverywhere(r.Context(), principal, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// userAgent is what a session list shows so somebody can recognize their own
// sessions.
//
// Truncated, because it is an attacker-controlled header with no length
// limit and there is no reason to store an unbounded string. Never parsed
// into a browser name: that is a guessing game the string does not support,
// and a wrong guess is worse than the raw value for the one question this
// answers — "was that me".
const maxUserAgentLength = 400

func userAgent(r *http.Request) string {
	ua := r.UserAgent()
	if len(ua) > maxUserAgentLength {
		return ua[:maxUserAgentLength]
	}
	return ua
}
