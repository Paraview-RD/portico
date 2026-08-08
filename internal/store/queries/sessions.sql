-- name: CreateSession :exec
INSERT INTO sessions (
    id, tenant_id, user_id, ip, user_agent,
    created_at, last_seen_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $6, $7);

-- name: GetLiveSession :one
-- A session is usable only if it is unrevoked and unexpired. Both conditions
-- are in the query rather than checked afterwards, so a caller cannot act on
-- a row it forgot to validate.
--
-- Scoped by tenant as well as by id, even though an id is a UUID and
-- collisions are not the concern: the tenant comes from the token, so a
-- session cannot be presented against a tenant it was not issued in. That is
-- the same guarantee the account lookup makes a few lines later, and there
-- is no reason for this one to be weaker.
SELECT * FROM sessions
WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL AND expires_at > $3
LIMIT 1;

-- name: TouchSession :exec
-- Lazily updates last_seen_at. The caller decides how stale is stale enough
-- to be worth a write; see auth.Middleware.
UPDATE sessions SET last_seen_at = $1 WHERE tenant_id = $2 AND id = $3;

-- name: ListSessionsForUser :many
SELECT * FROM sessions
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > $3
ORDER BY last_seen_at DESC;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = $1
WHERE tenant_id = $2 AND id = $3 AND revoked_at IS NULL;

-- name: RevokeSessionsForUser :exec
-- Every session an account has. Signing out everywhere, and what a disable
-- or password change amounts to here.
UPDATE sessions SET revoked_at = $1
WHERE tenant_id = $2 AND user_id = $3 AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :exec
-- Rows past the retention window. A revoked or expired session is kept for a
-- while so that "when did this end, and from where" stays answerable.
DELETE FROM sessions
WHERE tenant_id = $1 AND expires_at < $2;
