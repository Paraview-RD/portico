-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- name: GetOrganizationByCode :one
SELECT * FROM organizations WHERE tenant_id = $1 AND code = $2 LIMIT 1;

-- name: CreateOrganization :exec
INSERT INTO organizations (
    id, tenant_id, name, code, remark, parent_id, status, sort_order,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UpdateOrganization :exec
UPDATE organizations
SET name = $1,
    remark = $2,
    parent_id = $3,
    sort_order = $4,
    updated_at = $5
WHERE tenant_id = $6 AND id = $7;

-- name: UpdateOrganizationStatus :exec
UPDATE organizations
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: ListOrganizations :many
SELECT * FROM organizations WHERE tenant_id = $1 ORDER BY sort_order, created_at;

-- name: ListActiveOrganizations :many
SELECT * FROM organizations
WHERE tenant_id = $1 AND status = 'ACTIVE'
ORDER BY sort_order, created_at;

-- name: ListOrganizationsByIDs :many
SELECT * FROM organizations WHERE tenant_id = $1 AND id = ANY($2::text[]);

