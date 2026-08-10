package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Session is what a successful login returns.
type Session struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expiresAt"`
	User      model.User `json:"user"`
}

// signInOutcome classifies a sign-in result for the metric.
//
// errors.Is rather than equality: a caller may wrap these, and a wrapped
// ErrAccountLocked counted as a generic error would hide exactly the number
// an operator turns lockout on in order to watch.
//
// MISSING_CREDENTIALS falls through to error deliberately. An empty form is
// not a failed attempt, and counting it as one would make the
// bad-credentials rate track how often somebody pressed Enter too early.
func signInOutcome(err error) string {
	switch {
	case err == nil:
		return metrics.OutcomeSuccess
	case errors.Is(err, ErrInvalidCredentials):
		return metrics.OutcomeBadCredentials
	case errors.Is(err, ErrAccountLocked):
		return metrics.OutcomeLocked
	case errors.Is(err, ErrAccountDisabled):
		return metrics.OutcomeDisabled
	case errors.Is(err, ErrPasswordExpired):
		return metrics.OutcomeExpired
	default:
		return metrics.OutcomeError
	}
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
// Two answers are more specific — disabled, and locked — and both are
// reached only after the password has matched, so they are available to
// somebody who could already establish the account exists. Being vague at
// that point costs a person who typed the right password the one piece of
// information that would tell them what to do.
func (s *UserService) Login(ctx context.Context, tenant model.Tenant, identifier, password, ip, userAgent string) (session Session, err error) {
	// Recorded once, from the error, rather than at each of the seven places
	// this can end. The reason is not brevity: a sign-in outcome that has to
	// be remembered at every return is one that will be missing from
	// whichever branch gets added next, and a metric that silently stops
	// counting a case looks exactly like that case not happening.
	defer func() { s.metrics.RecordSignIn(signInOutcome(err)) }()

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

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return Session{}, err
	}

	if !auth.CheckPassword(row.PasswordHash, password) {
		// The audit entry records the account, not the identifier that named
		// it — the trail is about who, and an email and a username reaching
		// the same account are the same event.
		s.countFailure(ctx, tenant, row.ID, row.Username, ip, settings)
		return Session{}, ErrInvalidCredentials
	}

	// Checked after the password, on the same reasoning as the disabled
	// account below: reaching here means the caller knows the password, so
	// saying the account is locked tells them nothing they could not already
	// establish, and telling them is far more useful than "wrong password"
	// for somebody who just typed the right one.
	//
	// Placing it after also means a wrong guess never learns that an account
	// is locked, so an attacker cannot use the lock as an oracle for which
	// accounts they have been getting close to.
	if row.LockedUntil != nil && row.LockedUntil.After(store.Now()) {
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account locked")
		return Session{}, ErrAccountLocked
	}

	if model.Status(row.Status) != model.StatusActive {
		// Closed by its owner is reported as such rather than as disabled.
		// The two look identical in the status column and call for entirely
		// different next steps: one person should talk to their
		// administrator about a suspension, the other is asking to come
		// back. Telling them both "disabled" sends one of them down the
		// wrong path.
		if row.ClosedAt != nil {
			s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account closed by its owner")
			return Session{}, ErrAccountClosed
		}
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account disabled")
		return Session{}, ErrAccountDisabled
	}

	// A self-registered account that has not proved its address does not get
	// in, where the tenant requires that.
	//
	// This one discloses, unlike almost everything else on this path: it
	// tells a caller who got the password right that the account exists and
	// is unverified. That is a registration oracle and it is accepted
	// deliberately, because the alternative is a dead end — somebody who
	// registered and never opened the email would be refused with no way to
	// find out why. The disclosure is confined to whoever already has the
	// password; the public resend endpoint gives nothing away.
	//
	// Only REGISTRATION accounts. An administrator-created, imported, or
	// directory-synchronized account is vouched for by whoever created it.
	if model.UserSource(row.Source) == model.SourceRegistration && row.VerifiedAt == nil {
		if settings.RegistrationVerification {
			s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "address not verified")
			return Session{}, ErrAccountUnverified
		}
	}

	// An expired password does not produce a session. Issuing one and
	// flagging it would mean handing out a working token and asking the
	// client to be well behaved, which an API client simply would not be.
	// The way forward is ChangeExpiredPassword, which takes the old password
	// and a new one and issues the session itself.
	if settings.PasswordPolicy().Expired(row.PasswordChangedAt, store.Now()) {
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "password expired")
		return Session{}, ErrPasswordExpired
	}

	// The password was right and nothing stood in the way, so whatever
	// failures preceded it were somebody mistyping.
	if row.FailedLoginAttempts > 0 || row.LockedUntil != nil {
		if err := q.ClearLoginFailures(ctx, row.ID, store.Now()); err != nil {
			return Session{}, fmt.Errorf("clear login failures: %w", err)
		}
	}

	user, err := s.Get(ctx, tenant.ID, row.ID)
	if err != nil {
		return Session{}, err
	}

	// The session row comes first: the token names it, so it has to exist
	// before the token does. A row with no token is harmless — it expires —
	// whereas a token naming a row that was never written authenticates
	// nobody, which reads as a broken deployment rather than a failed
	// sign-in.
	sessionID := uuid.NewString()
	now := store.Now()
	ttl := settings.TokenTTL()

	err = q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID:        sessionID,
		UserID:    user.ID,
		Ip:        ip,
		UserAgent: userAgent,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	token, expiresAt, err := s.tokens.Issue(user, tenant.Code, sessionID, row.TokenVersion, ttl)
	if err != nil {
		return Session{}, err
	}
	s.metrics.RecordTokenIssued(metrics.TokenSession)

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLoginSuccess,
		Result:  model.LogSuccess,
		ActorID: user.ID, ActorName: user.Username,
		IP: ip,
	})

	return Session{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

// ChangeExpiredPassword is the way back in for somebody whose password has
// aged out.
//
// It takes credentials rather than a session because there is no session to
// take: Login refuses an expired password outright rather than issuing a
// token and trusting the client to act on a flag. So this re-checks the old
// password itself, applies the same lockout accounting a sign-in would, and
// issues the session once the new password is set.
//
// It refuses when the password is *not* expired, so it cannot be used as an
// alternative change-password endpoint that skips being signed in.
func (s *UserService) ChangeExpiredPassword(ctx context.Context, tenant model.Tenant, identifier, currentPassword, newPassword, ip, userAgent string) (Session, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || currentPassword == "" {
		return Session{}, httpx.BadRequest("MISSING_CREDENTIALS",
			"An identifier and the current password are required.")
	}

	q := s.store.ForTenant(tenant.ID)

	row, err := q.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		if store.IsNoRows(err) {
			auth.BurnPasswordComparison()
			s.logLoginFailure(ctx, tenant.ID, "", identifier, ip, "no such user")
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("look up user: %w", err)
	}

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return Session{}, err
	}

	if !auth.CheckPassword(row.PasswordHash, currentPassword) {
		s.countFailure(ctx, tenant, row.ID, row.Username, ip, settings)
		return Session{}, ErrInvalidCredentials
	}
	if row.LockedUntil != nil && row.LockedUntil.After(store.Now()) {
		return Session{}, ErrAccountLocked
	}
	if model.Status(row.Status) != model.StatusActive {
		return Session{}, ErrAccountDisabled
	}
	if !settings.PasswordPolicy().Expired(row.PasswordChangedAt, store.Now()) {
		return Session{}, httpx.BadRequest("PASSWORD_NOT_EXPIRED",
			"This password has not expired. Sign in and change it from your profile.")
	}

	if err := s.setPassword(ctx, q, tenant.ID, row.ID, newPassword); err != nil {
		return Session{}, err
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordExpiredChange,
		ActorID: row.ID, ActorName: row.Username,
		TargetType: "USER", TargetID: row.ID, TargetName: row.Username,
		IP: ip,
	})

	// Signing them in afterwards rather than sending them back to the form:
	// they have just proved the old password and chosen a new one, and
	// making them type the new one again immediately teaches nothing.
	return s.Login(ctx, tenant, identifier, newPassword, ip, userAgent)
}

