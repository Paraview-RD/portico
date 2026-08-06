-- name: GetUserByID :one
SELECT * FROM users WHERE id = ? LIMIT 1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ? LIMIT 1;

-- name: CreateUser :exec
INSERT INTO users (
    id, username, display_name, password_hash, phone, email,
    role, status, organization_id, token_version, source,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = ?,
    phone = ?,
    email = ?,
    organization_id = ?,
    role = ?,
    updated_at = ?
WHERE id = ?;

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
SET password_hash = ?,
    token_version = token_version + 1,
    updated_at = ?
WHERE id = ?;

-- name: BumpUserTokenVersion :exec
UPDATE users
SET token_version = token_version + 1,
    updated_at = ?
WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountUsersByRole :one
SELECT COUNT(*) FROM users WHERE role = ?;

-- name: CountUsersByOrganization :one
SELECT COUNT(*) FROM users WHERE organization_id = ?;

-- name: ClearUsersOrganization :exec
UPDATE users SET organization_id = NULL, updated_at = ? WHERE organization_id = ?;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id IN (sqlc.slice('ids'));
