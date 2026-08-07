-- Tenants are the root of the isolation hierarchy, so unlike every other
-- table these queries are not scoped by tenant_id — there is nothing above a
-- tenant to scope them to. They are reachable only from the provisioning CLI
-- and from sign-in's tenant resolution, never from an authenticated request
-- handler.

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
