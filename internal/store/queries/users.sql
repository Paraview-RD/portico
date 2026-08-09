-- name: GetUserByID :one
SELECT * FROM users WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE tenant_id = $1 AND username = $2 LIMIT 1;

-- name: GetUserByIdentifier :one
-- Sign-in accepts any of the three identifiers (§3.4). This is deliberately
-- one query with a declared precedence rather than three tried in turn: a
-- username may look like an email, so "which column matched" has to have a
-- fixed answer, and an implicit one would depend on row order.
--
-- Username wins. If one account's email equals another's username, the
-- username holder signs in and the email holder cannot use that identifier —
-- an inconvenience for the second account, not access to the first, since
-- the password is still the gate.
--
-- Password recovery must NOT use this query. It resolves an identifier and
-- then sends a token, so a cross-column collision there would route someone
-- else's reset to the caller. Recovery uses the channel-specific lookups
-- below.
SELECT * FROM users
WHERE tenant_id = $1
  AND (username = $2
       OR (email <> '' AND email = $2)
       OR (phone <> '' AND phone = $2))
ORDER BY CASE
    WHEN username = $2 THEN 0
    WHEN email = $2 THEN 1
    ELSE 2
END
LIMIT 1;

-- name: GetUserByEmail :one
-- For password recovery over email, and only that. Matching the email column
-- alone is what keeps a username that happens to look like an address from
-- routing a reset to the wrong account.
SELECT * FROM users WHERE tenant_id = $1 AND email <> '' AND email = $2 LIMIT 1;

-- name: GetUserByPhone :one
-- The same, for recovery over SMS.
SELECT * FROM users WHERE tenant_id = $1 AND phone <> '' AND phone = $2 LIMIT 1;

-- name: CreateUser :exec
INSERT INTO users (
    id, tenant_id, username, display_name, password_hash, phone, email,
    role, status, organization_id, token_version, source,
    password_changed_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = $1,
    phone = $2,
    email = $3,
    organization_id = $4,
    role = $5,
    updated_at = $6
WHERE tenant_id = $7 AND id = $8;

-- name: UpdateUserStatus :exec
-- Disabling bumps token_version so any live session stops working at once.
UPDATE users
SET status = sqlc.arg(status),
    token_version = CASE WHEN sqlc.arg(status) = 'DISABLED'
                         THEN token_version + 1
                         ELSE token_version END,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: UpdateUserPassword :exec
-- Changing a password invalidates every token issued before it, and starts
-- the clock again for expiry.
UPDATE users
SET password_hash = sqlc.arg(password_hash),
    token_version = token_version + 1,
    password_changed_at = sqlc.arg(now)::timestamptz,
    updated_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: BumpUserTokenVersion :exec
UPDATE users
SET token_version = token_version + 1,
    updated_at = $1
WHERE tenant_id = $2 AND id = $3;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1;

-- name: CountUsersByOrganization :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND organization_id = $2;

-- name: CountUsersPerOrganization :many
-- Member counts for a whole organization listing, in one round trip rather
-- than one per organization.
SELECT organization_id, COUNT(*) AS member_count
FROM users
WHERE tenant_id = $1 AND organization_id IS NOT NULL
GROUP BY organization_id;

-- name: CountOtherActiveAdmins :one
-- How many administrators would remain if this one were demoted or
-- disabled. Scoped to the tenant: each tenant administers itself, so another
-- tenant's administrators are not a reason to let this one lock itself out.
SELECT COUNT(*) FROM users
WHERE tenant_id = $1 AND role = $2 AND status = $3 AND id <> $4;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE tenant_id = $1 AND id = ANY($2::text[]);

-- name: RecordFailedLogin :one
-- Counts a failed sign-in and locks the account when the threshold is met.
--
-- One statement, so that concurrent attempts cannot interleave a read and a
-- write and lose a count — which is exactly what an attacker running
-- parallel guesses would produce.
--
-- The count restarts if the previous failure is older than the counting
-- window, so five failures spread over a year are not a lockout.
--
-- An account already locked keeps its existing locked_until. Extending it on
-- every further attempt would let anyone hold a known account locked
-- indefinitely just by guessing at it.
UPDATE users
SET failed_login_attempts = CASE
        WHEN last_failed_login_at IS NULL OR last_failed_login_at < sqlc.arg(window_start)::timestamptz
        THEN 1
        ELSE failed_login_attempts + 1
    END,
    last_failed_login_at = sqlc.arg(now)::timestamptz,
    locked_until = CASE
        WHEN locked_until IS NOT NULL AND locked_until > sqlc.arg(now)::timestamptz
        THEN locked_until
        WHEN (CASE
                WHEN last_failed_login_at IS NULL OR last_failed_login_at < sqlc.arg(window_start)::timestamptz
                THEN 1
                ELSE failed_login_attempts + 1
              END) >= sqlc.arg(threshold)::int
        THEN sqlc.arg(lock_until)::timestamptz
        ELSE NULL
    END,
    updated_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING failed_login_attempts, locked_until;

-- name: ClearLoginFailures :exec
-- Forgets the failures. A successful sign-in, a completed password recovery,
-- and an administrator unlocking all mean the same thing here.
UPDATE users
SET failed_login_attempts = 0,
    last_failed_login_at = NULL,
    locked_until = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: CloseUserAccount :exec
UPDATE users
SET status = 'DISABLED',
    closed_at = sqlc.arg(now)::timestamptz,
    token_version = token_version + 1,
    updated_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = $1 AND id = $2;

-- Reinstating is deliberate: an administrator enabling a closed account is
-- undoing somebody's decision, so the mark comes off with it rather than
-- leaving a row that reads as closed and signs in.
-- name: ReopenUserAccount :exec
UPDATE users
SET closed_at = NULL, updated_at = $1
WHERE tenant_id = $2 AND id = $3;

-- name: CreateRegistrationVerification :exec
INSERT INTO registration_verifications (
    id, tenant_id, user_id, token_hash, channel, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- Asking again invalidates the previous link, so only the newest message
-- works and an older one somebody else has read does not.
-- name: SupersedeRegistrationVerifications :exec
UPDATE registration_verifications
SET used_at = $1
WHERE tenant_id = $2 AND user_id = $3 AND used_at IS NULL;

-- name: ConsumeRegistrationVerification :one
UPDATE registration_verifications
SET used_at = sqlc.arg(now)::timestamptz
WHERE tenant_id = $1
  AND token_hash = $2
  AND used_at IS NULL
  AND expires_at > sqlc.arg(now)::timestamptz
RETURNING *;

-- name: MarkUserVerified :exec
UPDATE users
SET verified_at = $1, updated_at = $1
WHERE tenant_id = $2 AND id = $3;
