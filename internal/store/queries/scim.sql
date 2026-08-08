-- name: GetSCIMCredentialByTokenHash :one
-- Not tenant-scoped, and cannot be: this is the query that determines the
-- tenant. A SCIM client presents a bearer token and nothing else — no
-- session, no tenant header, no principal — so the credential row is the
-- only thing that says which tenant the request acts in.
--
-- Listed in internal/store/tenancy_guard_test.go as the second and only
-- other unscoped query, beside GetUserForAuthentication, for the same
-- reason: a filter here would have to be given the answer it is looking for.
--
-- Disabled credentials are returned rather than filtered out, so the caller
-- can distinguish "no such token" from "this integration was switched off"
-- and say so — the second is worth a distinct answer to an operator staring
-- at a directory that has stopped syncing.
SELECT id, tenant_id, name, token_hash, token_prefix, status,
       last_used_at, created_at, updated_at
FROM scim_credentials
WHERE token_hash = $1
LIMIT 1;

-- name: TouchSCIMCredential :exec
UPDATE scim_credentials
SET last_used_at = $2
WHERE tenant_id = $3 AND id = $1;

-- name: CreateSCIMCredential :exec
INSERT INTO scim_credentials (
    id, tenant_id, name, token_hash, token_prefix, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, $6);

-- name: ListSCIMCredentials :many
SELECT id, tenant_id, name, token_hash, token_prefix, status,
       last_used_at, created_at, updated_at
FROM scim_credentials
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: SetSCIMCredentialStatus :exec
UPDATE scim_credentials
SET status = $2, updated_at = $3
WHERE tenant_id = $4 AND id = $1;

-- name: DeleteSCIMCredential :exec
-- Deleted rather than disabled, unlike accounts and organizations. The rule
-- there exists so the audit trail keeps naming the thing it refers to; a
-- credential is named in the trail by the events it caused, which are
-- unaffected by the row going away. Keeping revoked credentials forever
-- would instead leave a list an operator has to read past to find the live
-- ones.
DELETE FROM scim_credentials
WHERE tenant_id = $2 AND id = $1;

-- name: GetUserByExternalID :one
SELECT * FROM users
WHERE tenant_id = $1 AND external_id = $2
LIMIT 1;

-- name: SetUserExternalID :exec
UPDATE users
SET external_id = $2, updated_at = $3
WHERE tenant_id = $4 AND id = $1;

-- name: UpdateProvisionedUser :exec
-- The directory's view of an account, written as one statement.
--
-- Separate from UpdateUserProfile because it writes the username, which that
-- one deliberately does not: in the console a rename is an administrator's
-- decision about somebody else's account. Here the directory is the system of
-- record — it is what the externalId points back to — and refusing its rename
-- would leave the two permanently disagreeing, with every subsequent sync
-- trying the same change again.
--
-- Role and organization are absent, and that is the boundary: a directory may
-- say who somebody is, and may not say what they may do. SCIM has no notion
-- of Portico's roles, so a mapping would have to be invented, and an invented
-- one is a way to grant administrator by writing an attribute.
UPDATE users
SET username = $1,
    display_name = $2,
    phone = $3,
    email = $4,
    status = $5,
    external_id = $6,
    updated_at = $7
WHERE tenant_id = $8 AND id = $9;
