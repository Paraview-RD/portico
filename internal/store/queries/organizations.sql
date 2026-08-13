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


-- name: SetOrganizationManager :exec
UPDATE organizations
SET manager_id = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: AttachUserToOrganization :exec
INSERT INTO user_organization_attachments (tenant_id, user_id, organization_id, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, user_id, organization_id) DO NOTHING;

-- name: DetachUserFromOrganization :exec
DELETE FROM user_organization_attachments
WHERE tenant_id = $1 AND user_id = $2 AND organization_id = $3;

-- name: ListUserOrganizationAttachments :many
SELECT o.* FROM organizations o
JOIN user_organization_attachments a
  ON a.tenant_id = o.tenant_id AND a.organization_id = o.id
WHERE a.tenant_id = $1 AND a.user_id = $2
ORDER BY o.sort_order, o.created_at;

-- name: ListOrganizationAttachedUsers :many
SELECT u.* FROM users u
JOIN user_organization_attachments a
  ON a.tenant_id = u.tenant_id AND a.user_id = u.id
WHERE a.tenant_id = $1 AND a.organization_id = $2
ORDER BY u.display_name;

-- name: AssignOrganizationAdministrator :exec
-- Records who would administer an organization once delegated
-- administration exists. It grants nothing today; see the migration.
INSERT INTO organization_administrators (
    tenant_id, organization_id, user_id, scope, granted_by, granted_at
) VALUES ($1, $2, $3, $4, $5, $6);

-- name: RevokeOrganizationAdministrator :exec
DELETE FROM organization_administrators
WHERE tenant_id = $1 AND organization_id = $2 AND user_id = $3;

-- name: GetOrganizationAdministrator :one
SELECT * FROM organization_administrators
WHERE tenant_id = $1 AND organization_id = $2 AND user_id = $3;

-- name: ListOrganizationAdministrators :many
-- The account with the assignment, so a caller can show a name and whether
-- the person is still usable. Disabled accounts are listed rather than
-- filtered: an assignment that disappeared when somebody was suspended would
-- come back on its own when they were reinstated, and nobody would have
-- decided either.
SELECT sqlc.embed(u), a.scope, a.granted_by, a.granted_at
FROM organization_administrators a
JOIN users u ON u.tenant_id = a.tenant_id AND u.id = a.user_id
WHERE a.tenant_id = $1 AND a.organization_id = $2
ORDER BY u.display_name;

-- name: ListOrganizationsAdministeredBy :many
-- The query delegated administration will make on every request. Nothing
-- consumes it for a decision yet; the console shows it on an account.
SELECT sqlc.embed(o), a.scope, a.granted_at
FROM organization_administrators a
JOIN organizations o ON o.tenant_id = a.tenant_id AND o.id = a.organization_id
WHERE a.tenant_id = $1 AND a.user_id = $2
ORDER BY o.sort_order, o.created_at;
