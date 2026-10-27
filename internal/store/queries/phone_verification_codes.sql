-- name: CreatePhoneVerificationCode :exec
INSERT INTO phone_verification_codes (
    id, tenant_id, user_id, phone, code_hash, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetLivePhoneVerificationCode :one
-- The newest unconsumed, unexpired code this user has been sent for this
-- candidate phone. Newest for the same reason GetLiveSMSLoginCode is:
-- ConfirmPhoneChange does not know the code is right yet, and comparing
-- against anything but the most recent request would let a superseded code
-- still verify.
SELECT * FROM phone_verification_codes
WHERE tenant_id = $1
  AND user_id = $2
  AND phone = $3
  AND consumed_at IS NULL
  AND expires_at > $4
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementPhoneVerificationCodeAttempts :exec
UPDATE phone_verification_codes SET attempts = attempts + 1
WHERE tenant_id = $1 AND id = $2;

-- name: ConsumePhoneVerificationCode :exec
UPDATE phone_verification_codes SET consumed_at = $3
WHERE tenant_id = $1 AND id = $2;

-- name: CountRecentPhoneVerificationCodes :one
-- RequestPhoneChange's per-account daily cap, same shape as
-- CountRecentPasswordResets -- rows, not live codes, so an already-consumed
-- or superseded request still counts against the sender it was.
SELECT count(*) FROM phone_verification_codes
WHERE tenant_id = $1 AND user_id = $2 AND created_at > $3;

-- name: DeleteExpiredPhoneVerificationCodes :exec
-- Same retention shape as DeleteExpiredSMSLoginCodes.
DELETE FROM phone_verification_codes
WHERE tenant_id = $1 AND expires_at < $2;
