-- name: GetSetting :one
SELECT * FROM system_settings WHERE key = $1 LIMIT 1;

-- name: ListSettings :many
SELECT * FROM system_settings ORDER BY key;

-- name: UpsertSetting :exec
INSERT INTO system_settings (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;
