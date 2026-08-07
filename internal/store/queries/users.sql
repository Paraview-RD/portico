-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1 LIMIT 1;

-- name: CreateUser :exec
INSERT INTO users (
    id, username, display_name, password_hash, phone, email,
    role, status, organization_id, token_version, source,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = $1,
    phone = $2,
    email = $3,
    organization_id = $4,
    role = $5,
    updated_at = $6
WHERE id = $7;

-- name: UpdateUserStatus :exec
-- Disabling bumps token_version so any live session stops working at once.
UPDATE users
SET status = sqlc.arg(status),
    token_version = CASE WHEN sqlc.arg(status) = 'DISABLED'
                         THEN token_version + 1
                         ELSE token_version END,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: UpdateUserPassword :exec
-- Changing a password invalidates every token issued before it.
UPDATE users
SET password_hash = $1,
    token_version = token_version + 1,
    updated_at = $2
WHERE id = $3;

-- name: BumpUserTokenVersion :exec
UPDATE users
SET token_version = token_version + 1,
    updated_at = $1
WHERE id = $2;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountUsersByRole :one
SELECT COUNT(*) FROM users WHERE role = $1;

-- name: CountUsersByOrganization :one
SELECT COUNT(*) FROM users WHERE organization_id = $1;

-- name: ClearUsersOrganization :exec
UPDATE users SET organization_id = NULL, updated_at = $1 WHERE organization_id = $2;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id = ANY($1::text[]);
