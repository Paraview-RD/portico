-- name: CreateAuditLog :exec
INSERT INTO audit_logs (
    id, kind, action, actor_id, actor_username,
    target_type, target_id, target_name,
    result, detail, ip, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAuditLogByID :one
SELECT * FROM audit_logs WHERE id = ? LIMIT 1;
