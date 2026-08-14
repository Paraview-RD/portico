package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// Errors surfaced to the API layer. They are httpx errors so the status and
// code are decided once, next to the rule that produced them.
var (
	ErrUserNotFound  = httpx.NotFound("USER_NOT_FOUND", "No such user.")
	ErrUsernameTaken = httpx.Conflict("USERNAME_TAKEN",
		"That username is already in use.")
	ErrEmailTaken = httpx.Conflict("EMAIL_TAKEN",
		"That email address is already in use.")
	ErrPhoneTaken = httpx.Conflict("PHONE_TAKEN",
		"That phone number is already in use.")
	ErrOrganizationNotFound = httpx.NotFound("ORGANIZATION_NOT_FOUND",
		"No such organization.")
	ErrOrganizationDisabled = httpx.UnprocessableEntity("ORGANIZATION_DISABLED",
		"That organization is disabled and cannot take new members.")
	ErrCannotDisableSelf = httpx.UnprocessableEntity("CANNOT_DISABLE_SELF",
		"You cannot disable your own account.")
	ErrLastAdmin = httpx.UnprocessableEntity("LAST_ADMIN",
		"This is the only active administrator; promote another account first.")
	ErrInvalidCredentials = httpx.Unauthorized("INVALID_CREDENTIALS",
		"Incorrect username or password.")
	// ErrAccountLocked is returned to somebody whose password was right but
	// whose account is temporarily locked after repeated failures. It is
	// deliberately distinguishable from a wrong password: at this point the
	// caller has proved they know the password, so the only thing left to
	// tell them is why it did not work.
	ErrAccountLocked = httpx.Unauthorized("ACCOUNT_LOCKED",
		"Too many failed sign-in attempts. Try again later, or ask an administrator to unlock the account.")

	// ErrPasswordExpired is returned when the password is right but too old
	// to use. Like the locked and disabled answers, it is only reached after
	// the password has matched.
	ErrPasswordExpired = httpx.Unauthorized("PASSWORD_EXPIRED",
		"This password has expired and must be changed before signing in.")

	// ErrPasswordChangeRequired is returned when the password is right but is
	// one the account may not keep — the documented default a release
	// bootstraps its first administrator with.
	//
	// Separate from ErrPasswordExpired although both lead to the same form,
	// because the two say different things to the person reading them. "This
	// password has expired" in front of somebody who has just installed the
	// software and typed the password the manual gave them describes nothing
	// that happened, and the first thing they would do is go looking for the
	// expiry setting they must have got wrong.
	ErrPasswordChangeRequired = httpx.Unauthorized("PASSWORD_CHANGE_REQUIRED",
		"This account is still on its default password, which must be replaced before signing in.")

	ErrAccountDisabled = httpx.Unauthorized("ACCOUNT_DISABLED",
		"This account has been disabled.")
	ErrRegistrationDisabled = httpx.UnprocessableEntity("REGISTRATION_DISABLED",
		"Self-service registration is currently closed.")
)

// UserService owns account lifecycle and credentials.
//
// Every method that touches accounts is scoped to a tenant. Methods acting
// on behalf of a signed-in caller take the tenant from their principal;
// those that run before there is one — sign-in, registration, bootstrap —
// take it explicitly from a tenant that has already been resolved and found
// active.
type UserService struct {
	store    *store.Store
	audit    *AuditService
	settings *SettingsService
	tokens   *auth.TokenService
	// metrics may be nil, and every recording method tolerates that. A test
	// that only cares about behaviour should not have to build a registry to
	// get one.
	metrics *metrics.Registry
	// events may be nil too, for the same reason. Publishing is best-effort
	// by design — see WebhookService.Publish — so a nil one is the same as a
	// tenant with no subscriptions.
	events EventPublisher
}

// EventPublisher is the slice of the webhook service the account operations
// need.
//
// An interface so that user.go states its dependency as the one method it
// calls, and so the two do not form a cycle when the webhook service comes
// to describe a user.
type EventPublisher interface {
	Publish(ctx context.Context, tenantID, eventType string, data any)
}

// WithEvents attaches a publisher. Separate from the constructor because the
// webhook service is built after this one and only needs to be known by the
// operations that emit.
func (s *UserService) WithEvents(publisher EventPublisher) *UserService {
	s.events = publisher
	return s
}

// publish emits an event if there is anywhere to send it.
func (s *UserService) publish(ctx context.Context, tenantID, eventType string, user model.User) {
	if s.events == nil {
		return
	}
	s.events.Publish(ctx, tenantID, eventType, user)
}

// NewUserService wires a UserService.
func NewUserService(st *store.Store, audit *AuditService, settings *SettingsService, tokens *auth.TokenService, m *metrics.Registry) *UserService {
	return &UserService{
		store: st, audit: audit, settings: settings, tokens: tokens, metrics: m,
	}
}

