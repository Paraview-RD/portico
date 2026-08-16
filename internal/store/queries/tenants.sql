-- Tenants are the root of the isolation hierarchy, so unlike every other
-- table these queries are not scoped by tenant_id — there is nothing above a
-- tenant to scope them to.
--
-- Everything here except TenantOverview is reachable only from the
-- provisioning CLI and from sign-in's tenant resolution. TenantOverview is
-- the single exception and the only query in this system an authenticated
-- request can use to learn that another tenant exists; what stands in front
-- of it is a route that is not registered unless PORTICO_TENANT_CONSOLE is
-- on, and then only for an administrator of the default tenant. See
-- internal/server/routes.go and docs/access-guide.md.

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1 LIMIT 1;

-- name: GetTenantByCode :one
SELECT * FROM tenants WHERE code = $1 LIMIT 1;

-- name: CreateTenant :exec
INSERT INTO tenants (id, code, name, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListTenants :many
SELECT * FROM tenants ORDER BY code;

-- name: UpdateTenantStatus :exec
UPDATE tenants SET status = $1, updated_at = $2 WHERE id = $3;

-- name: TenantOverview :many
-- Every tenant with a count of what is inside it, for the operator console.
--
-- The counts are correlated subqueries rather than a join with GROUP BY so
-- that a tenant holding nothing still appears with zeros — a tenant somebody
-- created and never used is exactly the row an operator is looking for.
--
-- Note what this is and is not. Every read of a tenant-scoped table here
-- carries a tenant_id predicate, as the isolation guard demands; what is new
-- is that the predicate iterates over the tenants rather than being fixed to
-- the caller's own. So no row of another tenant's data leaves — only how many
-- of them there are. That distinction is the whole design of this feature,
-- and it is why nothing below selects a name, an address, or an identifier
-- belonging to a tenant other than through the tenants table itself.
--
-- An application is counted across the three kinds the console registers, so
-- the number agrees with what that tenant's own Applications page shows.
SELECT
    t.id,
    t.code,
    t.name,
    t.status,
    t.created_at,
    (SELECT count(*) FROM users u
        WHERE u.tenant_id = t.id) AS user_count,
    (SELECT count(*) FROM users u
        WHERE u.tenant_id = t.id AND u.status = 'ACTIVE') AS active_user_count,
    (SELECT count(*) FROM organizations o
        WHERE o.tenant_id = t.id) AS organization_count,
    ((SELECT count(*) FROM oauth_clients c
        WHERE c.tenant_id = t.id)
      + (SELECT count(*) FROM saml_service_providers s
        WHERE s.tenant_id = t.id)
      + (SELECT count(*) FROM cas_services v
        WHERE v.tenant_id = t.id))::bigint AS application_count,
    -- When anything last happened in there, which is what separates a tenant
    -- in use from one that was created and forgotten. An index-only read:
    -- idx_audit_logs_created is (tenant_id, created_at DESC).
    (SELECT max(a.created_at) FROM audit_logs a
        WHERE a.tenant_id = t.id) AS last_activity
FROM tenants t
ORDER BY t.code;
