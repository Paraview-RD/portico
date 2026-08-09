package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
	"github.com/paraview/portico/internal/webhook"
)

// ProfileInput is what a user may change about themselves.
//
// The absent fields are the point. Username is immutable, because downstream
// systems match on it and because it is a sign-in identifier. Role, status,
// and organization are administrative decisions — a self-service endpoint
// that accepted a role would be a privilege-escalation endpoint, and one
// that accepted an organization would let anyone file themselves under any
// department.
type ProfileInput struct {
	DisplayName string
	Phone       string
	Email       string
}

// UpdateOwnProfile lets a signed-in user maintain their own details (§3.5).
//
// Phone and email are sign-in identifiers and password-recovery
// destinations, so they go through the same validation and the same
// per-tenant uniqueness as when an administrator sets them.
//
// Changing either is not verified: a user may set an address they do not
// control. That is bounded — recovery for their own account would then be
// delivered somewhere they cannot read, which locks them out rather than
// letting them in, and the unique index stops them taking an address another
// account in the tenant already holds. Verified changes are a V0.2 item.
func (s *UserService) UpdateOwnProfile(ctx context.Context, actor auth.Principal, in ProfileInput, ip string) (model.User, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return model.User{}, httpx.BadRequest("DISPLAY_NAME_REQUIRED", "A display name is required.")
	}
	if err := validateContactDetails(in.Phone, in.Email); err != nil {
		return model.User{}, err
	}

	q := s.store.ForTenant(actor.TenantID)

	current, err := q.GetUserByID(ctx, actor.UserID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	// The existing role and organization are carried through unchanged
	// rather than taken from the request, so there is no path by which this
	// endpoint could alter either even if the input type grew a field.
	err = q.UpdateUserProfile(ctx, sqlcgen.UpdateUserProfileParams{
		ID:             actor.UserID,
		DisplayName:    in.DisplayName,
		Phone:          strings.TrimSpace(in.Phone),
		Email:          strings.TrimSpace(in.Email),
		OrganizationID: current.OrganizationID,
		Role:           current.Role,
		UpdatedAt:      store.Now(),
	})
	if err != nil {
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("update profile: %w", err)
	}

	updated, err := s.Get(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return model.User{}, err
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionProfileSelf,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: actor.UserID, TargetName: actor.Username,
		Detail: describeContactChange(current, updated),
		IP:     ip,
	})

	return updated, nil
}

// describeContactChange records which of the recovery destinations moved.
//
// This is worth recording specifically: an attacker with a stolen session
// repointing the email is how they turn temporary access into permanent
// access, and "the profile was updated" does not tell an investigator that
// happened. The values are logged because the trail is only useful if it
// says what it changed to.
func describeContactChange(before sqlcgen.User, after model.User) string {
	var changes []string
	if before.Email != after.Email {
		changes = append(changes, fmt.Sprintf("email: %q -> %q", before.Email, after.Email))
	}
	if before.Phone != after.Phone {
		changes = append(changes, fmt.Sprintf("phone: %q -> %q", before.Phone, after.Phone))
	}
	if len(changes) == 0 {
		return ""
	}
	return strings.Join(changes, "; ")
}

// ErrAccountClosed is what a closed account gets at sign-in.
//
// Distinct from ACCOUNT_DISABLED, and worth the extra code: the two call for
// different actions. Somebody an administrator suspended should talk to that
// administrator; somebody who closed their own account and now wants back in
// is asking for a different conversation, and being told "your account is
// disabled" would send them down the wrong path.
var ErrAccountClosed = httpx.Forbidden("ACCOUNT_CLOSED",
	"This account was closed by its owner. An administrator can reinstate it.")

// CloseOwnAccount is the one sanctioned way to disable yourself.
//
// Everywhere else that is refused — ErrCannotDisableSelf exists so an
// administrator cannot lock themselves out by accident. This is not an
// exception to that rule so much as the case it was never about: somebody
// deliberately leaving, having been asked to confirm.
//
// It deactivates rather than deletes, matching every other decision here, so
// the audit trail keeps pointing at an account that exists and an
// administrator can undo a mistake. Whether a deployment needs the
// irreversible kind is a question about personal-data erasure obligations
// rather than about this code.
func (s *UserService) CloseOwnAccount(ctx context.Context, actor auth.Principal, password, ip string) error {
	q := s.store.ForTenant(actor.TenantID)

	row, err := q.GetUserByID(ctx, actor.UserID)
	if err != nil {
		if store.IsNoRows(err) {
			return ErrUserNotFound
		}
		return fmt.Errorf("get user: %w", err)
	}

	// The password, for the same reason changing one requires it: a stolen
	// token must not be enough to destroy the account it was stolen from.
	if !auth.CheckPassword(row.PasswordHash, password) {
		s.audit.Log(ctx, actor.TenantID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionAccountClose,
			Result:  model.LogFailure,
			ActorID: actor.UserID, ActorName: actor.Username,
			Detail: "password did not match", IP: ip,
		})
		return httpx.UnprocessableEntity("CURRENT_PASSWORD_MISMATCH",
			"The password is incorrect.")
	}

	// The tenant has to keep an administrator. Somebody closing the last one
	// would leave nobody who can reinstate them, which is a locked-out
	// tenant recoverable only from the command line.
	if model.Role(row.Role).IsAdmin() {
		if err := s.ensureNotLastAdmin(ctx, q, actor.UserID); err != nil {
			return err
		}
	}

	now := store.Now()
	if err := q.CloseUserAccount(ctx, actor.UserID, now); err != nil {
		return fmt.Errorf("close account: %w", err)
	}
	// Portico's own sessions die with the token version above; a relying
	// party's refresh token is a separate credential in a separate table and
	// would otherwise stay valid for its full month.
	if err := q.RevokeAllRefreshTokensForUser(ctx, actor.UserID, now); err != nil {
		return fmt.Errorf("revoke federated sessions: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionAccountClose,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: actor.UserID, TargetName: actor.Username,
		IP: ip,
	})

	// Downstream systems are told the same thing they are told about any
	// deactivation: this account no longer signs in. Whether it left or was
	// suspended is Portico's business, not theirs.
	if user, err := s.Get(ctx, actor.TenantID, actor.UserID); err == nil {
		s.publish(ctx, actor.TenantID, webhook.EventUserDisabled, user)
	}
	return nil
}
