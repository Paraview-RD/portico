-- name: CreateRefreshToken :exec
INSERT INTO oauth_refresh_tokens (
    id, tenant_id, client_id, subject, token_hash,
    scopes, audience, amr, auth_time, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: GetRefreshTokenByID :one
-- By primary key rather than by hash. The revocation endpoint is handed an
-- id, not the token: the protocol library resolves the token to one before
-- calling, so a lookup by hash there silently finds nothing.
SELECT * FROM oauth_refresh_tokens
WHERE tenant_id = $1 AND id = $2
LIMIT 1;

-- name: GetRefreshToken :one
-- Returns the row whatever its state, because the caller has to be able to
-- tell "expired" from "already spent" — a spent token being presented means
-- a copy leaked, and that calls for revoking the whole chain rather than
-- simply failing the call.
SELECT * FROM oauth_refresh_tokens
WHERE tenant_id = $1 AND token_hash = $2
LIMIT 1;

-- name: SpendRefreshToken :exec
UPDATE oauth_refresh_tokens
SET used_at = $1, replaced_by = $2
WHERE tenant_id = $3 AND id = $4;

-- name: RevokeRefreshToken :exec
UPDATE oauth_refresh_tokens
SET revoked_at = $1
WHERE tenant_id = $2 AND id = $3 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenChain :exec
-- Revokes every token descended from one, following replaced_by. Used when a
-- spent token is presented: which link of the chain leaked is unknown, so
-- the whole chain goes.
WITH RECURSIVE chain AS (
    SELECT r.id, r.replaced_by
    FROM oauth_refresh_tokens r
    WHERE r.tenant_id = $2 AND r.id = $3
    UNION ALL
    SELECT t.id, t.replaced_by
    FROM oauth_refresh_tokens t
    JOIN chain c ON t.id = c.replaced_by
    WHERE t.tenant_id = $2
)
UPDATE oauth_refresh_tokens o
SET revoked_at = $1
WHERE o.tenant_id = $2 AND o.id IN (SELECT id FROM chain) AND o.revoked_at IS NULL;

-- name: RevokeRefreshTokensForSession :exec
-- Ends a person's session with one relying party, which is what RP-initiated
-- logout and TerminateSession mean.
UPDATE oauth_refresh_tokens
SET revoked_at = $1
WHERE tenant_id = $2 AND subject = $3 AND client_id = $4 AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokensForUser :exec
-- Every relying party at once. Signing out of Portico, changing a password,
-- or being disabled has to reach the tokens other systems are holding, or
-- "sessions revoke immediately" stops being true the moment federation is
-- switched on.
UPDATE oauth_refresh_tokens
SET revoked_at = $1
WHERE tenant_id = $2 AND subject = $3 AND revoked_at IS NULL;

-- name: DeleteDeadRefreshTokenChains :exec
-- Clears refresh tokens whose entire rotation chain is long dead.
--
-- Not simply "expired": TokenRequestByRefreshToken checks used_at BEFORE it
-- checks expiry, deliberately, so that presenting an expired-but-spent token
-- still revokes the chain descended from it. That is the whole of the reuse
-- detection, and deleting expired rows one at a time would silently disable
-- it — the stolen token would come back as "unknown" and its live
-- replacements would survive.
--
-- So a row goes only when nothing downstream of it can still be used. Each
-- rotation issues a token with a later expiry than the one it replaces, so
-- the chain's terminal token — the one with no replacement — is the one that
-- expires last. When that is past the retention window the whole chain is
-- dead, and the recursion walks back up replaced_by to collect it.
--
-- Parent and child go in the same statement, which the self-referencing
-- foreign key permits: it is checked at end of statement, by which point
-- neither row is there to be referenced.
WITH RECURSIVE dead AS (
    SELECT r.id
    FROM oauth_refresh_tokens r
    WHERE r.tenant_id = $1
      AND r.replaced_by IS NULL
      AND r.expires_at < $2
    UNION ALL
    SELECT ancestor.id
    FROM oauth_refresh_tokens ancestor
    JOIN dead d ON ancestor.replaced_by = d.id
    WHERE ancestor.tenant_id = $1
)
DELETE FROM oauth_refresh_tokens o
WHERE o.tenant_id = $1 AND o.id IN (SELECT id FROM dead);
