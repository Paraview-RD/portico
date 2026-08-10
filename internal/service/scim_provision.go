package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// Provisioning: the operations a directory performs, as distinct from the
// ones an administrator performs.
//
// They are separate methods rather than the existing ones with a flag,
// because the rules genuinely differ. A provisioning system supplies no
// password, does not choose a role, cannot reach an organization it has no
// concept of, and identifies accounts by an externalId that the console
// knows nothing about. Threading all of that through Create and Update as
// optional behaviour would leave the administrator path carrying conditions
// that only a sync can trigger.
//
// What they share is everything that matters: the same uniqueness
// constraints, the same status semantics, the same audit trail, the same
// last-administrator protection. A directory cannot do anything to an
// account that an administrator could not.

// ErrExternalIDTaken is returned when a provisioning identifier is already
// bound to a different account.
var ErrExternalIDTaken = httpx.Conflict("EXTERNAL_ID_TAKEN",
	"That externalId is already bound to another account.")

// ErrProvisioningLastAdmin is returned when a sync would deactivate the last
// administrator.
//
// The same rule the console enforces, and it applies here for a better
// reason: a directory that stops listing somebody is a routine event, and
// without this a leaver's last day would lock everyone out of the tenant
// with no way back in short of the database.
var ErrProvisioningLastAdmin = httpx.UnprocessableEntity("LAST_ADMIN",
	"That account is the only active administrator and cannot be deactivated.")

// ProvisionUserInput is what a directory supplies.
//
// No password: a provisioning system pushing one would mean the directory
// holds a value it can replay, and this deployment's own policy would apply
// to something nobody here chose. No role: SCIM has no notion of Portico's
// two roles, and inventing a mapping would let a directory grant
// administrator by writing an attribute. No organization, for the same
// reason group provisioning is absent.
type ProvisionUserInput struct {
	Username    string
	DisplayName string
	Email       string
	Phone       string
	ExternalID  string
	Active      bool
	// Profile is the descriptive half, which a directory sends alongside
	// everything else. Written through the same statement the console uses,
	// so there is one place these attributes are validated.
	Profile model.UserProfile
}

// ProvisionUser creates an account on behalf of a directory.
//
// Reconciliation is part of the contract rather than an optimisation: a
// provisioning client that loses track of an account POSTs it again, and a
// server that answered "created" with a second row would duplicate the
// directory. When the externalId already exists the existing account is
// updated instead, which is what the client's own next GET would show
// anyway.
func (s *UserService) ProvisionUser(ctx context.Context, tenantID string, in ProvisionUserInput) (model.User, error) {
	in.normalize()
	if in.Username == "" {
		return model.User{}, httpx.BadRequest("USERNAME_REQUIRED",
			"userName is required.")
	}

	q := s.store.ForTenant(tenantID)

	if in.ExternalID != "" {
		existing, err := q.GetUserByExternalID(ctx, in.ExternalID)
		switch {
		case err == nil:
			return s.UpdateProvisionedUser(ctx, tenantID, existing.ID, in)
		case store.IsNoRows(err):
			// The ordinary case: a new account.
		default:
			return model.User{}, fmt.Errorf("look up user by external id: %w", err)
		}
	}

	// A password is required by the schema and never used: this account signs
	// in through the directory, or not at all. It is random and immediately
	// discarded rather than empty or fixed, so that nothing can authenticate
	// as it if a future change starts accepting passwords for provisioned
	// accounts. Same generation as the bootstrap administrator's.
	hash, err := auth.HashPassword(uuid.NewString())
	if err != nil {
		return model.User{}, err
	}

	now := store.Now()
	id := uuid.NewString()
	status := model.StatusDisabled
	if in.Active {
		status = model.StatusActive
	}

	err = q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:           id,
		Username:     in.Username,
		DisplayName:  in.displayNameOrUsername(),
		PasswordHash: hash,
		Phone:        in.Phone,
		Email:        in.Email,
		Role:         string(model.RoleUser),
		Status:       string(status),
		Source:       string(model.SourceSCIM),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("write user: %w", err)
	}

	if in.ExternalID != "" {
		err = q.SetUserExternalID(ctx, sqlcgen.SetUserExternalIDParams{
			ID: id, ExternalID: &in.ExternalID, UpdatedAt: now,
		})
		if err != nil {
			if taken := takenFieldError(err); taken != nil {
				return model.User{}, taken
			}
			return model.User{}, fmt.Errorf("write user: %w", err)
		}
	}

	// The descriptive attributes, through the same statement the console
	// writes. One place these are validated, and one place they can be
	// wrong. The manager is never set from here — see scim.Handler.
	if err := s.writeProvisionedProfile(ctx, q, id, in.Profile, now); err != nil {
		return model.User{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSCIMUserCreate,
		ActorName:  ProvisioningActor,
		TargetType: targetUser, TargetID: id, TargetName: in.Username,
		Detail: "externalId=" + in.ExternalID,
	})

	created, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.User{}, err
	}
	s.publish(ctx, tenantID, webhook.EventUserCreated, created)
	return created, nil
}

