package service

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
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
}

// NewUserService wires a UserService.
func NewUserService(st *store.Store, audit *AuditService, settings *SettingsService, tokens *auth.TokenService) *UserService {
	return &UserService{store: st, audit: audit, settings: settings, tokens: tokens}
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

// UserQuery filters a user listing.
type UserQuery struct {
	// Keyword matches the username or display name (§3.1).
	Keyword string
	// Status, Role, and OrganizationID are exact filters; empty means all.
	Status         model.Status
	Role           model.Role
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
	if q.OrganizationID != "" {
		f.Add("organization_id = %s", q.OrganizationID)
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
		        password_changed_at, created_at, updated_at
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
			&u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt,
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
		PasswordChangedAt: &now,
		CreatedAt:         now,
		UpdatedAt:         now,
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

	return s.Get(ctx, tenantID, id)
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

	orgID, err := s.resolveAssignableOrganization(ctx, q, in.OrganizationID)
	if err != nil {
		return model.User{}, err
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

	return s.Get(ctx, actor.TenantID, userID)
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

	return s.Get(ctx, actor.TenantID, userID)
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
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
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
