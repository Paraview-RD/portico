-- name: CreateAuditLog :exec
INSERT INTO audit_logs (
    id, tenant_id, kind, action, actor_id, actor_username,
    target_type, target_id, target_name,
    result, detail, ip, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);
