-- name: CreateAuditLog :exec
INSERT INTO audit_logs (
    id, tenant_id, kind, action, actor_id, actor_username,
    target_type, target_id, target_name,
    result, detail, ip, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: DeleteAuditLogsBefore :exec
-- Removes entries older than a cutoff.
--
-- Only ever called when a tenant has configured a retention period. There is
-- no default cutoff and no automatic one: an audit trail that quietly
-- shortens itself is doing the worst thing an audit trail can do, so the
-- decision belongs to whoever runs the deployment and has to be made
-- explicitly.
DELETE FROM audit_logs WHERE tenant_id = $1 AND created_at < $2;
