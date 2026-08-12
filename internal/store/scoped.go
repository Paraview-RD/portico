package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Scoped is the query layer bound to a single tenant.
//
// It exists because tenant isolation cannot be a rule people follow. One
// query that forgets its tenant_id predicate is a cross-tenant data leak,
// and a missing WHERE clause is close to invisible in review — it reads
// exactly like a query that is simply less specific. So the tenant is bound
// once, here, and the service layer is given an object through which asking
// for another tenant's rows is not expressible.
//
// Three things hold the guarantee up together:
//
//  1. Every statement in internal/store/queries reachable from here filters
//     on tenant_id. TestTenantScopedQueriesFilterByTenant reads the .sql
//     files and fails if one does not.
//  2. The tenant argument is supplied here rather than by the caller, so a
//     handler cannot pass a tenant taken from the request instead of from
//     the authenticated principal. Where a generated params struct still has
//     a TenantID field, this overwrites it — whatever the caller set is
//     discarded, not honoured.
//  3. The tests in internal/server/tenancy_test.go exercise the API with two
//     tenants holding identically named rows and assert that neither can see
//     or change the other's.
//
// Store.Queries remains available for the tables that are not tenant-scoped
// (tenants themselves) and for the two queries that run before the tenant is
// known, because they are what establishes it: authentication by user id,
// and SCIM credential lookup by token hash. Those are the only legitimate
// uses, and the guard test enforces that the second group has exactly two
// members.
type Scoped struct {
	q        *sqlcgen.Queries
	db       *sql.DB
	tenantID string
}

// ForTenant returns a view of the query layer bound to tenantID.
//
// It does not verify that the tenant exists. Callers get a tenant either
// from an authenticated principal — where a foreign key already guarantees
// it — or from an explicit lookup by code, which checks existence and status
// as part of resolving it. An empty or unknown tenant fails closed anyway:
// reads match no rows and writes are refused by the foreign key.
func (s *Store) ForTenant(tenantID string) *Scoped {
	return &Scoped{q: s.Queries, db: s.db, tenantID: tenantID}
}

// TenantID is the tenant this view is bound to. It is needed by the few
// list queries whose WHERE clause varies with the filters supplied and so
// cannot be generated; those build their SQL through a helper that seeds the
// tenant predicate first.
func (s *Scoped) TenantID() string { return s.tenantID }

// DB exposes the raw handle for those same hand-written queries.
func (s *Scoped) DB() *sql.DB { return s.db }

// --- users ---------------------------------------------------------------

// GetUserByID returns one account of this tenant.
func (s *Scoped) GetUserByID(ctx context.Context, id string) (sqlcgen.User, error) {
	return s.q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: s.tenantID, ID: id})
}

// GetUserByUsername returns the account holding a username in this tenant.
// Usernames are unique per tenant, so this is unambiguous only once the
// tenant is known.
func (s *Scoped) GetUserByUsername(ctx context.Context, username string) (sqlcgen.User, error) {
	return s.q.GetUserByUsername(ctx,
		sqlcgen.GetUserByUsernameParams{TenantID: s.tenantID, Username: username})
}

// GetUserByIdentifier returns the account a sign-in identifier names — a
// username, email address, or phone number, in that order of precedence.
//
// Only sign-in may use this. Password recovery resolves through
// GetUserByEmail or GetUserByPhone, which match a single column, because
// resolving across columns and then sending a token would let a colliding
// identifier route someone else's reset.
func (s *Scoped) GetUserByIdentifier(ctx context.Context, identifier string) (sqlcgen.User, error) {
	return s.q.GetUserByIdentifier(ctx,
		sqlcgen.GetUserByIdentifierParams{TenantID: s.tenantID, Username: identifier})
}

// GetUserByEmail returns the account with a bound email address. For
// password recovery over email.
func (s *Scoped) GetUserByEmail(ctx context.Context, email string) (sqlcgen.User, error) {
	return s.q.GetUserByEmail(ctx,
		sqlcgen.GetUserByEmailParams{TenantID: s.tenantID, Email: email})
}

// GetUserByPhone returns the account with a bound phone number. For password
// recovery over SMS.
func (s *Scoped) GetUserByPhone(ctx context.Context, phone string) (sqlcgen.User, error) {
	return s.q.GetUserByPhone(ctx,
		sqlcgen.GetUserByPhoneParams{TenantID: s.tenantID, Phone: phone})
}

// ListUsersByIDs returns the accounts among ids that belong to this tenant.
// Ids from elsewhere are silently absent rather than an error, which is
// what makes a batch lookup safe to call with ids of unknown provenance.
func (s *Scoped) ListUsersByIDs(ctx context.Context, ids []string) ([]sqlcgen.User, error) {
	return s.q.ListUsersByIDs(ctx, sqlcgen.ListUsersByIDsParams{TenantID: s.tenantID, Column2: ids})
}

// CreateUser adds an account to this tenant.
func (s *Scoped) CreateUser(ctx context.Context, arg sqlcgen.CreateUserParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateUser(ctx, arg)
}

// UpdateUserProfile changes an account's profile, role, and organization.
func (s *Scoped) UpdateUserProfile(ctx context.Context, arg sqlcgen.UpdateUserProfileParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateUserProfile(ctx, arg)
}

// RenameUser writes the username alone, for a directory that renamed an
// account it owns.
func (s *Scoped) RenameUser(ctx context.Context, arg sqlcgen.RenameUserParams) error {
	arg.TenantID = s.tenantID
	return s.q.RenameUser(ctx, arg)
}

// UpdateUserStatus enables or disables an account, revoking live sessions
// on disable.
func (s *Scoped) UpdateUserStatus(ctx context.Context, arg sqlcgen.UpdateUserStatusParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateUserStatus(ctx, arg)
}

// UpdateUserPassword replaces a password hash and revokes live sessions.
func (s *Scoped) UpdateUserPassword(ctx context.Context, arg sqlcgen.UpdateUserPasswordParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateUserPassword(ctx, arg)
}

// BumpUserTokenVersion revokes every token currently held by an account.
func (s *Scoped) BumpUserTokenVersion(ctx context.Context, arg sqlcgen.BumpUserTokenVersionParams) error {
	arg.TenantID = s.tenantID
	return s.q.BumpUserTokenVersion(ctx, arg)
}

// DeleteAuditLogsBefore removes entries older than a cutoff. Only called
// when a tenant has configured a retention period.
func (s *Scoped) DeleteAuditLogsBefore(ctx context.Context, before time.Time) error {
	return s.q.DeleteAuditLogsBefore(ctx,
		sqlcgen.DeleteAuditLogsBeforeParams{TenantID: s.tenantID, CreatedAt: before})
}

// --- sessions ------------------------------------------------------------

// CreateSession records a sign-in.
func (s *Scoped) CreateSession(ctx context.Context, arg sqlcgen.CreateSessionParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSession(ctx, arg)
}

// GetLiveSession returns an unrevoked, unexpired session by id.
func (s *Scoped) GetLiveSession(ctx context.Context, id string, now time.Time) (sqlcgen.Session, error) {
	return s.q.GetLiveSession(ctx,
		sqlcgen.GetLiveSessionParams{TenantID: s.tenantID, ID: id, ExpiresAt: now})
}

// TouchSession records that a session was used.
func (s *Scoped) TouchSession(ctx context.Context, id string, now time.Time) error {
	return s.q.TouchSession(ctx,
		sqlcgen.TouchSessionParams{TenantID: s.tenantID, ID: id, LastSeenAt: now})
}

// ListSessionsForUser returns an account's live sessions, most recently used
// first.
func (s *Scoped) ListSessionsForUser(ctx context.Context, userID string, now time.Time) ([]sqlcgen.Session, error) {
	return s.q.ListSessionsForUser(ctx,
		sqlcgen.ListSessionsForUserParams{TenantID: s.tenantID, UserID: userID, ExpiresAt: now})
}

// RevokeSession ends one session.
func (s *Scoped) RevokeSession(ctx context.Context, id string, now time.Time) error {
	return s.q.RevokeSession(ctx,
		sqlcgen.RevokeSessionParams{TenantID: s.tenantID, ID: id, RevokedAt: &now})
}