// countFailure records a wrong password and locks the account if that takes
// it to the tenant's threshold.
//
// This is per-account, and it is not the same control as the per-IP
// throttling a reverse proxy does. A proxy rate limit stops one source
// hammering the endpoint and does nothing about a slow spray from many
// addresses against one account; this stops the second and does nothing
// about the first. Both are wanted, which is why the deployment guide still
// asks for the proxy.
//
// A lock is temporary and never extended by further failures. Otherwise
// anyone who knows a username could keep that person locked out
// indefinitely simply by guessing at them, which trades one denial of
// service for another.
func (s *UserService) countFailure(ctx context.Context, tenant model.Tenant, userID, username, ip string, settings Settings) {
	if !settings.LockoutEnabled() {
		s.logLoginFailure(ctx, tenant.ID, userID, username, ip, "wrong password")
		return
	}

	now := store.Now()
	result, err := s.store.ForTenant(tenant.ID).RecordFailedLogin(ctx,
		sqlcgen.RecordFailedLoginParams{
			ID:          userID,
			Now:         now,
			WindowStart: now.Add(-settings.LockoutDuration()),
			Threshold:   lockoutThreshold(settings),
			LockUntil:   now.Add(settings.LockoutDuration()),
		})
	if err != nil {
		// The sign-in has already failed; failing to count it must not turn
		// that into a 500, which would tell a caller their guess was
		// interesting.
		slog.ErrorContext(ctx, "could not record failed sign-in",
			"error", err, "user_id", userID)
		s.logLoginFailure(ctx, tenant.ID, userID, username, ip, "wrong password")
		return
	}

	reason := fmt.Sprintf("wrong password (%d of %d)",
		result.FailedLoginAttempts, settings.LockoutThreshold)
	if result.LockedUntil != nil && result.LockedUntil.After(now) {
		reason = fmt.Sprintf("wrong password (%d of %d); account locked until %s",
			result.FailedLoginAttempts, settings.LockoutThreshold,
			result.LockedUntil.UTC().Format(time.RFC3339))
		// Counted here, where the lock is applied, rather than where a locked
		// account is turned away. A lock is applied once and refused many
		// times, and the two are different questions: how many accounts got
		// locked, versus how hard somebody is pushing at one that already is.
		s.metrics.RecordLockout()
	}
	s.logLoginFailure(ctx, tenant.ID, userID, username, ip, reason)
}