// UpdateProvisionedUser applies a directory's view of an account.
func (s *UserService) UpdateProvisionedUser(ctx context.Context, tenantID, userID string, in ProvisionUserInput) (model.User, error) {
	in.normalize()
	if in.Username == "" {
		return model.User{}, httpx.BadRequest("USERNAME_REQUIRED",
			"userName is required.")
	}

	q := s.store.ForTenant(tenantID)

	current, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	status := model.StatusDisabled
	if in.Active {
		status = model.StatusActive
	}
	if err := s.guardLastAdmin(ctx, q, current, status); err != nil {
		return model.User{}, err
	}

	now := store.Now()
	// One statement, and it includes the username: the directory is the
	// system of record for a provisioned account, so a rename there has to
	// land here. Writing it through UpdateUserProfile — which does not take a
	// username — silently dropped the rename, and every subsequent sync tried
	// the same change again while the two sides stayed permanently different.
	var externalID *string
	if in.ExternalID != "" {
		externalID = &in.ExternalID
	} else if current.ExternalID != nil {
		// Absent from the request does not mean "unbind". A client that omits
		// externalId on an update would otherwise detach the account from the
		// directory, and the next sync would create a second one.
		externalID = current.ExternalID
	}

	err = q.UpdateProvisionedUser(ctx, sqlcgen.UpdateProvisionedUserParams{
		ID:          userID,
		Username:    in.Username,
		DisplayName: in.displayNameOrUsername(),
		Phone:       in.Phone,
		Email:       in.Email,
		Status:      string(status),
		ExternalID:  externalID,
		UpdatedAt:   now,
	})
	if err != nil {
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("write user: %w", err)
	}

	// The descriptive attributes, through the same statement the console
	// writes. One place these are validated, and one place they can be
	// wrong. The manager is never set from here — see scim.Handler.
	if err := s.writeProvisionedProfile(ctx, q, userID, in.Profile, now); err != nil {
		return model.User{}, err
	}

	// Deactivation ends every session the account holds, immediately, the
	// same way an administrator disabling it does. A deprovisioned account
	// whose token keeps working until it expires is the whole thing this
	// integration exists to prevent.
	if status != model.StatusActive && model.Status(current.Status) == model.StatusActive {
		if err := s.revokeEverything(ctx, q, userID, now); err != nil {
			return model.User{}, err
		}
	}

	action := model.ActionSCIMUserUpdate
	if status != model.Status(current.Status) {
		action = model.ActionSCIMUserEnable
		if status != model.StatusActive {
			action = model.ActionSCIMUserDisable
		}
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorName:  ProvisioningActor,
		TargetType: targetUser, TargetID: userID, TargetName: in.Username,
	})

	updated, err := s.Get(ctx, tenantID, userID)
	if err != nil {
		return model.User{}, err
	}
	// A directory-driven change is still a change, and a subscriber wanting
	// to know when somebody is deprovisioned wants this one most of all.
	event := webhook.EventUserUpdated
	if status != model.Status(current.Status) {
		event = webhook.EventUserEnabled
		if status != model.StatusActive {
			event = webhook.EventUserDisabled
		}
	}
	s.publish(ctx, tenantID, event, updated)
	return updated, nil
}

// SetProvisionedUserActive is deprovisioning on its own, for DELETE.
//
// It goes through UpdateProvisionedUser rather than writing status directly,
// so that DELETE and PATCH active=false cannot drift apart — one of them
// revoking sessions and the other not is a difference nobody would notice
// until somebody left.
func (s *UserService) SetProvisionedUserActive(ctx context.Context, tenantID, userID string, active bool) (model.User, error) {
	current, err := s.Get(ctx, tenantID, userID)
	if err != nil {
		return model.User{}, err
	}

	return s.UpdateProvisionedUser(ctx, tenantID, userID, ProvisionUserInput{
		Username:    current.Username,
		DisplayName: current.DisplayName,
		Email:       current.Email,
		Phone:       current.Phone,
		ExternalID:  current.ExternalID,
		Active:      active,
		// Deactivating says nothing about who somebody is. Omitting this
		// cleared every descriptive attribute on the way out, which is the
		// opposite of "the account stays readable afterwards" — the reason
		// this deactivates instead of deleting.
		Profile: current.Profile,
	})
}

