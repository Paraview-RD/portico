-- name: CreatePasswordReset :exec
INSERT INTO password_resets (
    id, tenant_id, user_id, token_hash, channel, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetLivePasswordReset :one
-- A token is usable only if it is unspent and unexpired. Both conditions are
-- in the query rather than checked afterwards, so a caller cannot act on a
-- row it forgot to validate — there is no way to fetch a dead one.
SELECT * FROM password_resets
WHERE tenant_id = $1
  AND token_hash = $2
  AND used_at IS NULL
  AND expires_at > $3
LIMIT 1;

-- name: CountRecentPasswordResets :one
-- How many reset messages this account has been sent lately.
--
-- Counts rows rather than live tokens, and that is the point: a superseded
-- row is a message that was already delivered, and SupersedePasswordResets
-- marks every earlier request spent the moment a new one arrives. Counting
-- only unspent rows would therefore always return one, and the cap would
-- never fire.
--
-- Safe against the retention sweep only because that sweep keeps thirty days
-- and this window is one — see passwordResetRetention in internal/server. A
-- retention shorter than the window would silently shrink it and leave a cap
-- that cannot be reached.
SELECT COUNT(*) FROM password_resets
WHERE tenant_id = $1 AND user_id = $2 AND created_at > $3;

-- name: SpendPasswordReset :exec
UPDATE password_resets SET used_at = $1 WHERE tenant_id = $2 AND id = $3;

-- name: SupersedePasswordResets :exec
-- Marks a user's outstanding requests spent, so asking again invalidates the
-- previous link. Without this, every request a user ever made stays live
-- until it expires, and someone who has read one old message can use it.
UPDATE password_resets
SET used_at = $1
WHERE tenant_id = $2 AND user_id = $3 AND used_at IS NULL;

-- name: DeleteExpiredPasswordResets :exec
-- Clears reset requests long past their usefulness.
--
-- Spent and expired rows are kept for a retention window rather than deleted
-- the moment they die, so that "was a reset link ever issued for this
-- account, and was it used" stays answerable for a while after the fact. The
-- audit trail records the request and the completion separately; this table
-- is the operational copy, and it is the one that would otherwise grow
-- without limit.
DELETE FROM password_resets
WHERE tenant_id = $1 AND expires_at < $2;

-- name: RecordPasswordInHistory :exec
INSERT INTO password_history (id, tenant_id, user_id, password_hash, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: RecentPasswordHashes :many
-- The newest N hashes for an account, for a reuse check.
SELECT password_hash FROM password_history
WHERE tenant_id = $1 AND user_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: TrimPasswordHistory :exec
-- Drops everything past the depth now configured, so lowering the setting
-- takes effect rather than leaving older entries consultable forever.
DELETE FROM password_history h
WHERE h.tenant_id = $1 AND h.user_id = $2 AND h.id NOT IN (
    SELECT keep.id FROM password_history keep
    WHERE keep.tenant_id = $1 AND keep.user_id = $2
    ORDER BY keep.created_at DESC
    LIMIT $3
);