// LookupForAuth implements auth.UserLookup. It runs on every authenticated
// request, so it stays a single indexed read.
//
// This is the one account read that is not tenant-scoped, because it is what
// establishes the tenant: all it has to go on is the subject of a token. The
// middleware compares the tenant it returns against the token's claim, so
// the absence of a filter here does not widen what a token can reach. See
// internal/store/queries/authentication.sql.
func (s *UserService) LookupForAuth(ctx context.Context, userID string) (auth.Account, error) {
	row, err := s.store.Queries.GetUserForAuthentication(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return auth.Account{}, auth.ErrUserNotFound
		}
		return auth.Account{}, fmt.Errorf("look up user: %w", err)
	}

	out := auth.Account{
		ID:           row.ID,
		TenantID:     row.TenantID,
		Username:     row.Username,
		DisplayName:  row.DisplayName,
		Role:         model.Role(row.Role),
		Status:       model.Status(row.Status),
		Closed:       row.ClosedAt != nil,
		TokenVersion: row.TokenVersion,
	}
	if row.OrganizationID != nil {
		out.OrganizationID = *row.OrganizationID
		// The organization name is resolved separately so the common path
		// stays one query; a missing organization is not an auth failure.
		org, err := s.store.ForTenant(row.TenantID).GetOrganizationByID(ctx, *row.OrganizationID)
		if err == nil {
			out.OrganizationName = org.Name
		}
	}
	return out, nil
}

// Get returns one user with their organization name resolved.
func (s *UserService) Get(ctx context.Context, tenantID, userID string) (model.User, error) {
	q := s.store.ForTenant(tenantID)

	row, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	users, err := s.attachOrganizations(ctx, q, []sqlcgen.User{row})
	if err != nil {
		return model.User{}, err
	}
	return users[0], nil
}

// UnassignedOrganization is what OrganizationID is set to in order to ask
// for the people who are in no organization at all.
//
// An empty string cannot express it, because an empty string already means
// "every organization" — so before this existed there was no way to ask the
// question, and the accounts nobody has filed anywhere are exactly the ones
// somebody goes looking for. Safe as a reserved value because organization
// ids are UUIDs (see OrganizationService.Create) and can never be this.
const UnassignedOrganization = "none"

// UserQuery filters a user listing.
type UserQuery struct {
	// Keyword matches the username or display name (§3.1).
	Keyword string
	// Status and Role are exact filters; empty means all.
	Status model.Status
	Role   model.Role
	// OrganizationID selects an organization and everything under it, not
	// that one organization alone. Empty means all of them, and
	// UnassignedOrganization means the people in none.
	//
	// The subtree is the whole point: this filter is reached from a tree, and
	// picking a division and being shown only the handful of people filed
	// directly against the division itself — rather than everybody in it —
	// reads as a defect rather than as a distinction. The narrower question
	// has never been asked; the broader one is asked constantly.
	OrganizationID string
}

