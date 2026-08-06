-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = ? LIMIT 1;

-- name: GetOrganizationByCode :one
SELECT * FROM organizations WHERE code = ? LIMIT 1;

-- name: CreateOrganization :exec
INSERT INTO organizations (
    id, name, code, remark, status, sort_order, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateOrganization :exec
UPDATE organizations
SET name = ?,
    remark = ?,
    sort_order = ?,
    updated_at = ?
WHERE id = ?;

-- name: UpdateOrganizationStatus :exec
UPDATE organizations SET status = ?, updated_at = ? WHERE id = ?;

-- name: ListOrganizations :many
SELECT * FROM organizations ORDER BY sort_order, created_at;

-- name: ListActiveOrganizations :many
SELECT * FROM organizations WHERE status = 'ACTIVE' ORDER BY sort_order, created_at;

-- name: ListOrganizationsByIDs :many
SELECT * FROM organizations WHERE id IN (sqlc.slice('ids'));

-- name: CountOrganizations :one
SELECT COUNT(*) FROM organizations;
