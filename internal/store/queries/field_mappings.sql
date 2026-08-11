-- name: ListApplicationFieldMappings :many
-- One application's mappings. The caller passes the id of whichever kind of
-- application it has and nulls for the other two; IS NOT DISTINCT FROM matches
-- the null columns, and the CHECK constraint guarantees that identifies exactly
-- one application rather than a set.
SELECT * FROM application_field_mappings
WHERE tenant_id = $1
  AND oauth_client_id IS NOT DISTINCT FROM $2
  AND saml_sp_id IS NOT DISTINCT FROM $3
  AND cas_service_id IS NOT DISTINCT FROM $4
ORDER BY source_key;

-- name: ListApplicationsMappingField :many
-- Which applications receive one fact, across all three protocols at once.
-- This is the disclosure question — "who gets department" — and answering it in
-- one query is most of why the three protocols share a table.
SELECT * FROM application_field_mappings
WHERE tenant_id = $1 AND source_key = $2 AND suppressed = FALSE;

-- name: DeleteApplicationFieldMappings :exec
-- Clears one application's set. A save replaces the whole set rather than
-- upserting row by row, because that is what the form is: a table somebody
-- edited. Row-by-row would need three different ON CONFLICT targets and would
-- leave whatever the form deleted still in place.
DELETE FROM application_field_mappings
WHERE tenant_id = $1
  AND oauth_client_id IS NOT DISTINCT FROM $2
  AND saml_sp_id IS NOT DISTINCT FROM $3
  AND cas_service_id IS NOT DISTINCT FROM $4;

-- name: CreateApplicationFieldMapping :exec
INSERT INTO application_field_mappings (
    id, tenant_id, oauth_client_id, saml_sp_id, cas_service_id,
    source_key, target_name, friendly_name, suppressed, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