// List returns a page of users, newest first.
//
// Hand-written because the filters are optional and sqlc cannot express a
// query whose WHERE clause varies. The tenant predicate is written into the
// SQL rather than added by the filter builder, so it is visible in the query
// and checked by the guard test in internal/store.
func (s *UserService) List(ctx context.Context, tenantID string, q UserQuery, page Page) ([]model.User, int64, error) {
	f := tenantFilters(tenantID)

	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		pattern := "%" + escapeLike(keyword) + "%"
		f.Add(`(username LIKE %s ESCAPE '\' OR display_name LIKE %s ESCAPE '\')`, pattern, pattern)
	}
	if q.Status != "" {
		f.Add("status = %s", string(q.Status))
	}
	if q.Role != "" {
		f.Add("role = %s", string(q.Role))
	}
	switch {
	case q.OrganizationID == UnassignedOrganization:
		f.Add("organization_id IS NULL")
	case q.OrganizationID != "":
		// The subtree, resolved in the same statement rather than in a
		// round trip of its own, so the count and the page can never be
		// answered against two different shapes of the chart — somebody
		// reparenting a department between the two would otherwise produce
		// a total that disagrees with the rows under it.
		//
		// Both tenant predicates in here are redundancy, and it is worth
		// saying so rather than leaving somebody to assume they are what
		// keeps this walk inside the tenant. What actually does is the outer
		// query: users is filtered on tenant_id, and users.organization_id
		// carries a composite foreign key into (tenant_id, id), so an
		// organization belonging to another tenant cannot match a row this
		// query can see even if the walk below hands it one. The recursive
		// term is covered twice over, because organizations has the same
		// composite key on (tenant_id, parent_id) — a child in another
		// tenant is a row the database refuses rather than one to filter
		// out. Removing either predicate leaves every test here passing;
		// that was measured, not assumed.
		//
		// They stay because they cost nothing, and because somebody asking
		// "can this leave the tenant" should be able to answer from the
		// query instead of from two migrations and a join. The tenant is
		// bound explicitly rather than written as $1 on the knowledge that
		// tenantFilters puts it there, for the same reason: that knowledge
		// is true today and is a poor thing to depend on silently.
		//
		// Depth is bounded by resolveParent refusing cycles, so the
		// recursion terminates on any chart this server will accept.
		f.Add(`organization_id IN (
			WITH RECURSIVE subtree(id) AS (
				SELECT id FROM organizations WHERE tenant_id = %s AND id = %s
				UNION ALL
				SELECT child.id FROM organizations child
				  JOIN subtree ON child.parent_id = subtree.id
				 WHERE child.tenant_id = %s
			)
			SELECT id FROM subtree
		)`, tenantID, q.OrganizationID, tenantID)
	}

	clause := f.And()

	var total int64
	if err := s.store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE tenant_id = $1"+clause, f.Args()...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	pageClause, args := f.Paginate(page)
	rows, err := s.store.DB().QueryContext(ctx,
		// The column list is explicit and has to stay in step with the scan
		// below and with sqlcgen.User. A generated query would keep itself
		// honest; this one cannot be generated because its WHERE clause
		// depends on which filters the caller supplied. Adding the lockout
		// columns and forgetting this listing is exactly what happened once,
		// and the symptom was an administrator seeing no lock on a locked
		// account — hence TestHandWrittenUserSelectNamesEveryColumn.
		`SELECT id, tenant_id, username, display_name, password_hash, phone, email, role, status,
		        organization_id, token_version, source,
		        failed_login_attempts, last_failed_login_at, locked_until,
		        password_changed_at, must_change_password,
		        external_id, ldap_source_id, closed_at, verified_at,
		        name_formatted, family_name, given_name, middle_name,
		        honorific_prefix, honorific_suffix,
		        nick_name, profile_url, photo_url,
		        title, user_type, preferred_language, locale, timezone,
		        address_formatted, street_address, locality, region, postal_code, country,
		        employee_number, cost_center, department, manager_id,
		        created_at, updated_at
		 FROM users WHERE tenant_id = $1`+clause+`
		 ORDER BY created_at DESC, id DESC`+pageClause, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var found []sqlcgen.User
	for rows.Next() {
		var u sqlcgen.User
		if err := rows.Scan(
			&u.ID, &u.TenantID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Phone, &u.Email,
			&u.Role, &u.Status, &u.OrganizationID, &u.TokenVersion, &u.Source,
			&u.FailedLoginAttempts, &u.LastFailedLoginAt, &u.LockedUntil,
			&u.PasswordChangedAt, &u.MustChangePassword,
			&u.ExternalID, &u.LdapSourceID, &u.ClosedAt, &u.VerifiedAt,
			&u.NameFormatted, &u.FamilyName, &u.GivenName, &u.MiddleName,
			&u.HonorificPrefix, &u.HonorificSuffix,
			&u.NickName, &u.ProfileUrl, &u.PhotoUrl,
			&u.Title, &u.UserType, &u.PreferredLanguage, &u.Locale, &u.Timezone,
			&u.AddressFormatted, &u.StreetAddress, &u.Locality, &u.Region, &u.PostalCode, &u.Country,
			&u.EmployeeNumber, &u.CostCenter, &u.Department, &u.ManagerID,
			&u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		found = append(found, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}

	users, err := s.attachOrganizations(ctx, s.store.ForTenant(tenantID), found)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// CreateUserInput is an administrator-initiated account creation.
type CreateUserInput struct {
	Username       string
	DisplayName    string
	Password       string
	Phone          string
	Email          string
	Role           model.Role
	OrganizationID string
	Source         model.UserSource
	// MustChangePassword refuses this account at sign-in until the password
	// is replaced. Set for a bootstrap administrator that took the documented
	// default; nothing in the API offers it yet.
	MustChangePassword bool
}

// Create adds an account to a tenant. The caller is responsible for having
// checked that the actor is an administrator of that tenant.
func (s *UserService) Create(ctx context.Context, tenantID string, in CreateUserInput) (model.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)

	if err := validateUsername(in.Username); err != nil {
		return model.User{}, err
	}
	if in.DisplayName == "" {
		return model.User{}, httpx.BadRequest("DISPLAY_NAME_REQUIRED", "A display name is required.")
	}
	// Creating an account is the fourth way a password enters the system —
	// alongside self-service change, administrator reset, and recovery — and
	// the policy has to apply here too, or every account starts life with a
	// password the policy would have refused.
	policy, err := s.PasswordPolicyFor(ctx, tenantID)
	if err != nil {
		return model.User{}, err
	}
	if err := policy.Check(in.Password); err != nil {
		return model.User{}, err
	}
	if !in.Role.Valid() {
		return model.User{}, httpx.BadRequest("INVALID_ROLE", "Role must be SUPER_ADMIN or USER.")
	}
	if in.Source == "" {
		in.Source = model.SourceAdmin
	}

	if err := validateContactDetails(in.Phone, in.Email); err != nil {
		return model.User{}, err
	}

	q := s.store.ForTenant(tenantID)

	orgID, err := s.resolveAssignableOrganization(ctx, q, in.OrganizationID)
	if err != nil {
		return model.User{}, err
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}

	now := store.Now()
	id := uuid.NewString()
	err = q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:             id,
		Username:       in.Username,
		DisplayName:    in.DisplayName,
		PasswordHash:   hash,
		Phone:          strings.TrimSpace(in.Phone),
		Email:          strings.TrimSpace(in.Email),
		Role:           string(in.Role),
		Status:         string(model.StatusActive),
		OrganizationID: orgID,
		TokenVersion:   1,
		Source:         string(in.Source),
		// A password set at creation counts as set now, so a fresh account
		// does not arrive already expired.
		PasswordChangedAt:  &now,
		MustChangePassword: in.MustChangePassword,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err == nil {
		// The password an account is created with goes into its history too.
		// Without this the very first password is the one value that can
		// always be set again — and it is the likeliest one to be tried,
		// because it is usually the temporary password an administrator
		// handed over, which the person then "changes" straight back to.
		if err := s.rememberPassword(ctx, q, id, hash, policy); err != nil {
			return model.User{}, err
		}
	}
	if err != nil {
		// The unique indexes are the only thing that actually guarantees
		// uniqueness — a check-then-insert would still lose a race — so the
		// conflict is recognized here rather than pre-empted.
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("create user: %w", err)
	}

	created, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.User{}, err
	}
	s.publish(ctx, tenantID, webhook.EventUserCreated, created)
	return created, nil
}

// UpdateUserInput changes an account's profile. Password and status have
// their own operations because they carry different authorization rules.
type UpdateUserInput struct {
	DisplayName    string
	Phone          string
	Email          string
	Role           model.Role
	OrganizationID string
}

// Update changes a user's profile, role, and organization.
func (s *UserService) Update(ctx context.Context, actor auth.Principal, userID string, in UpdateUserInput) (model.User, error) {
	q := s.store.ForTenant(actor.TenantID)

	current, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return model.User{}, httpx.BadRequest("DISPLAY_NAME_REQUIRED", "A display name is required.")
	}
	if !in.Role.Valid() {
		return model.User{}, httpx.BadRequest("INVALID_ROLE", "Role must be SUPER_ADMIN or USER.")
	}
	if err := validateContactDetails(in.Phone, in.Email); err != nil {
		return model.User{}, err
	}

	// Demoting the last administrator would leave nobody able to administer
	// this tenant.
	if model.Role(current.Role).IsAdmin() && !in.Role.IsAdmin() {
		if err := s.ensureNotLastAdmin(ctx, q, userID); err != nil {
			return model.User{}, err
		}
	}

	// §3.4.1: disabling an organization leaves its existing members alone —
	// only a new binding to it is refused. Resubmitting the same,
	// unchanged organization is not a new binding, so it skips the status
	// check that would otherwise make saving any other field impossible
	// once somebody's organization is disabled after the fact.
	var orgID *string
	if in.OrganizationID == organizationRef(current.OrganizationID) {
		orgID = current.OrganizationID
	} else {
		orgID, err = s.resolveAssignableOrganization(ctx, q, in.OrganizationID)
		if err != nil {
			return model.User{}, err
		}
	}

	now := store.Now()
	err = q.UpdateUserProfile(ctx, sqlcgen.UpdateUserProfileParams{
		ID:             userID,
		DisplayName:    in.DisplayName,
		Phone:          strings.TrimSpace(in.Phone),
		Email:          strings.TrimSpace(in.Email),
		OrganizationID: orgID,
		Role:           string(in.Role),
		UpdatedAt:      now,
	})
	if err != nil {
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("update user: %w", err)
	}

	updated, err := s.Get(ctx, actor.TenantID, userID)
	if err != nil {
		return model.User{}, err
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: userID, TargetName: updated.Username,
	})
	s.publish(ctx, actor.TenantID, webhook.EventUserUpdated, updated)

	// An organization change is also an organization-log event, since §3.9
	// calls for membership moves to be traceable there.
	if organizationRef(current.OrganizationID) != updated.OrganizationID {
		s.audit.Log(ctx, actor.TenantID, AuditEntry{
			Kind: model.LogOrganization, Action: model.ActionOrgAssign,
			ActorID: actor.UserID, ActorName: actor.Username,
			TargetType: "USER", TargetID: userID, TargetName: updated.Username,
			Detail: fmt.Sprintf("organization: %q -> %q",
				organizationRef(current.OrganizationID), updated.OrganizationID),
		})
	}

	return updated, nil
}

