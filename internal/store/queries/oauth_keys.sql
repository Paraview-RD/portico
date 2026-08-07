-- name: CreateSigningKey :exec
INSERT INTO oauth_signing_keys (
    id, tenant_id, algorithm, private_key, public_key, status, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetActiveSigningKey :one
-- The key new tokens are signed with. There is exactly one; rotation makes a
-- new one active and retires the old, rather than leaving two to choose from.
SELECT * FROM oauth_signing_keys
WHERE tenant_id = $1 AND status = 'ACTIVE'
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPublishedSigningKeys :many
-- Everything the JWKS advertises: the active key and any retired key whose
-- tokens may still be in flight. A relying party that fetched the key set
-- before a rotation must still be able to verify what it holds.
SELECT * FROM oauth_signing_keys
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: RetireSigningKeys :exec
UPDATE oauth_signing_keys
SET status = 'RETIRED', retired_at = $1
WHERE tenant_id = $2 AND status = 'ACTIVE';

-- name: DeleteExpiredSigningKeys :exec
-- Retired long enough that nothing it signed can still be valid.
DELETE FROM oauth_signing_keys
WHERE tenant_id = $1 AND status = 'RETIRED' AND retired_at < $2;
