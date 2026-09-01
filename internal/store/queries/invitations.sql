-- name: CreateInvitation :exec
INSERT INTO invitations (
    id, tenant_id, code, organization_id, group_ids,
    quota, expires_at, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9);

-- name: GetInvitation :one
SELECT * FROM invitations WHERE tenant_id = $1 AND id = $2;

-- name: GetInvitationByCode :one
SELECT * FROM invitations WHERE tenant_id = $1 AND code = $2;

-- name: ListInvitations :many
SELECT * FROM invitations WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: UpdateInvitationStatus :exec
UPDATE invitations SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- The atomic increment that makes concurrent redemption safe: a row comes
-- back only if the invitation is ACTIVE and still has quota, and the check
-- and the increment happen in the same statement, so two concurrent callers
-- can never both observe used_count < quota and both commit. Expiry is
-- checked by the caller against store.Now() before this runs, not here —
-- this project compares against an application-supplied clock rather than
-- the database's own NOW() so tests can control it (see store.Now()).
--
-- This is deliberately not wrapped as a Scoped method (internal/store/scoped.go).
-- It must only run inside the same store.WithTx transaction as the account
-- creation it pays for; a non-transactional caller could redeem a code and
-- then fail to create the account, leaving the redemption unrecoverable.
-- name: RedeemInvitation :one
UPDATE invitations
SET used_count = used_count + 1, updated_at = $3
WHERE tenant_id = $1 AND id = $2
  AND status = 'ACTIVE' AND used_count < quota
RETURNING *;