// Unlock clears a lockout without changing the password.
//
// Separate from resetting the password because the two answer different
// situations. Somebody who mistyped five times and cannot wait fifteen
// minutes needs the lock gone and their password left alone; forcing a reset
// on them would be an administrator handing out a password over the phone,
// which is worse than the lockout.
func (s *UserService) Unlock(ctx context.Context, actor auth.Principal, userID string) (model.User, error) {
	target, err := s.Get(ctx, actor.TenantID, userID)
	if err != nil {
		return model.User{}, err
	}

	if err := s.store.ForTenant(actor.TenantID).ClearLoginFailures(ctx, userID, store.Now()); err != nil {
		return model.User{}, fmt.Errorf("unlock account: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserUnlock,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: target.ID, TargetName: target.Username,
	})

	unlocked, err := s.Get(ctx, actor.TenantID, userID)
	if err != nil {
		return model.User{}, err
	}
	s.publish(ctx, actor.TenantID, webhook.EventUserUnlocked, unlocked)
	return unlocked, nil
}

// SetStatus enables or disables an account. Disabling also revokes any live
// session, which the query handles by bumping token_version.
func (s *UserService) SetStatus(ctx context.Context, actor auth.Principal, userID string, status model.Status) (model.User, error) {
	if !status.Valid() {
		return model.User{}, httpx.BadRequest("INVALID_STATUS", "Status must be ACTIVE or DISABLED.")
	}

	q := s.store.ForTenant(actor.TenantID)

	target, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	if status == model.StatusDisabled {
		// Locking yourself out is never what you meant.
		if userID == actor.UserID {
			return model.User{}, ErrCannotDisableSelf
		}
		if model.Role(target.Role).IsAdmin() {
			if err := s.ensureNotLastAdmin(ctx, q, userID); err != nil {
				return model.User{}, err
			}
		}
	}

	err = q.UpdateUserStatus(ctx, sqlcgen.UpdateUserStatusParams{
		ID:        userID,
		Status:    string(status),
		UpdatedAt: store.Now(),
	})
	if err != nil {
		return model.User{}, fmt.Errorf("update user status: %w", err)
	}

	// Enabling a closed account is reinstating somebody, and the mark comes
	// off with it. Leaving it would produce a row that reads as closed and
	// signs in perfectly well — a state nobody can interpret.
	if status == model.StatusActive && target.ClosedAt != nil {
		if err := q.ReopenUserAccount(ctx, userID, store.Now()); err != nil {
			return model.User{}, fmt.Errorf("reopen account: %w", err)
		}
	}

	if status == model.StatusDisabled {
		// Disabling has to reach the relying parties too. A refresh token
		// checks the account's status when it is presented, but the token
		// itself would otherwise sit there valid for a month, and an
		// administrator disabling somebody means it now, everywhere.
		if err := q.RevokeAllRefreshTokensForUser(ctx, userID, store.Now()); err != nil {
			return model.User{}, fmt.Errorf("revoke federated sessions: %w", err)
		}
	}

	action := model.ActionUserEnable
	if status == model.StatusDisabled {
		action = model.ActionUserDisable
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: userID, TargetName: target.Username,
	})

	result, err := s.Get(ctx, actor.TenantID, userID)
	if err != nil {
		return model.User{}, err
	}

	// Emitted after the change is committed and the sessions are revoked, so
	// a receiver acting on "disabled" the instant it arrives cannot observe
	// an account that is disabled here but still has a live session.
	event := webhook.EventUserEnabled
	if status == model.StatusDisabled {
		event = webhook.EventUserDisabled
	}
	s.publish(ctx, actor.TenantID, event, result)

	return result, nil
}

