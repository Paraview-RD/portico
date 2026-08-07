-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = $1 LIMIT 1;

-- name: GetOrganizationByCode :one
SELECT * FROM organizations WHERE code = $1 LIMIT 1;

-- name: CreateOrganization :exec
INSERT INTO organizations (
    id, name, code, remark, status, sort_order, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateOrganization :exec
UPDATE organizations
SET name = $1,
    remark = $2,
    sort_order = $3,
    updated_at = $4
WHERE id = $5;

-- name: UpdateOrganizationStatus :exec
UPDATE organizations SET status = $1, updated_at = $2 WHERE id = $3;

-- name: ListOrganizations :many
SELECT * FROM organizations ORDER BY sort_order, created_at;

-- name: ListActiveOrganizations :many
SELECT * FROM organizations WHERE status = 'ACTIVE' ORDER BY sort_order, created_at;

-- name: ListOrganizationsByIDs :many
SELECT * FROM organizations WHERE id = ANY($1::text[]);

-- name: CountOrganizations :one
SELECT COUNT(*) FROM organizations;