// lockoutThreshold narrows the configured threshold to the width the query
// takes.
//
// Update already refuses anything above MaxLockoutThreshold, so this cannot
// truncate in practice — but a bare conversion says that only in the
// validation two files away, and a scanner cannot read it there. Clamping
// here makes the bound local and true regardless of how the value arrived.
func lockoutThreshold(settings Settings) int32 {
	if settings.LockoutThreshold > MaxLockoutThreshold {
		return MaxLockoutThreshold
	}
	if settings.LockoutThreshold < 0 {
		return 0
	}
	return int32(settings.LockoutThreshold)
}

func (s *UserService) logLoginFailure(ctx context.Context, tenantID, userID, username, ip, reason string) {
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLoginFailure,
		Result:  model.LogFailure,
		ActorID: userID, ActorName: username,
		Detail: reason, IP: ip,
	})
}

// Logout ends the session the caller is using.
//
// This one, not all of them. Before sessions existed the only revocation
// available was bumping token_version, which invalidates every token an
// account holds — so signing out on a laptop signed you out on your phone
// as well. That was never intended, only unavoidable. LogoutEverywhere is
// the deliberate version.
//
// Federated sessions are a separate question, and they still all go. Signing
// out of Portico could reasonably leave a relying party's own session
// running, since that is what its end_session endpoint is for. It does not,
// because "sign out" on a single sign-on system is read by the person
// clicking it as signing out of the things they signed in to, and the
// surprising failure is the one where it did less than they thought. A
// second browser stays signed in to Portico; nothing stays signed in to the
// applications.
func (s *UserService) Logout(ctx context.Context, actor auth.Principal, ip string) error {
	q := s.store.ForTenant(actor.TenantID)

	if err := q.RevokeSession(ctx, actor.SessionID, store.Now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	if err := q.RevokeAllRefreshTokensForUser(ctx, actor.UserID, store.Now()); err != nil {
		return fmt.Errorf("revoke federated sessions: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLogout,
		ActorID: actor.UserID, ActorName: actor.Username,
		IP: ip,
	})
	return nil
}

// LogoutEverywhere ends every session the account holds, on every device.
//
// What somebody reaches for when they think a session is not theirs. It
// bumps token_version as well as revoking the rows: that is redundant while
// both mechanisms agree, and it is the cheap insurance that a token which
// somehow escaped the session check still stops working.
func (s *UserService) LogoutEverywhere(ctx context.Context, actor auth.Principal, ip string) error {
	q := s.store.ForTenant(actor.TenantID)
	now := store.Now()

	if err := q.RevokeSessionsForUser(ctx, actor.UserID, now); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	err := q.BumpUserTokenVersion(ctx, sqlcgen.BumpUserTokenVersionParams{
		ID: actor.UserID, UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("bump token version: %w", err)
	}
	if err := q.RevokeAllRefreshTokensForUser(ctx, actor.UserID, now); err != nil {
		return fmt.Errorf("revoke federated sessions: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLogoutEverywhere,
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

	if err := s.setPassword(ctx, q, actor.TenantID, actor.UserID, newPassword); err != nil {
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

	if err := s.setPassword(ctx, q, actor.TenantID, userID, newPassword); err != nil {
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
//
// It is the single place all three password paths pass through — self
// change, administrator reset, and recovery — which is what makes it the
// right place to also cut the federated sessions.
func (s *UserService) setPassword(ctx context.Context, q *store.Scoped, tenantID, userID, plaintext string) error {
	// Every password in the system arrives through here — self-service
	// change, administrator reset, completed recovery, and the expired-
	// password path — so the policy is applied once rather than at four
	// call sites with four chances to leave one out.
	policy, err := s.PasswordPolicyFor(ctx, tenantID)
	if err != nil {
		return err
	}
	if err := policy.Check(plaintext); err != nil {
		return err
	}
	if err := s.checkReuse(ctx, q, userID, plaintext, policy); err != nil {
		return err
	}

	hash, err := auth.HashPassword(plaintext)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	err = q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: hash,
		Now:          store.Now(),
	})
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Bumping token_version only reaches Portico's own sessions. A refresh
	// token issued to a relying party is a separate credential, and leaving
	// it alive would mean the account somebody just took back could still be
	// refreshed indefinitely by whoever knew the old password.
	if err := q.RevokeAllRefreshTokensForUser(ctx, userID, store.Now()); err != nil {
		return fmt.Errorf("revoke federated sessions: %w", err)
	}

	// A new password means whoever set it is in control of the account, so
	// the failures that preceded it are no longer interesting and any lock
	// they caused should not outlive them. This is the one place all three
	// password paths meet — recovery, self-service change, administrator
	// reset — so putting it here covers them without three chances to
	// forget one.
	if err := q.ClearLoginFailures(ctx, userID, store.Now()); err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}

	// The session rows too. Bumping token_version above already stops every
	// token, but leaving the rows live would show somebody a list of
	// sessions that no longer work — and a list that lies about what is
	// signed in is worse than no list.
	if err := q.RevokeSessionsForUser(ctx, userID, store.Now()); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	// Recorded last, so a password refused for any reason above never lands
	// in the history and blocks itself from being set later.
	return s.rememberPassword(ctx, q, userID, hash, policy)
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

	// Accepted under the rules in force at the time.
	//
	// Where the tenant does not require verification, the address is marked
	// accepted now — so turning the requirement on later applies to
	// registrations after it and not to people who are already using their
	// accounts. The migration that introduced the column does the same thing
	// for accounts that predate it; this is that rule applied continuously
	// rather than once, and without it a policy change silently revokes
	// access from every existing member.
	settings, err := s.settings.Get(ctx, tenantID)
	if err != nil {
		return model.User{}, err
	}
	if !settings.RegistrationVerification {
		if err := s.store.ForTenant(tenantID).MarkUserVerified(ctx, user.ID, store.Now()); err != nil {
			return model.User{}, fmt.Errorf("mark accepted: %w", err)
		}
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