// attachOrganizations resolves organization names for a batch of rows in one
// query, so a page of users does not cost a lookup per row.
func (s *UserService) attachOrganizations(ctx context.Context, q *store.Scoped, rows []sqlcgen.User) ([]model.User, error) {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.OrganizationID == nil {
			continue
		}
		if _, dup := seen[*row.OrganizationID]; dup {
			continue
		}
		seen[*row.OrganizationID] = struct{}{}
		ids = append(ids, *row.OrganizationID)
	}

	names := make(map[string]string, len(ids))
	if len(ids) > 0 {
		orgs, err := q.ListOrganizationsByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("resolve organization names: %w", err)
		}
		for _, org := range orgs {
			names[org.ID] = org.Name
		}
	}

	// The same treatment for whoever these people report to: one query for
	// the page rather than one per row, and a name rather than an id,
	// because a client should never have to show a bare identifier.
	managerIDs := make([]string, 0, len(rows))
	seenManager := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.ManagerID == nil {
			continue
		}
		if _, dup := seenManager[*row.ManagerID]; dup {
			continue
		}
		seenManager[*row.ManagerID] = struct{}{}
		managerIDs = append(managerIDs, *row.ManagerID)
	}

	managers := make(map[string]string, len(managerIDs))
	if len(managerIDs) > 0 {
		found, err := q.ListUsersByIDs(ctx, managerIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve manager names: %w", err)
		}
		for _, manager := range found {
			managers[manager.ID] = manager.DisplayName
		}
	}

	users := make([]model.User, 0, len(rows))
	for _, row := range rows {
		user := model.User{
			ID:          row.ID,
			TenantID:    row.TenantID,
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Phone:       row.Phone,
			Email:       row.Email,
			Role:        model.Role(row.Role),
			Status:      model.Status(row.Status),
			Source:      model.UserSource(row.Source),
			ClosedAt:    row.ClosedAt,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
			Profile:     profileFromRow(row),
		}
		if row.ManagerID != nil {
			// Absent from the map means a manager who has since been
			// deleted, which cannot happen — accounts are never deleted —
			// or one in another tenant, which the write path refuses. Left
			// empty rather than falling back to the id, so a client shows
			// nothing instead of showing a UUID as a person's name.
			user.Profile.ManagerName = managers[*row.ManagerID]
		}
		// Only while it is actually in force. A stale timestamp on a lock
		// that has already expired would show an administrator a problem
		// that is no longer there.
		if row.LockedUntil != nil && row.LockedUntil.After(store.Now()) {
			user.LockedUntil = row.LockedUntil
		}
		if row.OrganizationID != nil {
			user.OrganizationID = *row.OrganizationID
			user.OrganizationName = names[*row.OrganizationID]
		}
		if row.ExternalID != nil {
			user.ExternalID = *row.ExternalID
		}
		users = append(users, user)
	}
	return users, nil
}

