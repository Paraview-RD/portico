-- name: GetSetting :one
SELECT * FROM system_settings WHERE tenant_id = $1 AND key = $2 LIMIT 1;

-- name: ListSettings :many
SELECT * FROM system_settings WHERE tenant_id = $1 ORDER BY key;

-- name: UpsertSetting :exec
INSERT INTO system_settings (tenant_id, key, value, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, key) DO UPDATE
SET value = excluded.value, updated_at = excluded.updated_at;
