-- name: CreateSAMLSigningKey :exec
INSERT INTO saml_signing_keys (
    id, tenant_id, private_key, certificate, status, created_at, expires_at
) VALUES ($1, $2, $3, $4, 'ACTIVE', $5, $6);

-- name: GetActiveSAMLSigningKey :one
SELECT * FROM saml_signing_keys
WHERE tenant_id = $1 AND status = 'ACTIVE'
LIMIT 1;

-- name: ListSAMLSigningKeys :many
-- Active first, then retired newest first. Retired keys are kept rather than
-- deleted: a service provider pins the certificate in its own configuration
-- and cannot be told to refetch, so an operator has to be able to see what
-- the previous one was while service providers are being moved across.
SELECT * FROM saml_signing_keys
WHERE tenant_id = $1
ORDER BY CASE WHEN status = 'ACTIVE' THEN 0 ELSE 1 END, created_at DESC;

-- name: RetireSAMLSigningKeys :exec
UPDATE saml_signing_keys
SET status = 'RETIRED', retired_at = $1
WHERE tenant_id = $2 AND status = 'ACTIVE';
