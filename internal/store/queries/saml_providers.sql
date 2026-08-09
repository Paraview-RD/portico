-- name: CreateSAMLServiceProvider :exec
INSERT INTO saml_service_providers (
    id, tenant_id, entity_id, name, metadata_xml, launch_url, logo_uri,
    status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetSAMLServiceProvider :one
SELECT * FROM saml_service_providers
WHERE tenant_id = $1 AND entity_id = $2
LIMIT 1;

-- Looked up by the row's own id, not the entity id, because the entity id is
-- a URI: putting one in a URL path means percent-encoding its slashes, and a
-- reverse proxy configured to normalize paths will decode them and split the
-- identifier across segments. An opaque id has no such edge.
-- name: GetSAMLServiceProviderByID :one
SELECT * FROM saml_service_providers
WHERE tenant_id = $1 AND id = $2
LIMIT 1;

-- name: ListSAMLServiceProviders :many
SELECT * FROM saml_service_providers
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: UpdateSAMLServiceProviderStatus :exec
UPDATE saml_service_providers
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND entity_id = $4;

-- Re-uploading metadata is how a service provider's certificate gets
-- rotated, so this has to exist for a registration to survive its first
-- year. The entity id is the match key and is not updated: a document
-- declaring a different one describes a different service provider, and the
-- service layer rejects it rather than silently repointing a registration.
-- name: UpdateSAMLServiceProvider :exec
UPDATE saml_service_providers
SET name = $1, metadata_xml = $2, launch_url = $3, logo_uri = $4, updated_at = $5
WHERE tenant_id = $6 AND entity_id = $7;

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
