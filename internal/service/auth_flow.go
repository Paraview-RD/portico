package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Session is what a successful login returns.
type Session struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expiresAt"`
	User      model.User `json:"user"`
}

// Login verifies credentials within a tenant and issues a token.
//
// The identifier may be a username, an email address, or a phone number
// (§3.4). All three produce exactly the same session — there is one
// credential check, one token, one audit entry, and nothing downstream can
// tell which was used. That is the requirement: the identifier is a way of
// naming an account, not a kind of account.
//
// The tenant is resolved by the caller before this runs, and is not
// something the credentials can influence: all three identifiers are unique
// per tenant, so "which tenant" has to be settled first or the lookup is
// ambiguous.
//
// Every failure returns the same ErrInvalidCredentials regardless of whether
// the account exists, so the response cannot be used to enumerate accounts.
// A disabled account is the one exception: telling the user their account is
// disabled is more useful than pretending the password is wrong, and the
// account is known to exist anyway once the password matches.
func (s *UserService) Login(ctx context.Context, tenant model.Tenant, identifier, password, ip string) (Session, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || password == "" {
		return Session{}, httpx.BadRequest("MISSING_CREDENTIALS",
			"An identifier and password are required.")
	}

	q := s.store.ForTenant(tenant.ID)

	row, err := q.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		if store.IsNoRows(err) {
			// Spend the same time as a real password check so response
			// timing does not reveal which accounts exist.
			auth.BurnPasswordComparison()
			s.logLoginFailure(ctx, tenant.ID, "", identifier, ip, "no such user")
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("look up user: %w", err)
	}

	if !auth.CheckPassword(row.PasswordHash, password) {
		// The audit entry records the account, not the identifier that named
		// it — the trail is about who, and an email and a username reaching
		// the same account are the same event.
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "wrong password")
		return Session{}, ErrInvalidCredentials
	}

	if model.Status(row.Status) != model.StatusActive {
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account disabled")
		return Session{}, ErrAccountDisabled
	}

	user, err := s.Get(ctx, tenant.ID, row.ID)
	if err != nil {
		return Session{}, err
	}

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return Session{}, err
	}

	token, expiresAt, err := s.tokens.Issue(user, tenant.Code, row.TokenVersion, settings.TokenTTL())
	if err != nil {
		return Session{}, err
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLoginSuccess,
		Result:  model.LogSuccess,
		ActorID: user.ID, ActorName: user.Username,
		IP: ip,
	})

	return Session{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

func (s *UserService) logLoginFailure(ctx context.Context, tenantID, userID, username, ip, reason string) {
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLoginFailure,
		Result:  model.LogFailure,
		ActorID: userID, ActorName: username,
		Detail: reason, IP: ip,
	})
}

// Logout invalidates every token currently held by the caller.
//
// With stateless tokens there is nothing to delete, so the account's
// token_version is bumped instead: existing tokens carry the old value and
// stop verifying on their next request.
func (s *UserService) Logout(ctx context.Context, actor auth.Principal, ip string) error {
	err := s.store.ForTenant(actor.TenantID).BumpUserTokenVersion(ctx,
		sqlcgen.BumpUserTokenVersionParams{
			ID:        actor.UserID,
			UpdatedAt: store.Now(),
		})
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLogout,
		ActorID: actor.UserID, ActorName: actor.Username,
		IP: ip,
	})
	return nil
}

// ChangeOwnPassword lets a signed-in user replace their password after
// proving they know the current one.
func (s *UserService) ChangeOwnPassword(ctx context.Context, actor auth.Principal, currentPassword, newPassword, ip string) error {
	q := s.store.ForTenant(actor.TenantID)

	row, err := q.GetUserByID(ctx, actor.UserID)
	if err != nil {
		if store.IsNoRows(err) {
			return ErrUserNotFound
		}
		return fmt.Errorf("get user: %w", err)
	}

	// Requiring the current password is what stops a stolen token from being
	// escalated into permanent account takeover.
	if !auth.CheckPassword(row.PasswordHash, currentPassword) {
		s.audit.Log(ctx, actor.TenantID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionPasswordSelf,
			Result:  model.LogFailure,
			ActorID: actor.UserID, ActorName: actor.Username,
			Detail: "current password did not match", IP: ip,
		})
		return httpx.UnprocessableEntity("CURRENT_PASSWORD_MISMATCH",
			"The current password is incorrect.")
	}

	if err := s.setPassword(ctx, q, actor.UserID, newPassword); err != nil {
		return err
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordSelf,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: actor.UserID, TargetName: actor.Username,
		IP: ip,
	})
	return nil
}

