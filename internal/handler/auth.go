package handler

import (
	"net/http"
	"time"

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

// registerResponse is the new account, plus whether it can be used yet.
//
// Embedded rather than nested, so this stays a superset of what registration
// returned before and no existing client has to change to keep working.
type registerResponse struct {
	model.User
	// VerificationRequired tells the screen to say "check your email"
	// instead of "you can sign in now". Without it the screen would have to
	// guess from the tenant's settings, which it cannot read.
	VerificationRequired bool `json:"verificationRequired,omitempty"`
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

	required, err := h.verification.Required(r.Context(), tenant.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Checked before the account exists, not after. The setting was
	// validated when it was turned on, but SMTP can be taken out of the
	// environment afterwards — and creating an account that can never be
	// verified is worse than refusing the registration, because the username
	// and the address are then taken by something nobody can use.
	if required && !h.settings.CanDeliver() {
		httpx.Fail(w, r, service.ErrVerificationUnavailable)
		return
	}

	user, err := h.users.Register(r.Context(), tenant.ID, toRegisterInput(req), httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if required {
		if err := h.verification.Send(r.Context(), tenant, user.ID); err != nil {
			// The account exists and cannot be used. Say so rather than
			// reporting success: the person would otherwise wait for a
			// message that was never sent. Resend is their way forward.
			httpx.Fail(w, r, err)
			return
		}
	}

	httpx.OK(w, registerResponse{User: user, VerificationRequired: required})
}

type verifyRequest struct {
	Tenant string `json:"tenant"`
	Token  string `json:"token"`
}

// ConfirmRegistration redeems a verification link.
//
// Public by necessity: the account cannot sign in until this succeeds, which
// is the whole point of it.
func (h *Handler) ConfirmRegistration(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.verification.Confirm(r.Context(), tenant.ID, req.Token, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"verified": true})
}

type resendVerificationRequest struct {
	Tenant string `json:"tenant"`
	// Destination is the email address or phone number given at
	// registration, not a username. The lookup is against the contact
	// columns for the same reason password recovery's is: resolving across
	// all three identifiers would let one account's email, equal to
	// another's username, send that other account's link to whoever typed
	// it.
	Destination string `json:"destination"`
}

// ResendVerification sends another link.
//
// Answers the same thing whether or not an account was found, whether or not
// it still needs verifying, and whether or not the message went out. This
// endpoint is public and unauthenticated, so anything else would make it an
// oracle for "does this address have an account here".
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	h.verification.Resend(r.Context(), tenant, req.Destination, httpx.ClientIP(r))
	httpx.OK(w, map[string]any{"sent": true})
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

// meResponse is the caller's own profile plus the one thing about their
// account they cannot see anywhere else.
//
// The user is embedded rather than nested, so this stays a superset of what
// /users/me returned before and no client has to be changed to keep working.
type meResponse struct {
	model.User
	// PasswordExpiresAt is when this password stops working, absent when
	// passwords do not expire in this tenant. The instant, not the policy:
	// the policy is administrator-only, and somebody does not need to be
	// told the rules to be told their own deadline.
	PasswordExpiresAt *time.Time `json:"passwordExpiresAt,omitempty"`
}

// Me returns the caller's own profile.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	user, err := h.users.Get(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Best effort. Failing to work out an expiry date is not a reason to
	// refuse somebody their own profile, and the screens that use this all
	// treat its absence as "no expiry", which is the common case anyway.
	expiresAt, err := h.users.PasswordExpiryFor(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		expiresAt = nil
	}

	httpx.OK(w, meResponse{User: user, PasswordExpiresAt: expiresAt})
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

type closeAccountRequest struct {
	// The password, for the same reason changing one requires it: a stolen
	// token must not be enough to destroy the account it was stolen from.
	Password string `json:"password"`
}

// CloseOwnAccount is the one sanctioned way to disable yourself.
//
// Everywhere else it is refused, so that an administrator cannot lock
// themselves out by accident. This is the case that rule was never about:
// somebody deliberately leaving, having confirmed with their password.
//
// It deactivates rather than deletes. The account stops signing in, every
// session and federated refresh token dies immediately, and an administrator
// can reinstate it — which an anonymizing deletion could not.
func (h *Handler) CloseOwnAccount(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req closeAccountRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.users.CloseOwnAccount(r.Context(), principal, req.Password, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The token this request arrived with is already dead. Say so, rather
	// than letting the client's next call fail without explanation.
	httpx.OK(w, map[string]any{"closed": true})
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
