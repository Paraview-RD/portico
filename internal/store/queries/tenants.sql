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
INSERT INTO tenants (id, code, name, status, created_at, updated_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListTenants :many
SELECT * FROM tenants ORDER BY code;

-- name: UpdateTenantStatus :exec
UPDATE tenants SET status = $1, updated_at = $2 WHERE id = $3;

-- name: SetTenantExpiry :exec
-- Moves a deadline, or removes one with NULL.
UPDATE tenants SET expires_at = $1, updated_at = $2 WHERE id = $3;

-- name: ListTenantsToDisable :many
-- Tenants whose deadline has passed while they are still able to sign in.
--
-- Status is part of the predicate so the sweep is idempotent: once disabled, a
-- tenant stops appearing here and is not written to on every pass.
SELECT * FROM tenants
WHERE expires_at IS NOT NULL AND expires_at <= $1 AND status = 'ACTIVE'
ORDER BY expires_at;

-- name: ListTenantsToDelete :many
-- Tenants whose grace period has also passed.
--
-- Separate from the query above rather than one query with two cases, because
-- these two do very different things: one is reversible and one is not. A
-- caller has to ask for the destructive list by name.
SELECT * FROM tenants
WHERE expires_at IS NOT NULL AND expires_at <= $1
ORDER BY expires_at;

-- name: DeleteTenant :exec
-- Everything scoped to this tenant goes with it, by ON DELETE CASCADE.
DELETE FROM tenants WHERE id = $1;

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
    -- The deadline, so the console can show how long a trial tenant has left
    -- without a second query. NULL for every tenant nobody gave one to.
    t.expires_at,
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
