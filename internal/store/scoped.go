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

func (s *Scoped) withTx(fn func(*sqlcgen.Queries) error) error {
	st := &Store{db: s.db, Queries: s.q}
	return st.WithTx(fn)
}
