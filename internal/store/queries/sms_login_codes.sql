-- name: CreateSMSLoginCode :exec
INSERT INTO sms_login_codes (
    id, tenant_id, phone, code_hash, expires_at, created_at, ip
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetLiveSMSLoginCode :one
-- The newest unconsumed, unexpired code for this tenant's phone number.
-- Newest rather than "the one matching the code" because the caller does
-- not know the code is right yet -- it has to compare the hash itself,
-- and comparing against anything but the most recent request would let an
-- old, superseded code still verify.
SELECT * FROM sms_login_codes
WHERE tenant_id = $1
  AND phone = $2
  AND consumed_at IS NULL
  AND expires_at > $3
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementSMSLoginCodeAttempts :exec
UPDATE sms_login_codes SET attempts = attempts + 1
WHERE tenant_id = $1 AND id = $2;

-- name: ConsumeSMSLoginCode :exec
UPDATE sms_login_codes SET consumed_at = $3
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteExpiredSMSLoginCodes :exec
-- Same retention shape as DeleteExpiredPasswordResets: clears rows well
-- past their usefulness rather than deleting them the instant they expire.
DELETE FROM sms_login_codes
WHERE tenant_id = $1 AND expires_at < $2;
