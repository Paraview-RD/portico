-- name: CreateAuthRequest :exec
INSERT INTO oauth_auth_requests (
    id, tenant_id, client_id, issuer, redirect_uri, response_type,
    response_mode, scopes, audience, state, nonce, code_challenge,
    code_challenge_method, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: GetAuthRequest :one
SELECT * FROM oauth_auth_requests
WHERE tenant_id = $1 AND id = $2 AND expires_at > $3
LIMIT 1;

-- name: GetAuthRequestByCode :one
-- Only an unexpired, completed request can be exchanged. Both conditions are
-- in the query so a caller cannot hold a row it forgot to check.
SELECT * FROM oauth_auth_requests
WHERE tenant_id = $1 AND code_hash = $2 AND done = TRUE AND expires_at > $3
LIMIT 1;

-- name: CompleteAuthRequest :exec
-- Records who signed in. Until this runs the request has no subject and
-- cannot produce a token.
UPDATE oauth_auth_requests
SET subject = $1, auth_time = $2, amr = $3, done = TRUE
WHERE tenant_id = $4 AND id = $5;

-- name: SaveAuthCode :exec
UPDATE oauth_auth_requests
SET code_hash = $1
WHERE tenant_id = $2 AND id = $3;

-- name: DeleteAuthRequest :exec
DELETE FROM oauth_auth_requests WHERE tenant_id = $1 AND id = $2;

-- name: DeleteExpiredAuthRequests :exec
DELETE FROM oauth_auth_requests WHERE tenant_id = $1 AND expires_at < $2;
