package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/paraview/portico/internal/store/sqlcgen"
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
// (tenants themselves) and for authentication, which runs before the tenant
// is known. Those are the only legitimate uses, and the guard test enforces
// that the second has exactly one member.
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

// ListPublishedSigningKeys returns everything the JWKS advertises.
func (s *Scoped) ListPublishedSigningKeys(ctx context.Context) ([]sqlcgen.OauthSigningKey, error) {
	return s.q.ListPublishedSigningKeys(ctx, s.tenantID)
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

// CompleteSAMLAuthRequest records who signed in.
func (s *Scoped) CompleteSAMLAuthRequest(ctx context.Context, id, subject string) error {
	return s.q.CompleteSAMLAuthRequest(ctx,
		sqlcgen.CompleteSAMLAuthRequestParams{TenantID: s.tenantID, ID: id, Subject: &subject})
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

func (s *Scoped) withTx(fn func(*sqlcgen.Queries) error) error {
	st := &Store{db: s.db, Queries: s.q}
	return st.WithTx(fn)
}