// takenFieldError maps a unique-constraint failure on users to the field
// that actually collided, or returns nil if err is something else.
//
// Discriminating on the constraint name matters more than it looks: users is
// unique on three things within a tenant, and reporting all three as
// "username already in use" sends whoever is fixing a bulk-import row to the
// wrong column. The names are declared in the migration for this reason.
func takenFieldError(err error) error {
	if !store.IsUniqueViolation(err) {
		return nil
	}
	switch store.ViolatedConstraint(err) {
	case "uq_users_tenant_username":
		return ErrUsernameTaken
	case "uq_users_tenant_email":
		return ErrEmailTaken
	case "uq_users_tenant_phone":
		return ErrPhoneTaken
	default:
		// A unique violation on users that is none of the three means a
		// constraint was added without extending this. Reporting the
		// username is a guess; say plainly that something collided.
		return httpx.Conflict("ALREADY_EXISTS",
			"Those details conflict with an existing account.")
	}
}

// resolveAssignableOrganization validates an organization reference for
// assignment, rejecting one that is missing or disabled (§3.4.1). An
// organization in another tenant simply does not exist as far as this
// lookup is concerned, which is what makes a cross-tenant assignment
// impossible rather than merely discouraged.
func (s *UserService) resolveAssignableOrganization(ctx context.Context, q *store.Scoped, orgID string) (*string, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil
	}

	org, err := q.GetOrganizationByID(ctx, orgID)
	if err != nil {
		if store.IsNoRows(err) {
			return nil, ErrOrganizationNotFound
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	if model.Status(org.Status) != model.StatusActive {
		return nil, ErrOrganizationDisabled
	}
	return &orgID, nil
}

// ensureNotLastAdmin fails if userID is the only remaining active
// administrator of their tenant. Another tenant's administrators are not
// counted: they cannot administer this one.
func (s *UserService) ensureNotLastAdmin(ctx context.Context, q *store.Scoped, userID string) error {
	count, err := q.CountOtherActiveAdmins(ctx,
		string(model.RoleSuperAdmin), string(model.StatusActive), userID)
	if err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if count == 0 {
		return ErrLastAdmin
	}
	return nil
}

// validateContactDetails checks the two fields that double as sign-in
// identifiers and as password-recovery destinations. Either may be empty,
// which means "not bound".
//
// The bar is "could this plausibly be delivered to", not "is this the
// canonical form". An address that cannot receive a reset is worth rejecting
// at entry; deciding whose numbering plan a phone number belongs to is not
// something an identity server should be in the business of.
func validateContactDetails(phone, email string) error {
	phone = strings.TrimSpace(phone)
	email = strings.TrimSpace(email)

	if email != "" {
		parsed, err := mail.ParseAddress(email)
		// ParseAddress also accepts `Name <a@b.c>`; requiring the parse to
		// round-trip keeps the stored value to the bare address, which is
		// what the unique index and the recovery lookup compare.
		if err != nil || parsed.Address != email {
			return httpx.BadRequest("INVALID_EMAIL", "That is not a valid email address.")
		}
		if len(email) > 254 {
			// The limit from RFC 5321 on a reverse-path.
			return httpx.BadRequest("INVALID_EMAIL", "That email address is too long.")
		}
	}

	if phone != "" {
		if len(phone) < 5 || len(phone) > 20 {
			return httpx.BadRequest("INVALID_PHONE", "That is not a valid phone number.")
		}
		for i, r := range phone {
			switch {
			case r >= '0' && r <= '9':
			case r == '+' && i == 0:
			default:
				return httpx.BadRequest("INVALID_PHONE",
					"A phone number may contain only digits, optionally led by +.")
			}
		}
	}

	return nil
}

func validateUsername(username string) error {
	if username == "" {
		return httpx.BadRequest("USERNAME_REQUIRED", "A username is required.")
	}
	if len(username) < 3 || len(username) > 64 {
		return httpx.BadRequest("INVALID_USERNAME", "Username must be between 3 and 64 characters.")
	}
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '@':
		default:
			return httpx.BadRequest("INVALID_USERNAME",
				"Username may contain only letters, digits, and the characters . _ - @")
		}
	}
	return nil
}

