-- name: ListUserAttributeDefinitions :many
-- The tenant's own attribute definitions, in the order a form draws them.
-- Disabled ones are included: the console shows them so they can be enabled
-- again, and the catalogue marks them rather than hiding them.
SELECT * FROM user_attribute_definitions
WHERE tenant_id = $1
ORDER BY sort_order, key;

-- name: GetUserAttributeDefinition :one
SELECT * FROM user_attribute_definitions
WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- name: GetUserAttributeDefinitionByKey :one
SELECT * FROM user_attribute_definitions
WHERE tenant_id = $1 AND key = $2 LIMIT 1;

-- name: CountUserAttributeDefinitions :one
-- For the per-tenant bound, which exists because every definition is a
-- candidate for outbound mapping and a mapped attribute is bytes in a token.
SELECT count(*) FROM user_attribute_definitions WHERE tenant_id = $1;

-- name: CreateUserAttributeDefinition :exec
INSERT INTO user_attribute_definitions (
    id, tenant_id, key, label, description, kind, allowed_values,
    required, sort_order, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- The key is not in the SET list. It is what a mapping stores, so renaming it
-- would silently stop a mapping that names it — in a system Portico does not
-- own and cannot warn.
-- name: UpdateUserAttributeDefinition :exec
UPDATE user_attribute_definitions
SET label = $1,
    description = $2,
    kind = $3,
    allowed_values = $4,
    required = $5,
    sort_order = $6,
    updated_at = $7
WHERE tenant_id = $8 AND id = $9;

-- name: UpdateUserAttributeDefinitionStatus :exec
UPDATE user_attribute_definitions
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: DeleteUserAttributeDefinition :exec
-- Discards every value recorded against it, by the cascade. Disabling is the
-- ordinary way to retire one; this is the other way, and the console says so.
DELETE FROM user_attribute_definitions WHERE tenant_id = $1 AND id = $2;

-- name: ListUserAttributeValues :many
-- One account's custom values, joined to their definitions so a caller has
-- the key and the kind without a second query. Disabled definitions are left
-- out: a value that is not being sent and not being shown is not part of the
-- account as anybody sees it.
SELECT v.definition_id, d.key, d.kind, v.value
FROM user_attribute_values v
JOIN user_attribute_definitions d ON d.id = v.definition_id
WHERE v.tenant_id = $1 AND v.user_id = $2 AND d.status = 'ACTIVE'
ORDER BY d.sort_order, d.key;

-- name: SetUserAttributeValue :exec
INSERT INTO user_attribute_values (
    tenant_id, user_id, definition_id, value, updated_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, definition_id)
DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;

-- name: DeleteUserAttributeValue :exec
-- Clearing a value removes the row rather than storing an empty string, so
-- that "never filled in" and "deliberately blank" do not become the same
-- state — the outbound rule is that nothing is sent empty, and a row holding
-- an empty string would be a value that is silently never sent.
DELETE FROM user_attribute_values
WHERE tenant_id = $1 AND user_id = $2 AND definition_id = $3;

-- name: CountUserAttributeValues :one
-- Answers "who has filled this in", asked before retiring or mapping one.
SELECT count(*) FROM user_attribute_values
WHERE tenant_id = $1 AND definition_id = $2;