// FindByExternalID resolves the identifier a directory knows an account by.
func (s *UserService) FindByExternalID(ctx context.Context, tenantID, externalID string) (model.User, error) {
	row, err := s.store.ForTenant(tenantID).GetUserByExternalID(ctx, externalID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("look up user by external id: %w", err)
	}
	return s.Get(ctx, tenantID, row.ID)
}

// ProvisioningActor is the actor name in the audit trail for a change no
// person made. It is not an account, and deliberately not one: an entry
// attributed to a user id that exists would be a lie about who acted.
//
// Exported because the console filters the audit log by it to show what a
// directory has done, and a test asserts the two literals agree.
const ProvisioningActor = "scim"

const targetUser = "USER"

func (in *ProvisionUserInput) normalize() {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.ExternalID = strings.TrimSpace(in.ExternalID)
}

// displayNameOrUsername keeps the column non-empty.
//
// A directory that sends no display name is not unusual, and the schema
// requires one — falling back to the username shows something recognisable
// rather than a blank row in every listing.
func (in ProvisionUserInput) displayNameOrUsername() string {
	if in.DisplayName != "" {
		return in.DisplayName
	}
	return in.Username
}

// guardLastAdmin refuses a deactivation that would leave the tenant with no
// administrator.
func (s *UserService) guardLastAdmin(ctx context.Context, q *store.Scoped, current sqlcgen.User, next model.Status) error {
	deactivating := next != model.StatusActive &&
		model.Status(current.Status) == model.StatusActive
	if !deactivating || model.Role(current.Role) != model.RoleSuperAdmin {
		return nil
	}

	remaining, err := q.CountOtherActiveAdmins(ctx,
		string(model.RoleSuperAdmin), string(model.StatusActive), current.ID)
	if err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if remaining == 0 {
		return ErrProvisioningLastAdmin
	}
	return nil
}

// revokeEverything ends the account's sessions and federated tokens.
func (s *UserService) revokeEverything(ctx context.Context, q *store.Scoped, userID string, now time.Time) error {
	if err := q.RevokeSessionsForUser(ctx, userID, now); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if err := q.BumpUserTokenVersion(ctx, sqlcgen.BumpUserTokenVersionParams{
		ID: userID, UpdatedAt: now,
	}); err != nil {
		return fmt.Errorf("bump token version: %w", err)
	}
	if err := q.RevokeAllRefreshTokensForUser(ctx, userID, now); err != nil {
		return fmt.Errorf("revoke federated sessions: %w", err)
	}
	return nil
}

// writeProvisionedProfile stores the descriptive attributes a directory sent.
//
// The manager is deliberately left alone rather than cleared: a directory
// does not send one — its id space is its own — so treating "absent" as
// "remove" would wipe a relationship an operator set in the console on every
// sync.
func (s *UserService) writeProvisionedProfile(ctx context.Context, q *store.Scoped, userID string, in model.UserProfile, now time.Time) error {
	current, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	err = q.UpdateUserProfileAttributes(ctx, sqlcgen.UpdateUserProfileAttributesParams{
		ID:                userID,
		NameFormatted:     in.NameFormatted,
		FamilyName:        in.FamilyName,
		GivenName:         in.GivenName,
		MiddleName:        in.MiddleName,
		HonorificPrefix:   in.HonorificPrefix,
		HonorificSuffix:   in.HonorificSuffix,
		NickName:          in.NickName,
		ProfileUrl:        in.ProfileURL,
		PhotoUrl:          in.PhotoURL,
		Title:             in.Title,
		UserType:          in.UserType,
		PreferredLanguage: in.PreferredLanguage,
		Locale:            in.Locale,
		Timezone:          in.Timezone,
		AddressFormatted:  in.AddressFormatted,
		StreetAddress:     in.StreetAddress,
		Locality:          in.Locality,
		Region:            in.Region,
		PostalCode:        in.PostalCode,
		Country:           in.Country,
		EmployeeNumber:    in.EmployeeNumber,
		CostCenter:        in.CostCenter,
		Department:        in.Department,
		ManagerID:         current.ManagerID,
		UpdatedAt:         now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return httpx.Conflict("EMPLOYEE_NUMBER_TAKEN",
				"Another account already has that employee number.")
		}
		return fmt.Errorf("write profile: %w", err)
	}
	return nil
}