// RevokeSessionsForUser ends every session an account holds.
func (s *Scoped) RevokeSessionsForUser(ctx context.Context, userID string, now time.Time) error {
	return s.q.RevokeSessionsForUser(ctx,
		sqlcgen.RevokeSessionsForUserParams{TenantID: s.tenantID, UserID: userID, RevokedAt: &now})
}

// DeleteExpiredSessions clears rows past the retention window.
func (s *Scoped) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredSessions(ctx,
		sqlcgen.DeleteExpiredSessionsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// RecordPasswordInHistory remembers a hash so a policy can refuse reuse.
func (s *Scoped) RecordPasswordInHistory(ctx context.Context, id, userID, hash string, now time.Time) error {
	return s.q.RecordPasswordInHistory(ctx, sqlcgen.RecordPasswordInHistoryParams{
		ID: id, TenantID: s.tenantID, UserID: userID,
		PasswordHash: hash, CreatedAt: now,
	})
}

// RecentPasswordHashes returns the newest depth hashes for an account.
func (s *Scoped) RecentPasswordHashes(ctx context.Context, userID string, depth int32) ([]string, error) {
	return s.q.RecentPasswordHashes(ctx,
		sqlcgen.RecentPasswordHashesParams{TenantID: s.tenantID, UserID: userID, Limit: depth})
}

// TrimPasswordHistory drops entries past the configured depth.
func (s *Scoped) TrimPasswordHistory(ctx context.Context, userID string, depth int32) error {
	return s.q.TrimPasswordHistory(ctx,
		sqlcgen.TrimPasswordHistoryParams{TenantID: s.tenantID, UserID: userID, Limit: depth})
}

// RecordFailedLogin counts a failed sign-in and locks the account if that
// takes it to the threshold. It returns the new count and the lock, if any.
func (s *Scoped) RecordFailedLogin(ctx context.Context, arg sqlcgen.RecordFailedLoginParams) (sqlcgen.RecordFailedLoginRow, error) {
	arg.TenantID = s.tenantID
	return s.q.RecordFailedLogin(ctx, arg)
}

// ClearLoginFailures forgets an account's failures and unlocks it.
func (s *Scoped) ClearLoginFailures(ctx context.Context, userID string, now time.Time) error {
	return s.q.ClearLoginFailures(ctx,
		sqlcgen.ClearLoginFailuresParams{TenantID: s.tenantID, ID: userID, Now: now})
}

// CountUsers counts this tenant's accounts.
func (s *Scoped) CountUsers(ctx context.Context) (int64, error) {
	return s.q.CountUsers(ctx, s.tenantID)
}

// CountUsersByOrganization counts the members of one organization.
func (s *Scoped) CountUsersByOrganization(ctx context.Context, organizationID *string) (int64, error) {
	return s.q.CountUsersByOrganization(ctx,
		sqlcgen.CountUsersByOrganizationParams{TenantID: s.tenantID, OrganizationID: organizationID})
}

// CountUsersPerOrganization returns member counts for every organization
// with at least one, in one round trip.
func (s *Scoped) CountUsersPerOrganization(ctx context.Context) ([]sqlcgen.CountUsersPerOrganizationRow, error) {
	return s.q.CountUsersPerOrganization(ctx, s.tenantID)
}

// CountOtherActiveAdmins counts the active administrators other than
// excludeUserID, which is what tells the caller whether demoting or
// disabling that account would leave the tenant unadministrable.
func (s *Scoped) CountOtherActiveAdmins(ctx context.Context, role, status, excludeUserID string) (int64, error) {
	return s.q.CountOtherActiveAdmins(ctx, sqlcgen.CountOtherActiveAdminsParams{
		TenantID: s.tenantID,
		Role:     role,
		Status:   status,
		ID:       excludeUserID,
	})
}

// --- organizations -------------------------------------------------------

// GetOrganizationByID returns one of this tenant's organizations.
func (s *Scoped) GetOrganizationByID(ctx context.Context, id string) (sqlcgen.Organization, error) {
	return s.q.GetOrganizationByID(ctx,
		sqlcgen.GetOrganizationByIDParams{TenantID: s.tenantID, ID: id})
}

// GetOrganizationByCode returns the organization holding a code in this
// tenant. Codes are unique per tenant, not globally.
func (s *Scoped) GetOrganizationByCode(ctx context.Context, code string) (sqlcgen.Organization, error) {
	return s.q.GetOrganizationByCode(ctx,
		sqlcgen.GetOrganizationByCodeParams{TenantID: s.tenantID, Code: code})
}

// ListOrganizations returns every organization in display order.
func (s *Scoped) ListOrganizations(ctx context.Context) ([]sqlcgen.Organization, error) {
	return s.q.ListOrganizations(ctx, s.tenantID)
}

// ListActiveOrganizations returns the organizations that can take new
// members.
func (s *Scoped) ListActiveOrganizations(ctx context.Context) ([]sqlcgen.Organization, error) {
	return s.q.ListActiveOrganizations(ctx, s.tenantID)
}

// ListOrganizationsByIDs returns the organizations among ids that belong to
// this tenant.
func (s *Scoped) ListOrganizationsByIDs(ctx context.Context, ids []string) ([]sqlcgen.Organization, error) {
	return s.q.ListOrganizationsByIDs(ctx,
		sqlcgen.ListOrganizationsByIDsParams{TenantID: s.tenantID, Column2: ids})
}

// CreateOrganization adds an organization to this tenant.
func (s *Scoped) CreateOrganization(ctx context.Context, arg sqlcgen.CreateOrganizationParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateOrganization(ctx, arg)
}

// UpdateOrganization changes an organization's name, remark, and ordering.
func (s *Scoped) UpdateOrganization(ctx context.Context, arg sqlcgen.UpdateOrganizationParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateOrganization(ctx, arg)
}

// UpdateOrganizationStatus enables or disables an organization.
func (s *Scoped) UpdateOrganizationStatus(ctx context.Context, arg sqlcgen.UpdateOrganizationStatusParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateOrganizationStatus(ctx, arg)
}

// --- password recovery ---------------------------------------------------

// CreatePasswordReset records an outstanding reset request.
func (s *Scoped) CreatePasswordReset(ctx context.Context, arg sqlcgen.CreatePasswordResetParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreatePasswordReset(ctx, arg)
}

// GetLivePasswordReset returns the unspent, unexpired reset a token names.
// A spent or expired one is not returned at all, so a caller cannot hold a
// dead row and forget to check it.
func (s *Scoped) GetLivePasswordReset(ctx context.Context, tokenHash string, now time.Time) (sqlcgen.PasswordReset, error) {
	return s.q.GetLivePasswordReset(ctx, sqlcgen.GetLivePasswordResetParams{
		TenantID: s.tenantID, TokenHash: tokenHash, ExpiresAt: now,
	})
}

// SpendPasswordReset marks a reset used, making it single-use.
func (s *Scoped) SpendPasswordReset(ctx context.Context, id string, now time.Time) error {
	return s.q.SpendPasswordReset(ctx,
		sqlcgen.SpendPasswordResetParams{TenantID: s.tenantID, ID: id, UsedAt: &now})
}

// SupersedePasswordResets marks a user's outstanding resets used, so a new
// request invalidates every earlier link.
func (s *Scoped) SupersedePasswordResets(ctx context.Context, userID string, now time.Time) error {
	return s.q.SupersedePasswordResets(ctx,
		sqlcgen.SupersedePasswordResetsParams{TenantID: s.tenantID, UserID: userID, UsedAt: &now})
}

// --- federation -----------------------------------------------------------
//
// Every relying party, key, authorization request, and refresh token belongs
// to one tenant, because each tenant is its own issuer. That is not a detail
// of the storage layout: it is what makes a token minted for one tenant
// unusable against another, since a relying party checks `iss` and the key
// set behind it.

// CreateSigningKey stores a newly generated key.
func (s *Scoped) CreateSigningKey(ctx context.Context, arg sqlcgen.CreateSigningKeyParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSigningKey(ctx, arg)
}

// GetActiveSigningKey returns the key new tokens are signed with.
func (s *Scoped) GetActiveSigningKey(ctx context.Context) (sqlcgen.OauthSigningKey, error) {
	return s.q.GetActiveSigningKey(ctx, s.tenantID)
}

// ListPublishedSigningKeys returns everything the JWKS advertises: the active
// key, and any key retired more recently than notBefore.
func (s *Scoped) ListPublishedSigningKeys(ctx context.Context, notBefore time.Time) ([]sqlcgen.OauthSigningKey, error) {
	return s.q.ListPublishedSigningKeys(ctx, sqlcgen.ListPublishedSigningKeysParams{
		TenantID: s.tenantID, RetiredAt: &notBefore,
	})
}

// RetireSigningKeys marks the current key retired. It stays in the key set
// until its tokens expire.
func (s *Scoped) RetireSigningKeys(ctx context.Context, now time.Time) error {
	return s.q.RetireSigningKeys(ctx,
		sqlcgen.RetireSigningKeysParams{TenantID: s.tenantID, RetiredAt: &now})
}

// DeleteExpiredSigningKeys drops keys retired long enough that nothing they
// signed can still verify.
func (s *Scoped) DeleteExpiredSigningKeys(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredSigningKeys(ctx,
		sqlcgen.DeleteExpiredSigningKeysParams{TenantID: s.tenantID, RetiredAt: &before})
}

// CreateOAuthClient registers a relying party.
func (s *Scoped) CreateOAuthClient(ctx context.Context, arg sqlcgen.CreateOAuthClientParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateOAuthClient(ctx, arg)
}

// GetOAuthClient returns a relying party by the client_id it presents.
func (s *Scoped) GetOAuthClient(ctx context.Context, clientID string) (sqlcgen.OauthClient, error) {
	return s.q.GetOAuthClient(ctx,
		sqlcgen.GetOAuthClientParams{TenantID: s.tenantID, ClientID: clientID})
}

// ListOAuthClients returns every relying party registered in this tenant.
func (s *Scoped) ListOAuthClients(ctx context.Context) ([]sqlcgen.OauthClient, error) {
	return s.q.ListOAuthClients(ctx, s.tenantID)
}

// UpdateOAuthClientStatus enables or disables a relying party.
func (s *Scoped) UpdateOAuthClientStatus(ctx context.Context, clientID, status string, now time.Time) error {
	return s.q.UpdateOAuthClientStatus(ctx, sqlcgen.UpdateOAuthClientStatusParams{
		TenantID: s.tenantID, ClientID: clientID, Status: status, UpdatedAt: now,
	})
}

// UpdateOAuthClient changes a relying party's settings.
func (s *Scoped) UpdateOAuthClient(ctx context.Context, arg sqlcgen.UpdateOAuthClientParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateOAuthClient(ctx, arg)
}

// UpdateOAuthClientSecret replaces a confidential client's secret hash.
func (s *Scoped) UpdateOAuthClientSecret(ctx context.Context, clientID string, secretHash *string, now time.Time) error {
	return s.q.UpdateOAuthClientSecret(ctx, sqlcgen.UpdateOAuthClientSecretParams{
		TenantID: s.tenantID, ClientID: clientID, SecretHash: secretHash, UpdatedAt: now,
	})
}

// DeleteExpiredPasswordResets clears reset requests past the retention
// window.
func (s *Scoped) DeleteExpiredPasswordResets(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredPasswordResets(ctx,
		sqlcgen.DeleteExpiredPasswordResetsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// DeleteDeadRefreshTokenChains clears rotation chains whose last token
// expired long enough ago that nothing downstream can still be presented.
func (s *Scoped) DeleteDeadRefreshTokenChains(ctx context.Context, before time.Time) error {
	return s.q.DeleteDeadRefreshTokenChains(ctx,
		sqlcgen.DeleteDeadRefreshTokenChainsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// CreateAuthRequest records an authorization request in flight.
func (s *Scoped) CreateAuthRequest(ctx context.Context, arg sqlcgen.CreateAuthRequestParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateAuthRequest(ctx, arg)
}

// GetAuthRequest returns an unexpired request by id.
func (s *Scoped) GetAuthRequest(ctx context.Context, id string, now time.Time) (sqlcgen.OauthAuthRequest, error) {
	return s.q.GetAuthRequest(ctx,
		sqlcgen.GetAuthRequestParams{TenantID: s.tenantID, ID: id, ExpiresAt: now})
}

// GetAuthRequestByCode returns the completed, unexpired request an
// authorization code names. A request that is neither is not returned at all.
func (s *Scoped) GetAuthRequestByCode(ctx context.Context, codeHash string, now time.Time) (sqlcgen.OauthAuthRequest, error) {
	return s.q.GetAuthRequestByCode(ctx,
		sqlcgen.GetAuthRequestByCodeParams{TenantID: s.tenantID, CodeHash: &codeHash, ExpiresAt: now})
}

// CompleteAuthRequest records who signed in.
func (s *Scoped) CompleteAuthRequest(ctx context.Context, id, subject string, authTime time.Time, amr []string) error {
	return s.q.CompleteAuthRequest(ctx, sqlcgen.CompleteAuthRequestParams{
		TenantID: s.tenantID, ID: id, Subject: &subject, AuthTime: &authTime, Amr: amr,
	})
}

// SaveAuthCode attaches an authorization code to a completed request.
func (s *Scoped) SaveAuthCode(ctx context.Context, id, codeHash string) error {
	return s.q.SaveAuthCode(ctx,
		sqlcgen.SaveAuthCodeParams{TenantID: s.tenantID, ID: id, CodeHash: &codeHash})
}

// DeleteAuthRequest removes a request, spent or abandoned.
func (s *Scoped) DeleteAuthRequest(ctx context.Context, id string) error {
	return s.q.DeleteAuthRequest(ctx,
		sqlcgen.DeleteAuthRequestParams{TenantID: s.tenantID, ID: id})
}

// DeleteExpiredAuthRequests clears requests nobody completed.
func (s *Scoped) DeleteExpiredAuthRequests(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredAuthRequests(ctx,
		sqlcgen.DeleteExpiredAuthRequestsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// CreateRefreshToken stores an issued refresh token.
func (s *Scoped) CreateRefreshToken(ctx context.Context, arg sqlcgen.CreateRefreshTokenParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateRefreshToken(ctx, arg)
}

// GetRefreshTokenByID returns a token row by its primary key, which is what
// the revocation endpoint is given rather than the token itself.
func (s *Scoped) GetRefreshTokenByID(ctx context.Context, id string) (sqlcgen.OauthRefreshToken, error) {
	return s.q.GetRefreshTokenByID(ctx,
		sqlcgen.GetRefreshTokenByIDParams{TenantID: s.tenantID, ID: id})
}

// GetRefreshToken returns a token row whatever its state, so the caller can
// tell an expired token from one that has already been spent.
func (s *Scoped) GetRefreshToken(ctx context.Context, tokenHash string) (sqlcgen.OauthRefreshToken, error) {
	return s.q.GetRefreshToken(ctx,
		sqlcgen.GetRefreshTokenParams{TenantID: s.tenantID, TokenHash: tokenHash})
}

// SpendRefreshToken marks a token used and records its replacement.
func (s *Scoped) SpendRefreshToken(ctx context.Context, id, replacedBy string, now time.Time) error {
	return s.q.SpendRefreshToken(ctx, sqlcgen.SpendRefreshTokenParams{
		TenantID: s.tenantID, ID: id, ReplacedBy: &replacedBy, UsedAt: &now,
	})
}

// RevokeRefreshToken revokes one token.
func (s *Scoped) RevokeRefreshToken(ctx context.Context, id string, now time.Time) error {
	return s.q.RevokeRefreshToken(ctx,
		sqlcgen.RevokeRefreshTokenParams{TenantID: s.tenantID, ID: id, RevokedAt: &now})
}

// RevokeRefreshTokenChain revokes a token and everything descended from it,
// which is the response to a spent token being presented.
func (s *Scoped) RevokeRefreshTokenChain(ctx context.Context, id string, now time.Time) error {
	return s.q.RevokeRefreshTokenChain(ctx,
		sqlcgen.RevokeRefreshTokenChainParams{TenantID: s.tenantID, ID: id, RevokedAt: &now})
}

// RevokeRefreshTokensForSession ends a person's session with one relying
// party.
func (s *Scoped) RevokeRefreshTokensForSession(ctx context.Context, subject, clientID string, now time.Time) error {
	return s.q.RevokeRefreshTokensForSession(ctx, sqlcgen.RevokeRefreshTokensForSessionParams{
		TenantID: s.tenantID, Subject: subject, ClientID: clientID, RevokedAt: &now,
	})
}

// RevokeAllRefreshTokensForUser reaches every relying party at once, which is
// what signing out of Portico, changing a password, and being disabled all
// have to do — otherwise "sessions revoke immediately" stops being true the
// moment federation is switched on.
func (s *Scoped) RevokeAllRefreshTokensForUser(ctx context.Context, subject string, now time.Time) error {
	return s.q.RevokeAllRefreshTokensForUser(ctx, sqlcgen.RevokeAllRefreshTokensForUserParams{
		TenantID: s.tenantID, Subject: subject, RevokedAt: &now,
	})
}

// --- audit ---------------------------------------------------------------

// CreateAuditLog appends an entry to this tenant's audit trail.
func (s *Scoped) CreateAuditLog(ctx context.Context, arg sqlcgen.CreateAuditLogParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateAuditLog(ctx, arg)
}

// --- settings ------------------------------------------------------------

// GetSetting reads one of this tenant's settings.
func (s *Scoped) GetSetting(ctx context.Context, key string) (sqlcgen.SystemSetting, error) {
	return s.q.GetSetting(ctx, sqlcgen.GetSettingParams{TenantID: s.tenantID, Key: key})
}

// ListSettings reads all of this tenant's stored settings. Keys with no row
// fall back to the defaults the settings service owns.
func (s *Scoped) ListSettings(ctx context.Context) ([]sqlcgen.SystemSetting, error) {
	return s.q.ListSettings(ctx, s.tenantID)
}

// UpsertSettings writes several settings in one transaction, so a partial
// write cannot leave a tenant with half of a configuration change applied.
func (s *Scoped) UpsertSettings(ctx context.Context, values map[string]string, updatedAt time.Time) error {
	return s.withTx(func(q *sqlcgen.Queries) error {
		for key, value := range values {
			err := q.UpsertSetting(ctx, sqlcgen.UpsertSettingParams{
				TenantID:  s.tenantID,
				Key:       key,
				Value:     value,
				UpdatedAt: updatedAt,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// --- SAML -----------------------------------------------------------------

// CreateSAMLSigningKey stores a newly generated key and its certificate.
func (s *Scoped) CreateSAMLSigningKey(ctx context.Context, arg sqlcgen.CreateSAMLSigningKeyParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSAMLSigningKey(ctx, arg)
}

// GetActiveSAMLSigningKey returns the key assertions are signed with.
func (s *Scoped) GetActiveSAMLSigningKey(ctx context.Context) (sqlcgen.SamlSigningKey, error) {
	return s.q.GetActiveSAMLSigningKey(ctx, s.tenantID)
}

// ListSAMLSigningKeys returns every key, active first. Retired keys are kept
// so an operator can see what a service provider may still be pinning.
func (s *Scoped) ListSAMLSigningKeys(ctx context.Context) ([]sqlcgen.SamlSigningKey, error) {
	return s.q.ListSAMLSigningKeys(ctx, s.tenantID)
}

// RetireSAMLSigningKeys marks the current key retired.
func (s *Scoped) RetireSAMLSigningKeys(ctx context.Context, now time.Time) error {
	return s.q.RetireSAMLSigningKeys(ctx,
		sqlcgen.RetireSAMLSigningKeysParams{TenantID: s.tenantID, RetiredAt: &now})
}

// CreateSAMLServiceProvider registers a service provider.
func (s *Scoped) CreateSAMLServiceProvider(ctx context.Context, arg sqlcgen.CreateSAMLServiceProviderParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSAMLServiceProvider(ctx, arg)
}

// GetSAMLServiceProvider returns a service provider by its entity id.
func (s *Scoped) GetSAMLServiceProvider(ctx context.Context, entityID string) (sqlcgen.SamlServiceProvider, error) {
	return s.q.GetSAMLServiceProvider(ctx,
		sqlcgen.GetSAMLServiceProviderParams{TenantID: s.tenantID, EntityID: entityID})
}

// GetSAMLServiceProviderByID returns a service provider by its own id.
func (s *Scoped) GetSAMLServiceProviderByID(ctx context.Context, id string) (sqlcgen.SamlServiceProvider, error) {
	return s.q.GetSAMLServiceProviderByID(ctx,
		sqlcgen.GetSAMLServiceProviderByIDParams{TenantID: s.tenantID, ID: id})
}

// ListSAMLServiceProviders returns every service provider in this tenant.
func (s *Scoped) ListSAMLServiceProviders(ctx context.Context) ([]sqlcgen.SamlServiceProvider, error) {
	return s.q.ListSAMLServiceProviders(ctx, s.tenantID)
}

// UpdateSAMLServiceProviderStatus enables or disables a service provider.
func (s *Scoped) UpdateSAMLServiceProviderStatus(ctx context.Context, entityID, status string, now time.Time) error {
	return s.q.UpdateSAMLServiceProviderStatus(ctx, sqlcgen.UpdateSAMLServiceProviderStatusParams{
		TenantID: s.tenantID, EntityID: entityID, Status: status, UpdatedAt: now,
	})
}

// UpdateSAMLServiceProvider replaces a service provider's name and metadata.
func (s *Scoped) UpdateSAMLServiceProvider(ctx context.Context, entityID, name, metadataXML, launchURL, logoURI string, now time.Time) error {
	return s.q.UpdateSAMLServiceProvider(ctx, sqlcgen.UpdateSAMLServiceProviderParams{
		TenantID: s.tenantID, EntityID: entityID,
		Name: name, MetadataXml: metadataXML, LaunchUrl: launchURL,
		LogoUri: logoURI, UpdatedAt: now,
	})
}

// CreateSAMLAuthRequest records an authentication request in flight.
func (s *Scoped) CreateSAMLAuthRequest(ctx context.Context, arg sqlcgen.CreateSAMLAuthRequestParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSAMLAuthRequest(ctx, arg)
}

// GetSAMLAuthRequest returns an unexpired request by id.
func (s *Scoped) GetSAMLAuthRequest(ctx context.Context, id string, now time.Time) (sqlcgen.SamlAuthRequest, error) {
	return s.q.GetSAMLAuthRequest(ctx,
		sqlcgen.GetSAMLAuthRequestParams{TenantID: s.tenantID, ID: id, ExpiresAt: now})
}

// CompleteSAMLAuthRequest records who signed in and the hashed secret the
// callback must present to mint the assertion.
func (s *Scoped) CompleteSAMLAuthRequest(ctx context.Context, id, subject, secretHash string) error {
	return s.q.CompleteSAMLAuthRequest(ctx, sqlcgen.CompleteSAMLAuthRequestParams{
		TenantID:         s.tenantID,
		ID:               id,
		Subject:          &subject,
		CompletionSecret: secretHash,
	})
}

// DeleteSAMLAuthRequest removes a request once its assertion has been sent.
func (s *Scoped) DeleteSAMLAuthRequest(ctx context.Context, id string) error {
	return s.q.DeleteSAMLAuthRequest(ctx,
		sqlcgen.DeleteSAMLAuthRequestParams{TenantID: s.tenantID, ID: id})
}

// DeleteExpiredSAMLAuthRequests clears requests nobody completed.
func (s *Scoped) DeleteExpiredSAMLAuthRequests(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredSAMLAuthRequests(ctx,
		sqlcgen.DeleteExpiredSAMLAuthRequestsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// --- CAS ------------------------------------------------------------------

// CreateCASService registers a CAS service.
func (s *Scoped) CreateCASService(ctx context.Context, arg sqlcgen.CreateCASServiceParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateCASService(ctx, arg)
}

// ListCASServices returns every CAS service in this tenant.
func (s *Scoped) ListCASServices(ctx context.Context) ([]sqlcgen.CasService, error) {
	return s.q.ListCASServices(ctx, s.tenantID)
}

// GetCASService returns one registration by its exact prefix.
func (s *Scoped) GetCASService(ctx context.Context, prefix string) (sqlcgen.CasService, error) {
	return s.q.GetCASService(ctx,
		sqlcgen.GetCASServiceParams{TenantID: s.tenantID, UrlPrefix: prefix})
}

// GetCASServiceByID returns one registration by its own id.
func (s *Scoped) GetCASServiceByID(ctx context.Context, id string) (sqlcgen.CasService, error) {
	return s.q.GetCASServiceByID(ctx,
		sqlcgen.GetCASServiceByIDParams{TenantID: s.tenantID, ID: id})
}

// UpdateCASServiceStatus enables or disables a CAS service.
func (s *Scoped) UpdateCASServiceStatus(ctx context.Context, prefix, status string, now time.Time) error {
	return s.q.UpdateCASServiceStatus(ctx, sqlcgen.UpdateCASServiceStatusParams{
		TenantID: s.tenantID, UrlPrefix: prefix, Status: status, UpdatedAt: now,
	})
}

// UpdateCASService changes a registration's name and URL prefix. The
// registration to change is named by its current prefix; UrlPrefix is the
// new one.
func (s *Scoped) UpdateCASService(ctx context.Context, currentPrefix, name, newPrefix, launchURL, logoURI string, now time.Time) error {
	return s.q.UpdateCASService(ctx, sqlcgen.UpdateCASServiceParams{
		TenantID: s.tenantID, UrlPrefix_2: currentPrefix,
		Name: name, UrlPrefix: newPrefix, LaunchUrl: launchURL,
		LogoUri: logoURI, UpdatedAt: now,
	})
}

// CreateCASTicket stores an issued service ticket.
func (s *Scoped) CreateCASTicket(ctx context.Context, arg sqlcgen.CreateCASTicketParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateCASTicket(ctx, arg)
}

// ConsumeCASTicket spends a ticket and returns it, in one statement. A read
// followed by a write would let two validations of the same ticket both
// succeed.
func (s *Scoped) ConsumeCASTicket(ctx context.Context, ticketHash string, now time.Time) (sqlcgen.CasTicket, error) {
	return s.q.ConsumeCASTicket(ctx, sqlcgen.ConsumeCASTicketParams{
		TenantID: s.tenantID, TicketHash: ticketHash, ConsumedAt: &now,
	})
}

// DeleteExpiredCASTickets clears tickets nobody validated.
func (s *Scoped) DeleteExpiredCASTickets(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredCASTickets(ctx,
		sqlcgen.DeleteExpiredCASTicketsParams{TenantID: s.tenantID, ExpiresAt: before})
}

// --- SCIM ------------------------------------------------------------
//
// Resolving a credential to its tenant is not here: it is the query that
// decides the tenant, so it cannot be reached through an object that already
// has one. It lives on Store.Queries, with GetUserForAuthentication.

// CreateSCIMCredential stores a new provisioning credential.
func (s *Scoped) CreateSCIMCredential(ctx context.Context, arg sqlcgen.CreateSCIMCredentialParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSCIMCredential(ctx, arg)
}

// ListSCIMCredentials returns the tenant's provisioning credentials.
func (s *Scoped) ListSCIMCredentials(ctx context.Context) ([]sqlcgen.ScimCredential, error) {
	return s.q.ListSCIMCredentials(ctx, s.tenantID)
}

// SetSCIMCredentialStatus enables or disables one.
func (s *Scoped) SetSCIMCredentialStatus(ctx context.Context, arg sqlcgen.SetSCIMCredentialStatusParams) error {
	arg.TenantID = s.tenantID
	return s.q.SetSCIMCredentialStatus(ctx, arg)
}

// DeleteSCIMCredential revokes one permanently.
func (s *Scoped) DeleteSCIMCredential(ctx context.Context, id string) error {
	return s.q.DeleteSCIMCredential(ctx,
		sqlcgen.DeleteSCIMCredentialParams{TenantID: s.tenantID, ID: id})
}

// TouchSCIMCredential records that a credential was used.
func (s *Scoped) TouchSCIMCredential(ctx context.Context, arg sqlcgen.TouchSCIMCredentialParams) error {
	arg.TenantID = s.tenantID
	return s.q.TouchSCIMCredential(ctx, arg)
}

// GetUserByExternalID finds the account a provisioning system knows by that
// identifier. Scoped, unlike the credential lookup: by the time this runs the
// tenant is settled.
func (s *Scoped) GetUserByExternalID(ctx context.Context, externalID string) (sqlcgen.User, error) {
	return s.q.GetUserByExternalID(ctx, sqlcgen.GetUserByExternalIDParams{
		TenantID: s.tenantID, ExternalID: &externalID,
	})
}

// UpdateProvisionedUser writes the directory's view of an account.
func (s *Scoped) UpdateProvisionedUser(ctx context.Context, arg sqlcgen.UpdateProvisionedUserParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateProvisionedUser(ctx, arg)
}

// SetUserExternalID binds an account to a provisioning identifier.
func (s *Scoped) SetUserExternalID(ctx context.Context, arg sqlcgen.SetUserExternalIDParams) error {
	arg.TenantID = s.tenantID
	return s.q.SetUserExternalID(ctx, arg)
}

// --- groups ----------------------------------------------------------

// CreateGroup adds a group to this tenant.
func (s *Scoped) CreateGroup(ctx context.Context, arg sqlcgen.CreateGroupParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateGroup(ctx, arg)
}

// GetGroup returns one by id.
func (s *Scoped) GetGroup(ctx context.Context, id string) (sqlcgen.Group, error) {
	return s.q.GetGroup(ctx, sqlcgen.GetGroupParams{TenantID: s.tenantID, ID: id})
}

// GetGroupByDisplayName resolves the name a directory pushes.
func (s *Scoped) GetGroupByDisplayName(ctx context.Context, name string) (sqlcgen.Group, error) {
	return s.q.GetGroupByDisplayName(ctx,
		sqlcgen.GetGroupByDisplayNameParams{TenantID: s.tenantID, DisplayName: name})
}

// GetGroupByExternalID resolves the identifier a directory knows it by.
func (s *Scoped) GetGroupByExternalID(ctx context.Context, externalID string) (sqlcgen.Group, error) {
	return s.q.GetGroupByExternalID(ctx,
		sqlcgen.GetGroupByExternalIDParams{TenantID: s.tenantID, ExternalID: &externalID})
}

// ListGroups returns the tenant's groups by name.
func (s *Scoped) ListGroups(ctx context.Context) ([]sqlcgen.Group, error) {
	return s.q.ListGroups(ctx, s.tenantID)
}

// ListGroupsWithMemberCounts returns the console's list in one query.
func (s *Scoped) ListGroupsWithMemberCounts(ctx context.Context) ([]sqlcgen.ListGroupsWithMemberCountsRow, error) {
	return s.q.ListGroupsWithMemberCounts(ctx, s.tenantID)
}

// CountGroups returns how many the tenant has.
func (s *Scoped) CountGroups(ctx context.Context) (int64, error) {
	return s.q.CountGroups(ctx, s.tenantID)
}

// UpdateGroup changes a group's name, description, and external id.
func (s *Scoped) UpdateGroup(ctx context.Context, arg sqlcgen.UpdateGroupParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateGroup(ctx, arg)
}

// DeleteGroup removes a group and its memberships.
func (s *Scoped) DeleteGroup(ctx context.Context, id string) error {
	return s.q.DeleteGroup(ctx, sqlcgen.DeleteGroupParams{TenantID: s.tenantID, ID: id})
}

// AddGroupMember adds one account to one group, idempotently.
func (s *Scoped) AddGroupMember(ctx context.Context, groupID, userID string, at time.Time) error {
	return s.q.AddGroupMember(ctx, sqlcgen.AddGroupMemberParams{
		TenantID: s.tenantID, GroupID: groupID, UserID: userID, AddedAt: at,
	})
}

// RemoveGroupMember takes one out.
func (s *Scoped) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	return s.q.RemoveGroupMember(ctx, sqlcgen.RemoveGroupMemberParams{
		TenantID: s.tenantID, GroupID: groupID, UserID: userID,
	})
}

// RemoveAllGroupMembers empties a group, for a wholesale replacement.
func (s *Scoped) RemoveAllGroupMembers(ctx context.Context, groupID string) error {
	return s.q.RemoveAllGroupMembers(ctx,
		sqlcgen.RemoveAllGroupMembersParams{TenantID: s.tenantID, GroupID: groupID})
}

// ListGroupMembers returns a group's members with their usernames.
func (s *Scoped) ListGroupMembers(ctx context.Context, groupID string) ([]sqlcgen.ListGroupMembersRow, error) {
	return s.q.ListGroupMembers(ctx,
		sqlcgen.ListGroupMembersParams{TenantID: s.tenantID, GroupID: groupID})
}

// CountGroupMembers returns how many people are in a group.
func (s *Scoped) CountGroupMembers(ctx context.Context, groupID string) (int64, error) {
	return s.q.CountGroupMembers(ctx,
		sqlcgen.CountGroupMembersParams{TenantID: s.tenantID, GroupID: groupID})
}

// ListGroupsForUser returns the groups an account belongs to.
func (s *Scoped) ListGroupsForUser(ctx context.Context, userID string) ([]sqlcgen.ListGroupsForUserRow, error) {
	return s.q.ListGroupsForUser(ctx,
		sqlcgen.ListGroupsForUserParams{TenantID: s.tenantID, UserID: userID})
}

// --- webhooks --------------------------------------------------------

// CreateWebhookSubscription registers an outbound subscription.
func (s *Scoped) CreateWebhookSubscription(ctx context.Context, arg sqlcgen.CreateWebhookSubscriptionParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateWebhookSubscription(ctx, arg)
}

// ListWebhookSubscriptions returns all of a tenant's subscriptions.
func (s *Scoped) ListWebhookSubscriptions(ctx context.Context) ([]sqlcgen.WebhookSubscription, error) {
	return s.q.ListWebhookSubscriptions(ctx, s.tenantID)
}

// ListActiveWebhookSubscriptions returns the ones that would receive an event.
func (s *Scoped) ListActiveWebhookSubscriptions(ctx context.Context) ([]sqlcgen.WebhookSubscription, error) {
	return s.q.ListActiveWebhookSubscriptions(ctx, s.tenantID)
}

// GetWebhookSubscription returns one by id.
func (s *Scoped) GetWebhookSubscription(ctx context.Context, id string) (sqlcgen.WebhookSubscription, error) {
	return s.q.GetWebhookSubscription(ctx,
		sqlcgen.GetWebhookSubscriptionParams{TenantID: s.tenantID, ID: id})
}

// SetWebhookSubscriptionStatus pauses or resumes one.
func (s *Scoped) SetWebhookSubscriptionStatus(ctx context.Context, arg sqlcgen.SetWebhookSubscriptionStatusParams) error {
	arg.TenantID = s.tenantID
	return s.q.SetWebhookSubscriptionStatus(ctx, arg)
}

// RotateWebhookSubscriptionSecret installs a new signing key and moves the
// old one aside, to be sent alongside until it expires.
func (s *Scoped) RotateWebhookSubscriptionSecret(ctx context.Context, arg sqlcgen.RotateWebhookSubscriptionSecretParams) error {
	arg.TenantID = s.tenantID
	return s.q.RotateWebhookSubscriptionSecret(ctx, arg)
}

// DeleteWebhookSubscription removes one and its deliveries.
func (s *Scoped) DeleteWebhookSubscription(ctx context.Context, id string) error {
	return s.q.DeleteWebhookSubscription(ctx,
		sqlcgen.DeleteWebhookSubscriptionParams{TenantID: s.tenantID, ID: id})
}

// EnqueueWebhookDelivery queues one event for one subscription.
func (s *Scoped) EnqueueWebhookDelivery(ctx context.Context, arg sqlcgen.EnqueueWebhookDeliveryParams) error {
	arg.TenantID = s.tenantID
	return s.q.EnqueueWebhookDelivery(ctx, arg)
}

// CountPendingSnapshotDeliveries reports how much of a snapshot is still
// queued for one subscription.
func (s *Scoped) CountPendingSnapshotDeliveries(ctx context.Context, subscriptionID string) (int64, error) {
	return s.q.CountPendingSnapshotDeliveries(ctx, sqlcgen.CountPendingSnapshotDeliveriesParams{
		TenantID: s.tenantID, SubscriptionID: subscriptionID})
}

// ClaimDueWebhookDeliveries takes up to limit deliveries that are due.
// Must run inside a transaction: the claim is the row lock.
func (s *Scoped) ClaimDueWebhookDeliveries(ctx context.Context, q *sqlcgen.Queries, now time.Time, limit int32) ([]sqlcgen.WebhookDelivery, error) {
	return q.ClaimDueWebhookDeliveries(ctx, sqlcgen.ClaimDueWebhookDeliveriesParams{
		TenantID: s.tenantID, NextAttemptAt: &now, Limit: limit,
	})
}

// MarkWebhookDelivered records a success.
func (s *Scoped) MarkWebhookDelivered(ctx context.Context, arg sqlcgen.MarkWebhookDeliveredParams) error {
	arg.TenantID = s.tenantID
	return s.q.MarkWebhookDelivered(ctx, arg)
}

// MarkWebhookAttemptFailed records a failure and when to try again.
func (s *Scoped) MarkWebhookAttemptFailed(ctx context.Context, arg sqlcgen.MarkWebhookAttemptFailedParams) error {
	arg.TenantID = s.tenantID
	return s.q.MarkWebhookAttemptFailed(ctx, arg)
}

// ListWebhookDeliveries returns a subscription's recent attempts.
func (s *Scoped) ListWebhookDeliveries(ctx context.Context, subscriptionID string, limit int32) ([]sqlcgen.WebhookDelivery, error) {
	return s.q.ListWebhookDeliveries(ctx, sqlcgen.ListWebhookDeliveriesParams{
		TenantID: s.tenantID, SubscriptionID: subscriptionID, Limit: limit,
	})
}

// DeleteOldWebhookDeliveries clears finished deliveries past their retention.
func (s *Scoped) DeleteOldWebhookDeliveries(ctx context.Context, before time.Time) error {
	return s.q.DeleteOldWebhookDeliveries(ctx,
		sqlcgen.DeleteOldWebhookDeliveriesParams{TenantID: s.tenantID, CreatedAt: before})
}

func (s *Scoped) withTx(fn func(*sqlcgen.Queries) error) error {
	st := &Store{db: s.db, Queries: s.q}
	return st.WithTx(fn)
}

/* ------------------------------------------------------ LDAP directories */

// CreateLDAPSource registers a directory to read accounts out of.
func (s *Scoped) CreateLDAPSource(ctx context.Context, arg sqlcgen.CreateLDAPSourceParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateLDAPSource(ctx, arg)
}

// ListLDAPSources returns the tenant's directories.
func (s *Scoped) ListLDAPSources(ctx context.Context) ([]sqlcgen.LdapSource, error) {
	return s.q.ListLDAPSources(ctx, s.tenantID)
}

// GetLDAPSource returns one by id.
func (s *Scoped) GetLDAPSource(ctx context.Context, id string) (sqlcgen.LdapSource, error) {
	return s.q.GetLDAPSource(ctx, sqlcgen.GetLDAPSourceParams{TenantID: s.tenantID, ID: id})
}

// UpdateLDAPSource replaces the editable settings. The bind password is not
// among them; it has its own statement so that a form which cannot show the
// current value cannot blank it either.
func (s *Scoped) UpdateLDAPSource(ctx context.Context, arg sqlcgen.UpdateLDAPSourceParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateLDAPSource(ctx, arg)
}

// UpdateLDAPSourceBindPassword stores a freshly sealed credential.
func (s *Scoped) UpdateLDAPSourceBindPassword(ctx context.Context, id, sealed string, now time.Time) error {
	return s.q.UpdateLDAPSourceBindPassword(ctx, sqlcgen.UpdateLDAPSourceBindPasswordParams{
		TenantID: s.tenantID, ID: id, BindPassword: sealed, UpdatedAt: now,
	})
}

// UpdateLDAPSourceStatus enables or disables a directory.
func (s *Scoped) UpdateLDAPSourceStatus(ctx context.Context, id, status string, now time.Time) error {
	return s.q.UpdateLDAPSourceStatus(ctx, sqlcgen.UpdateLDAPSourceStatusParams{
		TenantID: s.tenantID, ID: id, Status: status, UpdatedAt: now,
	})
}

// MarkLDAPSourceSynced records that a run finished.
func (s *Scoped) MarkLDAPSourceSynced(ctx context.Context, id string, at time.Time) error {
	return s.q.MarkLDAPSourceSynced(ctx, sqlcgen.MarkLDAPSourceSyncedParams{
		TenantID: s.tenantID, ID: id, LastSyncedAt: &at,
	})
}

// ClaimNextDueLDAPSource takes one directory whose synchronization interval
// has elapsed and returns its id, or IsNoRows when none is due. Claiming is
// the write that records the attempt, so a second instance asking at the same
// moment gets a different directory or nothing.
//
// Scoped like everything else: the scheduler walks tenants and asks once per
// tenant, exactly as webhook delivery does, rather than sweeping the table
// across all of them.
func (s *Scoped) ClaimNextDueLDAPSource(ctx context.Context, now time.Time) (string, error) {
	return s.q.ClaimNextDueLDAPSource(ctx, sqlcgen.ClaimNextDueLDAPSourceParams{
		TenantID: s.tenantID, LastSyncAttemptAt: &now,
	})
}

// StartLDAPSyncRun opens a run record before any directory is contacted, so
// a sync that dies mid-flight leaves evidence rather than nothing.
func (s *Scoped) StartLDAPSyncRun(ctx context.Context, arg sqlcgen.StartLDAPSyncRunParams) error {
	arg.TenantID = s.tenantID
	return s.q.StartLDAPSyncRun(ctx, arg)
}

// FinishLDAPSyncRun closes it with what happened.
func (s *Scoped) FinishLDAPSyncRun(ctx context.Context, arg sqlcgen.FinishLDAPSyncRunParams) error {
	arg.TenantID = s.tenantID
	return s.q.FinishLDAPSyncRun(ctx, arg)
}

// ListLDAPSyncRuns returns a directory's recent runs, newest first.
func (s *Scoped) ListLDAPSyncRuns(ctx context.Context, sourceID string, limit int32) ([]sqlcgen.LdapSyncRun, error) {
	return s.q.ListLDAPSyncRuns(ctx, sqlcgen.ListLDAPSyncRunsParams{
		TenantID: s.tenantID, SourceID: sourceID, Limit: limit,
	})
}

// ListUsersFromLDAPSource returns every account a directory owns, which is
// what a sync compares against to work out what has vanished from it.
func (s *Scoped) ListUsersFromLDAPSource(ctx context.Context, sourceID string) ([]sqlcgen.User, error) {
	return s.q.ListUsersFromLDAPSource(ctx, sqlcgen.ListUsersFromLDAPSourceParams{
		TenantID: s.tenantID, LdapSourceID: &sourceID,
	})
}

// BindUserToLDAPSource records which directory owns an account.
func (s *Scoped) BindUserToLDAPSource(ctx context.Context, userID, sourceID string, now time.Time) error {
	return s.q.BindUserToLDAPSource(ctx, sqlcgen.BindUserToLDAPSourceParams{
		TenantID: s.tenantID, ID: userID, LdapSourceID: &sourceID, UpdatedAt: now,
	})
}

// CloseUserAccount marks an account closed by its holder: disabled, stamped,
// and every token it holds revoked, in one statement so no window exists
// where it is closed but still signed in.
func (s *Scoped) CloseUserAccount(ctx context.Context, userID string, now time.Time) error {
	return s.q.CloseUserAccount(ctx, sqlcgen.CloseUserAccountParams{
		TenantID: s.tenantID, ID: userID, Now: now,
	})
}

// ReopenUserAccount clears the closure mark, which an administrator enabling
// the account does as part of the same act.
func (s *Scoped) ReopenUserAccount(ctx context.Context, userID string, now time.Time) error {
	return s.q.ReopenUserAccount(ctx, sqlcgen.ReopenUserAccountParams{
		TenantID: s.tenantID, ID: userID, UpdatedAt: now,
	})
}

/* ---------------------------------------------- Registration verification */

// CreateRegistrationVerification records an outstanding request to prove an
// address.
func (s *Scoped) CreateRegistrationVerification(ctx context.Context, arg sqlcgen.CreateRegistrationVerificationParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateRegistrationVerification(ctx, arg)
}

// SupersedeRegistrationVerifications marks every outstanding request for an
// account used, so asking again invalidates the previous link.
func (s *Scoped) SupersedeRegistrationVerifications(ctx context.Context, userID string, now time.Time) error {
	return s.q.SupersedeRegistrationVerifications(ctx, sqlcgen.SupersedeRegistrationVerificationsParams{
		TenantID: s.tenantID, UserID: userID, UsedAt: &now,
	})
}

// ConsumeRegistrationVerification spends a token and returns it, in one
// statement. A read followed by a write would let the same link be redeemed
// twice.
func (s *Scoped) ConsumeRegistrationVerification(ctx context.Context, tokenHash string, now time.Time) (sqlcgen.RegistrationVerification, error) {
	return s.q.ConsumeRegistrationVerification(ctx, sqlcgen.ConsumeRegistrationVerificationParams{
		TenantID: s.tenantID, TokenHash: tokenHash, Now: now,
	})
}

// MarkUserVerified records that an account proved its address.
func (s *Scoped) MarkUserVerified(ctx context.Context, userID string, at time.Time) error {
	return s.q.MarkUserVerified(ctx, sqlcgen.MarkUserVerifiedParams{
		TenantID: s.tenantID, ID: userID, VerifiedAt: &at,
	})
}

// UpdateUserProfileAttributes writes the descriptive half of an account.
func (s *Scoped) UpdateUserProfileAttributes(ctx context.Context, arg sqlcgen.UpdateUserProfileAttributesParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateUserProfileAttributes(ctx, arg)
}

/* --------------------------------------------- Organization attachments */

// SetOrganizationManager nominates whoever is responsible for it, or clears
// the nomination with nil.
func (s *Scoped) SetOrganizationManager(ctx context.Context, id string, managerID *string, now time.Time) error {
	return s.q.SetOrganizationManager(ctx, sqlcgen.SetOrganizationManagerParams{
		TenantID: s.tenantID, ID: id, ManagerID: managerID, UpdatedAt: now,
	})
}

// AttachUserToOrganization adds an advisory attachment. Idempotent: asking
// twice is not an error, because a caller reconciling a list should not have
// to know what is already there.
func (s *Scoped) AttachUserToOrganization(ctx context.Context, userID, organizationID string, now time.Time) error {
	return s.q.AttachUserToOrganization(ctx, sqlcgen.AttachUserToOrganizationParams{
		TenantID: s.tenantID, UserID: userID, OrganizationID: organizationID, CreatedAt: now,
	})
}

// DetachUserFromOrganization removes one.
func (s *Scoped) DetachUserFromOrganization(ctx context.Context, userID, organizationID string) error {
	return s.q.DetachUserFromOrganization(ctx, sqlcgen.DetachUserFromOrganizationParams{
		TenantID: s.tenantID, UserID: userID, OrganizationID: organizationID,
	})
}

// ListUserOrganizationAttachments returns the organizations a person is
// attached to, not counting the one they primarily belong to.
func (s *Scoped) ListUserOrganizationAttachments(ctx context.Context, userID string) ([]sqlcgen.Organization, error) {
	return s.q.ListUserOrganizationAttachments(ctx, sqlcgen.ListUserOrganizationAttachmentsParams{
		TenantID: s.tenantID, UserID: userID,
	})
}

// ListOrganizationAttachedUsers returns the people attached to one.
func (s *Scoped) ListOrganizationAttachedUsers(ctx context.Context, organizationID string) ([]sqlcgen.User, error) {
	return s.q.ListOrganizationAttachedUsers(ctx, sqlcgen.ListOrganizationAttachedUsersParams{
		TenantID: s.tenantID, OrganizationID: organizationID,
	})
}

// --- per-recipient field mappings ---------------------------------------

// RecipientRef names one thing that receives fields, without saying what kind
// it is. Exactly one field is set, which the schema also enforces: four
// nullable foreign keys and a CHECK, so that the database keeps the reference
// honest and takes the mappings away with the recipient.
//
// Not "application": three of the four are, and a webhook subscription is not.
// It is the one Portico pushes to rather than answers, and it is what an
// administrator usually means by synchronising to a downstream system.
type RecipientRef struct {
	OAuthClientID         string
	SAMLSPID              string
	CASServiceID          string
	WebhookSubscriptionID string
}

func (r RecipientRef) ids() (oauth, saml, cas, hook *string) {
	if r.OAuthClientID != "" {
		oauth = &r.OAuthClientID
	}
	if r.SAMLSPID != "" {
		saml = &r.SAMLSPID
	}
	if r.CASServiceID != "" {
		cas = &r.CASServiceID
	}
	if r.WebhookSubscriptionID != "" {
		hook = &r.WebhookSubscriptionID
	}
	return oauth, saml, cas, hook
}

// ListFieldMappings returns one recipient's mappings.
func (s *Scoped) ListFieldMappings(ctx context.Context, ref RecipientRef) ([]sqlcgen.FieldMapping, error) {
	oauth, saml, cas, hook := ref.ids()
	return s.q.ListFieldMappings(ctx, sqlcgen.ListFieldMappingsParams{
		TenantID: s.tenantID, OauthClientID: oauth, SamlSpID: saml,
		CasServiceID: cas, WebhookSubscriptionID: hook,
	})
}

// ListRecipientsMappingField answers which recipients receive one fact, across
// all four kinds at once.
func (s *Scoped) ListRecipientsMappingField(ctx context.Context, sourceKey string) ([]sqlcgen.FieldMapping, error) {
	return s.q.ListRecipientsMappingField(ctx,
		sqlcgen.ListRecipientsMappingFieldParams{TenantID: s.tenantID, SourceKey: sourceKey})
}

// DeleteFieldMappings clears one recipient's set, which is what a save does
// before writing the set the form holds.
func (s *Scoped) DeleteFieldMappings(ctx context.Context, ref RecipientRef) error {
	oauth, saml, cas, hook := ref.ids()
	return s.q.DeleteFieldMappings(ctx, sqlcgen.DeleteFieldMappingsParams{
		TenantID: s.tenantID, OauthClientID: oauth, SamlSpID: saml,
		CasServiceID: cas, WebhookSubscriptionID: hook,
	})
}

// CreateFieldMapping writes one row.
func (s *Scoped) CreateFieldMapping(ctx context.Context, arg sqlcgen.CreateFieldMappingParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateFieldMapping(ctx, arg)
}

// --- tenant-defined user attributes -----------------------------------

// ListUserAttributeDefinitions returns the tenant's own attribute definitions,
// disabled ones included: the console shows those so they can be enabled
// again, and the field catalogue marks them rather than hiding them.
func (s *Scoped) ListUserAttributeDefinitions(ctx context.Context) ([]sqlcgen.UserAttributeDefinition, error) {
	return s.q.ListUserAttributeDefinitions(ctx, s.tenantID)
}

// GetUserAttributeDefinition returns one by id.
func (s *Scoped) GetUserAttributeDefinition(ctx context.Context, id string) (sqlcgen.UserAttributeDefinition, error) {
	return s.q.GetUserAttributeDefinition(ctx,
		sqlcgen.GetUserAttributeDefinitionParams{TenantID: s.tenantID, ID: id})
}

// GetUserAttributeDefinitionByKey returns one by the key a mapping stores.
func (s *Scoped) GetUserAttributeDefinitionByKey(ctx context.Context, key string) (sqlcgen.UserAttributeDefinition, error) {
	return s.q.GetUserAttributeDefinitionByKey(ctx,
		sqlcgen.GetUserAttributeDefinitionByKeyParams{TenantID: s.tenantID, Key: key})
}

// CountUserAttributeDefinitions is for the per-tenant bound.
func (s *Scoped) CountUserAttributeDefinitions(ctx context.Context) (int64, error) {
	return s.q.CountUserAttributeDefinitions(ctx, s.tenantID)
}

// CreateUserAttributeDefinition adds one.
func (s *Scoped) CreateUserAttributeDefinition(ctx context.Context, arg sqlcgen.CreateUserAttributeDefinitionParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateUserAttributeDefinition(ctx, arg)
}

// UpdateUserAttributeDefinition changes the editable parts. The key is not
// among them; it is what a mapping stores.
func (s *Scoped) UpdateUserAttributeDefinition(ctx context.Context, arg sqlcgen.UpdateUserAttributeDefinitionParams) error {
	arg.TenantID = s.tenantID
	return s.q.UpdateUserAttributeDefinition(ctx, arg)
}

// UpdateUserAttributeDefinitionStatus retires one or brings it back, keeping
// every value already recorded against it.
func (s *Scoped) UpdateUserAttributeDefinitionStatus(ctx context.Context, id, status string, now time.Time) error {
	return s.q.UpdateUserAttributeDefinitionStatus(ctx,
		sqlcgen.UpdateUserAttributeDefinitionStatusParams{
			TenantID: s.tenantID, ID: id, Status: status, UpdatedAt: now,
		})
}

// DeleteUserAttributeDefinition removes one and, by the cascade, every value
// recorded against it.
func (s *Scoped) DeleteUserAttributeDefinition(ctx context.Context, id string) error {
	return s.q.DeleteUserAttributeDefinition(ctx,
		sqlcgen.DeleteUserAttributeDefinitionParams{TenantID: s.tenantID, ID: id})
}

// ListUserAttributeValues returns one account's custom values with the key and
// kind of each, so a caller needs no second query to render or send them.
func (s *Scoped) ListUserAttributeValues(ctx context.Context, userID string) ([]sqlcgen.ListUserAttributeValuesRow, error) {
	return s.q.ListUserAttributeValues(ctx,
		sqlcgen.ListUserAttributeValuesParams{TenantID: s.tenantID, UserID: userID})
}

// SetUserAttributeValue writes one, replacing whatever was there.
func (s *Scoped) SetUserAttributeValue(ctx context.Context, arg sqlcgen.SetUserAttributeValueParams) error {
	arg.TenantID = s.tenantID
	return s.q.SetUserAttributeValue(ctx, arg)
}

// DeleteUserAttributeValue clears one. It removes the row rather than storing
// an empty string, so "never filled in" and "deliberately blank" stay
// distinguishable.
func (s *Scoped) DeleteUserAttributeValue(ctx context.Context, userID, definitionID string) error {
	return s.q.DeleteUserAttributeValue(ctx,
		sqlcgen.DeleteUserAttributeValueParams{
			TenantID: s.tenantID, UserID: userID, DefinitionID: definitionID,
		})
}

// CountUserAttributeValues answers "who has filled this in".
func (s *Scoped) CountUserAttributeValues(ctx context.Context, definitionID string) (int64, error) {
	return s.q.CountUserAttributeValues(ctx,
		sqlcgen.CountUserAttributeValuesParams{TenantID: s.tenantID, DefinitionID: definitionID})
}

// --- application logos -----------------------------------------------

// CreateApplicationLogo stores an uploaded picture.
func (s *Scoped) CreateApplicationLogo(ctx context.Context, arg sqlcgen.CreateApplicationLogoParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateApplicationLogo(ctx, arg)
}

// GetApplicationLogo returns one of this tenant's logos.
//
// Scoped like everything else here, which is what the tenant in the request
// path is for: the picture is served without credentials — a tile is drawn on
// the sign-in screen, before anybody has any — so the tenant cannot come from a
// principal, and an unscoped lookup by id would be a query that could have
// taken a tenant and did not. The path supplies it and this filters on it.
func (s *Scoped) GetApplicationLogo(ctx context.Context, id string) (sqlcgen.ApplicationLogo, error) {
	return s.q.GetApplicationLogo(ctx,
		sqlcgen.GetApplicationLogoParams{TenantID: s.tenantID, ID: id})
}

// DeleteOrphanedApplicationLogos removes uploads that nothing references and
// that are older than the cutoff. It reports how many went.
func (s *Scoped) DeleteOrphanedApplicationLogos(ctx context.Context, uploadedBefore time.Time) (int64, error) {
	return s.q.DeleteOrphanedApplicationLogos(ctx,
		sqlcgen.DeleteOrphanedApplicationLogosParams{
			TenantID: s.tenantID, CreatedAt: uploadedBefore,
		})
}
