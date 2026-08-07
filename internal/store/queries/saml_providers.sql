-- name: CreateSAMLServiceProvider :exec
INSERT INTO saml_service_providers (
    id, tenant_id, entity_id, name, metadata_xml, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSAMLServiceProvider :one
SELECT * FROM saml_service_providers
WHERE tenant_id = $1 AND entity_id = $2
LIMIT 1;

-- name: ListSAMLServiceProviders :many
SELECT * FROM saml_service_providers
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: UpdateSAMLServiceProviderStatus :exec
UPDATE saml_service_providers
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND entity_id = $4;

-- name: CreateSAMLAuthRequest :exec
INSERT INTO saml_auth_requests (
    id, tenant_id, issuer, request_xml, relay_state, sp_entity_id,
    created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSAMLAuthRequest :one
SELECT * FROM saml_auth_requests
WHERE tenant_id = $1 AND id = $2 AND expires_at > $3
LIMIT 1;

-- name: CompleteSAMLAuthRequest :exec
-- Records who signed in. Until this runs the request has no subject and
-- cannot produce an assertion.
UPDATE saml_auth_requests
SET subject = $1, done = TRUE
WHERE tenant_id = $2 AND id = $3;

-- name: DeleteSAMLAuthRequest :exec
DELETE FROM saml_auth_requests WHERE tenant_id = $1 AND id = $2;

-- name: DeleteExpiredSAMLAuthRequests :exec
DELETE FROM saml_auth_requests WHERE tenant_id = $1 AND expires_at < $2;
