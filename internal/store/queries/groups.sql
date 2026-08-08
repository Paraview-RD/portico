-- name: CreateGroup :exec
INSERT INTO groups (
    id, tenant_id, display_name, description, external_id, source,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $7);

-- name: GetGroup :one
SELECT * FROM groups WHERE tenant_id = $1 AND id = $2;

-- name: GetGroupByDisplayName :one
SELECT * FROM groups WHERE tenant_id = $1 AND display_name = $2;

-- name: GetGroupByExternalID :one
SELECT * FROM groups WHERE tenant_id = $1 AND external_id = $2;

-- name: ListGroups :many
SELECT * FROM groups WHERE tenant_id = $1 ORDER BY display_name;

-- name: CountGroups :one
SELECT COUNT(*) FROM groups WHERE tenant_id = $1;

-- name: UpdateGroup :exec
UPDATE groups
SET display_name = $1, description = $2, external_id = $3, updated_at = $4
WHERE tenant_id = $5 AND id = $6;

-- name: DeleteGroup :exec
-- Deleted rather than disabled, unlike accounts and organizations.
--
-- A group is a set, not a party the audit trail refers to: entries name the
-- people and the actor, and a removed group leaves those readable. Keeping
-- deleted groups would instead leave a list an administrator has to read
-- past, and a directory that deletes and recreates one — which they do —
-- would accumulate them.
DELETE FROM groups WHERE tenant_id = $1 AND id = $2;

-- name: AddGroupMember :exec
-- Idempotent: a directory re-sending a membership it already pushed is
-- normal, and failing it would turn an ordinary reconciliation into an
-- error the operator has to interpret.
INSERT INTO group_members (tenant_id, group_id, user_id, added_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :exec
DELETE FROM group_members
WHERE tenant_id = $1 AND group_id = $2 AND user_id = $3;

-- name: RemoveAllGroupMembers :exec
-- For PATCH replace, which hands over the whole membership at once.
DELETE FROM group_members WHERE tenant_id = $1 AND group_id = $2;

-- name: ListGroupMembers :many
-- The username comes along because SCIM's member representation carries a
-- display value, and fetching it per member would be a query per row.
SELECT m.user_id, u.username, u.display_name
FROM group_members m
JOIN users u ON u.tenant_id = m.tenant_id AND u.id = m.user_id
WHERE m.tenant_id = $1 AND m.group_id = $2
ORDER BY u.username;

-- name: ListGroupsForUser :many
SELECT g.id, g.display_name
FROM group_members m
JOIN groups g ON g.tenant_id = m.tenant_id AND g.id = m.group_id
WHERE m.tenant_id = $1 AND m.user_id = $2
ORDER BY g.display_name;

-- name: CountGroupMembers :one
SELECT COUNT(*) FROM group_members WHERE tenant_id = $1 AND group_id = $2;

-- name: ListGroupsWithMemberCounts :many
-- One query for the console's list, rather than a count per group.
SELECT g.*, COUNT(m.user_id) AS member_count
FROM groups g
LEFT JOIN group_members m ON m.tenant_id = g.tenant_id AND m.group_id = g.id
WHERE g.tenant_id = $1
GROUP BY g.id
ORDER BY g.display_name;
