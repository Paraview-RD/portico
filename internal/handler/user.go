package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// ListUsers returns a filtered page of users.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())
	pagination := httpx.ParsePagination(r)
	query := r.URL.Query()

	users, total, err := h.users.List(r.Context(), principal.TenantID, service.UserQuery{
		Keyword:        query.Get("keyword"),
		Status:         model.Status(query.Get("status")),
		Role:           model.Role(query.Get("role")),
		OrganizationID: query.Get("organizationId"),
	}, service.Page{Limit: pagination.Limit(), Offset: pagination.Offset()})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, httpx.NewPageResult(users, total, pagination))
}

// GetUser returns one user.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	user, err := h.users.Get(r.Context(), principal.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

type createUserRequest struct {
	Username       string `json:"username"`
	DisplayName    string `json:"displayName"`
	Password       string `json:"password"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	OrganizationID string `json:"organizationId"`
}

// CreateUser adds an account.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req createUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	role := model.Role(req.Role)
	if role == "" {
		role = model.RoleUser
	}

	user, err := h.users.Create(r.Context(), principal.TenantID, service.CreateUserInput{
		Username:       req.Username,
		DisplayName:    req.DisplayName,
		Password:       req.Password,
		Phone:          req.Phone,
		Email:          req.Email,
		Role:           role,
		OrganizationID: req.OrganizationID,
		Source:         model.SourceAdmin,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	h.audit.Log(r.Context(), principal.TenantID, service.AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionUserCreate,
		ActorID: principal.UserID, ActorName: principal.Username,
		TargetType: "USER", TargetID: user.ID, TargetName: user.Username,
		Detail: "created by administrator", IP: httpx.ClientIP(r),
	})

	httpx.OK(w, user)
}

type updateUserRequest struct {
	DisplayName    string `json:"displayName"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	OrganizationID string `json:"organizationId"`
}

// UpdateUser changes a user's profile, role, and organization.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req updateUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := h.users.Update(r.Context(), principal, chi.URLParam(r, "id"), service.UpdateUserInput{
		DisplayName:    req.DisplayName,
		Phone:          req.Phone,
		Email:          req.Email,
		Role:           model.Role(req.Role),
		OrganizationID: req.OrganizationID,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

// EnableUser reactivates an account.
func (h *Handler) EnableUser(w http.ResponseWriter, r *http.Request) {
	h.setUserStatus(w, r, model.StatusActive)
}

// DisableUser deactivates an account, revoking its live sessions.
//
// This is the MVP's substitute for deletion: accounts are never physically
// removed, so the audit trail stays intact (§3.1).
func (h *Handler) DisableUser(w http.ResponseWriter, r *http.Request) {
	h.setUserStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setUserStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	principal := auth.MustPrincipal(r.Context())

	user, err := h.users.SetStatus(r.Context(), principal, chi.URLParam(r, "id"), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}

type resetPasswordRequest struct {
	NewPassword string `json:"newPassword"`
}

// ResetUserPassword sets another account's password.
func (h *Handler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req resetPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err := h.users.ResetPassword(r.Context(), principal, chi.URLParam(r, "id"), req.NewPassword, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}
