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
	Tenant   string `json:"tenant"`
	Username string `json:"username"`
	Password string `json:"password"`
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

	session, err := h.users.Login(r.Context(), tenant, req.Username, req.Password, httpx.ClientIP(r))
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