func organizationRef(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// profileFromRow copies the descriptive half of an account out of its row.
//
// Written out rather than reflected over. Thirty assignments are dull and a
// reflection-based copy is one renamed column away from silently dropping a
// field — and a field that silently stops being returned is exactly the kind
// of defect nobody reports, because the screen just shows a blank.
func profileFromRow(row sqlcgen.User) model.UserProfile {
	profile := model.UserProfile{
		NameFormatted:   row.NameFormatted,
		FamilyName:      row.FamilyName,
		GivenName:       row.GivenName,
		MiddleName:      row.MiddleName,
		HonorificPrefix: row.HonorificPrefix,
		HonorificSuffix: row.HonorificSuffix,

		NickName:   row.NickName,
		ProfileURL: row.ProfileUrl,
		PhotoURL:   row.PhotoUrl,

		Title:             row.Title,
		UserType:          row.UserType,
		PreferredLanguage: row.PreferredLanguage,
		Locale:            row.Locale,
		Timezone:          row.Timezone,

		AddressFormatted: row.AddressFormatted,
		StreetAddress:    row.StreetAddress,
		Locality:         row.Locality,
		Region:           row.Region,
		PostalCode:       row.PostalCode,
		Country:          row.Country,

		EmployeeNumber: row.EmployeeNumber,
		CostCenter:     row.CostCenter,
		Department:     row.Department,
	}
	if row.ManagerID != nil {
		profile.ManagerID = *row.ManagerID
	}
	return profile
}

// ErrManagerNotFound is returned when the manager named on a profile is not
// an account in this tenant.
var ErrManagerNotFound = httpx.UnprocessableEntity("MANAGER_NOT_FOUND",
	"No such account to report to.")

// ErrManagerIsSelf is returned for somebody reporting to themselves.
//
// Longer chains are not checked. A cycle of two is always a mistake and
// costs one comparison to catch; a cycle of five is a data-quality problem
// in whatever system produced it, and finding one would mean a recursive
// query on every write of a field nothing in Portico reads for authorization.
var ErrManagerIsSelf = httpx.UnprocessableEntity("MANAGER_IS_SELF",
	"An account cannot report to itself.")

// SetProfile writes the descriptive attributes of an account.
//
// Separate from Update, which changes role, status, and organization. Those
// are decisions about somebody's access; these describe them. A single
// endpoint taking both would mean a form that edits a job title has to send
// a role, and sending the wrong one by omission is how a self-service screen
// becomes a privilege-escalation endpoint.
func (s *UserService) SetProfile(ctx context.Context, actor auth.Principal, userID string, in model.UserProfile) (model.User, error) {
	q := s.store.ForTenant(actor.TenantID)

	target, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	var manager *string
	if id := strings.TrimSpace(in.ManagerID); id != "" {
		if id == userID {
			return model.User{}, ErrManagerIsSelf
		}
		// Read through the tenant-scoped view, so naming an account in
		// another tenant is a miss rather than a cross-tenant reference.
		if _, err := q.GetUserByID(ctx, id); err != nil {
			if store.IsNoRows(err) {
				return model.User{}, ErrManagerNotFound
			}
			return model.User{}, fmt.Errorf("get manager: %w", err)
		}
		manager = &id
	}

	err = q.UpdateUserProfileAttributes(ctx, sqlcgen.UpdateUserProfileAttributesParams{
		ID:                userID,
		NameFormatted:     strings.TrimSpace(in.NameFormatted),
		FamilyName:        strings.TrimSpace(in.FamilyName),
		GivenName:         strings.TrimSpace(in.GivenName),
		MiddleName:        strings.TrimSpace(in.MiddleName),
		HonorificPrefix:   strings.TrimSpace(in.HonorificPrefix),
		HonorificSuffix:   strings.TrimSpace(in.HonorificSuffix),
		NickName:          strings.TrimSpace(in.NickName),
		ProfileUrl:        strings.TrimSpace(in.ProfileURL),
		PhotoUrl:          strings.TrimSpace(in.PhotoURL),
		Title:             strings.TrimSpace(in.Title),
		UserType:          strings.TrimSpace(in.UserType),
		PreferredLanguage: strings.TrimSpace(in.PreferredLanguage),
		Locale:            strings.TrimSpace(in.Locale),
		Timezone:          strings.TrimSpace(in.Timezone),
		AddressFormatted:  strings.TrimSpace(in.AddressFormatted),
		StreetAddress:     strings.TrimSpace(in.StreetAddress),
		Locality:          strings.TrimSpace(in.Locality),
		Region:            strings.TrimSpace(in.Region),
		PostalCode:        strings.TrimSpace(in.PostalCode),
		Country:           strings.TrimSpace(in.Country),
		EmployeeNumber:    strings.TrimSpace(in.EmployeeNumber),
		CostCenter:        strings.TrimSpace(in.CostCenter),
		Department:        strings.TrimSpace(in.Department),
		ManagerID:         manager,
		UpdatedAt:         store.Now(),
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			// The only unique attribute here. An employee number is how an
			// HR system names a person, so two accounts claiming one is a
			// reconciliation error rather than something to store.
			return model.User{}, httpx.Conflict("EMPLOYEE_NUMBER_TAKEN",
				"Another account already has that employee number.")
		}
		return model.User{}, fmt.Errorf("update profile: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: userID, TargetName: target.Username,
		Detail: "profile attributes",
	})

	return s.Get(ctx, actor.TenantID, userID)
}

// BulkOutcome is what one account in a bulk request did.
type BulkOutcome struct {
	UserID string `json:"userId"`
	// Code is empty on success, and the error code otherwise. Per account
	// rather than for the request as a whole: an operator selecting forty
	// people and finding one of them is the last administrator needs to know
	// which one, not that "it failed".
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// BulkResult summarizes a bulk request.
type BulkResult struct {
	Total     int           `json:"total"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Outcomes  []BulkOutcome `json:"outcomes"`
}

// MaxBulkUsers bounds one bulk request.
//
// Each account is its own statement with its own audit entry and its own
// webhook, so a request of ten thousand would hold a connection for minutes
// and produce a result nobody can read. The console pages at a hundred; this
// is five of those.
const MaxBulkUsers = 500

// ErrTooManyBulkUsers is returned for a request beyond that.
var ErrTooManyBulkUsers = httpx.BadRequest("TOO_MANY_USERS",
	fmt.Sprintf("At most %d accounts at a time.", MaxBulkUsers))

// BulkSetStatus enables or disables several accounts.
//
// Each one goes through SetStatus rather than a single UPDATE, and that is
// the point rather than an oversight: every rule that applies to disabling
// one account — the last administrator cannot be disabled, nobody can
// disable themselves, sessions and federated tokens end immediately, the
// trail records it — applies to each of these. A bulk path that wrote
// straight to the table would be a way around all of them, and the way
// around would be invisible.
//
// Failures are collected rather than fatal. An operator who selected forty
// people and hit one they may not disable wants the other thirty-nine done
// and a note about the one, not a refusal with nothing changed.
func (s *UserService) BulkSetStatus(ctx context.Context, actor auth.Principal, userIDs []string, status model.Status) (BulkResult, error) {
	if len(userIDs) > MaxBulkUsers {
		return BulkResult{}, ErrTooManyBulkUsers
	}

	result := BulkResult{Total: len(userIDs), Outcomes: make([]BulkOutcome, 0, len(userIDs))}
	for _, id := range userIDs {
		outcome := BulkOutcome{UserID: id}
		if _, err := s.SetStatus(ctx, actor, id, status); err != nil {
			outcome.Code, outcome.Message = describeBulkError(err)
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

// BulkSetOrganization moves several accounts into one organization, or out
// of any with an empty id.
//
// Through Update, for the same reason as above: it is the path that
// validates the organization exists, belongs to this tenant, and is not
// disabled.
func (s *UserService) BulkSetOrganization(ctx context.Context, actor auth.Principal, userIDs []string, organizationID string) (BulkResult, error) {
	if len(userIDs) > MaxBulkUsers {
		return BulkResult{}, ErrTooManyBulkUsers
	}

	result := BulkResult{Total: len(userIDs), Outcomes: make([]BulkOutcome, 0, len(userIDs))}
	for _, id := range userIDs {
		outcome := BulkOutcome{UserID: id}

		current, err := s.Get(ctx, actor.TenantID, id)
		if err != nil {
			outcome.Code, outcome.Message = describeBulkError(err)
			result.Failed++
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}

		// Everything else is carried through unchanged. Update replaces the
		// editable fields, so sending only the organization would blank a
		// display name and demote whoever was an administrator.
		_, err = s.Update(ctx, actor, id, UpdateUserInput{
			DisplayName:    current.DisplayName,
			Phone:          current.Phone,
			Email:          current.Email,
			Role:           current.Role,
			OrganizationID: organizationID,
		})
		if err != nil {
			outcome.Code, outcome.Message = describeBulkError(err)
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

// describeBulkError turns a service error into something a per-row report
// can carry, without letting an internal fault masquerade as a rule.
func describeBulkError(err error) (code, message string) {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code, apiErr.Message
	}
	// An unexpected failure is reported as one rather than as a validation
	// result, so a database problem does not read as "this account may not
	// be disabled".
	return "INTERNAL_ERROR", "This account could not be changed."
}