// ResetPassword lets an administrator set another account's password without
// knowing the old one.
func (s *UserService) ResetPassword(ctx context.Context, actor auth.Principal, userID, newPassword, ip string) error {
	q := s.store.ForTenant(actor.TenantID)

	target, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return ErrUserNotFound
		}
		return fmt.Errorf("get user: %w", err)
	}

	if err := s.setPassword(ctx, q, userID, newPassword); err != nil {
		return err
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordReset,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: userID, TargetName: target.Username,
		IP: ip,
	})
	return nil
}

// setPassword validates, hashes, and stores a new password. The query bumps
// token_version, so changing a password signs the account out everywhere.
func (s *UserService) setPassword(ctx context.Context, q *store.Scoped, userID, plaintext string) error {
	if err := auth.ValidatePassword(plaintext); err != nil {
		return httpx.BadRequest("WEAK_PASSWORD", err.Error())
	}

	hash, err := auth.HashPassword(plaintext)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	err = q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: hash,
		UpdatedAt:    store.Now(),
	})
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// RegisterInput is a self-service sign-up.
type RegisterInput struct {
	Username    string
	DisplayName string
	Password    string
	Phone       string
	Email       string
}

// Register creates an account from a public sign-up request, in the tenant
// the caller named (or the default one).
//
// The role is always USER and is never taken from the request: letting a
// caller pick their own role would make the whole permission model
// meaningless. Organization is left empty for an administrator to fill in
// later (§3.4.2).
func (s *UserService) Register(ctx context.Context, tenantID string, in RegisterInput, ip string) (model.User, error) {
	enabled, err := s.settings.RegistrationEnabled(ctx, tenantID)
	if err != nil {
		return model.User{}, err
	}
	if !enabled {
		return model.User{}, ErrRegistrationDisabled
	}

	user, err := s.Create(ctx, tenantID, CreateUserInput{
		Username:    in.Username,
		DisplayName: in.DisplayName,
		Password:    in.Password,
		Phone:       in.Phone,
		Email:       in.Email,
		Role:        model.RoleUser,
		Source:      model.SourceRegistration,
	})
	if err != nil {
		return model.User{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionUserSelfReg,
		ActorID: user.ID, ActorName: user.Username,
		TargetType: "USER", TargetID: user.ID, TargetName: user.Username,
		Detail: "self-service registration", IP: ip,
	})

	return user, nil
}

// EnsureInitialAdmin creates the bootstrap administrator when a tenant has
// no users at all, and reports the generated password so it can be printed
// once at startup.
//
// The check is per tenant, not per deployment: every tenant needs its own
// first administrator, since no account can administer more than one. That
// is also what lets the provisioning CLI reuse this when creating a tenant.
//
// Returning the password rather than storing it anywhere is deliberate: it
// exists only in the startup output, and the operator is expected to change
// it.
func (s *UserService) EnsureInitialAdmin(ctx context.Context, tenantID, username, password string) (created bool, generatedPassword string, err error) {
	count, err := s.store.ForTenant(tenantID).CountUsers(ctx)
	if err != nil {
		return false, "", fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return false, "", nil
	}

	if username == "" {
		username = "admin"
	}
	generated := false
	if password == "" {
		// A random password beats a well-known default: an instance that is
		// reachable before anyone finishes setup should not be trivially
		// accessible.
		password = uuid.NewString()
		generated = true
	}

	if _, err := s.Create(ctx, tenantID, CreateUserInput{
		Username:    username,
		DisplayName: "Administrator",
		Password:    password,
		Role:        model.RoleSuperAdmin,
		Source:      model.SourceAdmin,
	}); err != nil {
		return false, "", fmt.Errorf("create initial administrator: %w", err)
	}

	if generated {
		return true, password, nil
	}
	return true, "", nil
}
