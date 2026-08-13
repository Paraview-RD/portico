-- name: ListExternalIdentityProviders :many
-- Every provider a tenant has configured, enabled or not.
SELECT * FROM external_identity_providers
WHERE tenant_id = $1 ORDER BY name;

-- name: ListActiveExternalIdentityProviders :many
-- What the sign-in screen offers. A disabled provider is not a button:
-- somebody switched it off, and offering it would mean a person picks it and
-- is refused after a round trip to somebody else's login page.
SELECT * FROM external_identity_providers
WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY name;

-- name: GetExternalIdentityProvider :one
SELECT * FROM external_identity_providers WHERE tenant_id = $1 AND id = $2;

-- name: CreateExternalIdentityProvider :exec
INSERT INTO external_identity_providers (
    id, tenant_id, name, button_label, issuer, client_id, client_secret,
    scopes, trust_verified_email, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'ACTIVE', $10, $10);

-- name: UpdateExternalIdentityProvider :exec
-- The secret is replaced only when a new one was given: an edit that left
-- the field blank means "leave it alone", because the console never shows
-- what is stored and a blank field is what an operator sees every time.
UPDATE external_identity_providers
SET name = sqlc.arg(name),
    button_label = sqlc.arg(button_label),
    issuer = sqlc.arg(issuer),
    client_id = sqlc.arg(client_id),
    client_secret = CASE WHEN sqlc.arg(client_secret)::text = ''
                         THEN client_secret ELSE sqlc.arg(client_secret)::text END,
    scopes = sqlc.arg(scopes),
    trust_verified_email = sqlc.arg(trust_verified_email),
    updated_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: SetExternalIdentityProviderStatus :exec
UPDATE external_identity_providers
SET status = $3, updated_at = $4 WHERE tenant_id = $1 AND id = $2;

-- name: DeleteExternalIdentityProvider :exec
-- The bindings go with it, by cascade. Deliberate: an identity that names a
-- provider nobody can reach is not a credential, and leaving the rows would
-- mean a re-added provider silently inheriting who was bound to the old one.
DELETE FROM external_identity_providers WHERE tenant_id = $1 AND id = $2;

-- name: GetExternalIdentity :one
-- The whole point of the pair: a subject is unique inside its issuer and
-- nowhere else.
SELECT * FROM external_identities
WHERE tenant_id = $1 AND provider_id = $2 AND subject = $3;

-- name: ListExternalIdentitiesForUser :many
SELECT * FROM external_identities
WHERE tenant_id = $1 AND user_id = $2 ORDER BY created_at;

-- name: CreateExternalIdentity :exec
INSERT INTO external_identities (
    id, tenant_id, provider_id, user_id, subject, email, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: TouchExternalIdentity :exec
UPDATE external_identities SET last_used_at = $4
WHERE tenant_id = $1 AND provider_id = $2 AND subject = $3;

-- name: DeleteExternalIdentity :exec
DELETE FROM external_identities
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: CountExternalIdentitiesForProvider :one
SELECT count(*) FROM external_identities
WHERE tenant_id = $1 AND provider_id = $2;

-- name: CreateExternalAuthRequest :exec
INSERT INTO external_auth_requests (
    state, tenant_id, provider_id, nonce, code_verifier, user_id,
    created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: TakeExternalAuthRequest :one
-- Read and delete in one statement.
--
-- Single-use is the property, and doing it in two statements would leave a
-- window where the same state answers twice — which is exactly what an
-- attacker replaying a callback is trying to do. Expiry is part of the
-- WHERE rather than a check afterwards, so a stale row cannot be consumed
-- and then rejected.
DELETE FROM external_auth_requests
WHERE tenant_id = $1 AND state = $2 AND expires_at > $3
RETURNING *;

-- name: DeleteExpiredExternalAuthRequests :exec
-- Per tenant and swept in a loop, like the password resets and the dead
-- refresh-token chains. A single unscoped DELETE would be cheaper and would
-- also be the one statement in this file that could reach another tenant's
-- rows — and a sweep is exactly where nobody would notice.
DELETE FROM external_auth_requests WHERE tenant_id = $1 AND expires_at < $2;
