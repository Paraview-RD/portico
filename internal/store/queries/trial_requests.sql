-- Self-service trial signups. Unscoped, unlike everything else here: these
-- rows are what exists before a tenant does. See migrations/00023.

-- name: CreateTrialRequest :one
-- Reserves the code and the address in one statement. A duplicate code or a
-- second confirmed request for the same address is refused by the indexes
-- rather than by a prior read, because two clicks arriving together would
-- both pass a read.
INSERT INTO trial_requests (
    id, email, email_key, company_name, tenant_code, industry,
    token_hash, expires_at, request_ip
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetTrialRequestByToken :one
-- What a followed link resolves to. Returns the row whatever its state: an
-- already-confirmed request and an expired one need different answers, and
-- both are better than "no such link".
SELECT * FROM trial_requests WHERE token_hash = $1;

-- name: MarkTrialRequestConfirmed :one
-- Spends the link and records what it produced.
--
-- The WHERE clause carries confirmed_at IS NULL rather than trusting the read
-- above: two clicks on the same link a moment apart both find a pending row,
-- and only one of them may create a tenant.
UPDATE trial_requests
SET confirmed_at = now(), tenant_id = $2
WHERE id = $1 AND confirmed_at IS NULL
RETURNING *;

-- name: CountConfirmedTrials :one
-- The global quota. Confirmed only: a pending request has cost nothing yet.
SELECT count(*) FROM trial_requests WHERE confirmed_at IS NOT NULL;

-- name: CountRecentTrialRequestsFromIP :one
-- The rate limiter's slower cousin — one address opening tenants steadily
-- rather than quickly, which per-minute limiting does not see.
SELECT count(*) FROM trial_requests
WHERE request_ip = $1 AND created_at > $2;

-- name: DeleteExpiredTrialRequests :execrows
-- Returns a reserved code to circulation. Unconfirmed only: a confirmed
-- request is the record of a tenant that exists.
DELETE FROM trial_requests
WHERE confirmed_at IS NULL AND expires_at < now();

-- name: DeleteTrialRequestByToken :execrows
-- Reverses a reservation whose link could not be sent. The address and the
-- code are held by the row, so a failed send has to release them or the
-- visitor cannot retry with the details they just typed.
DELETE FROM trial_requests WHERE token_hash = $1 AND confirmed_at IS NULL;

-- name: CountConfirmedTrialsForEmail :one
-- Whether this mailbox already has a tenant.
--
-- On email_key rather than email, so that a plus-sub-address is the same
-- mailbox it actually is. See migrations/00024.
--
-- Checked at request time as well as enforced by the index, because the index
-- is partial on confirmed rows: a second *pending* request for the same
-- address does not collide with it, so without this read the visitor is told
-- to check their email and only discovers the refusal after clicking — with a
-- tenant already created for their first request.
SELECT count(*) FROM trial_requests
WHERE email_key = $1 AND confirmed_at IS NOT NULL;

-- name: CountRecentTrialRequestsForEmail :one
-- How much mail this server has been asked to send one mailbox lately.
--
-- The unique index above sees confirmed rows only, which leaves the case that
-- costs somebody else something: a request that is never confirmed has still
-- put a message in their inbox. Without this, an address nobody controls can
-- be sent a fresh "confirm your Portico trial" as often as anyone likes, each
-- with a different tenant code so nothing else collides.
--
-- Counts pending and confirmed alike: both were an email.
SELECT count(*) FROM trial_requests
WHERE email_key = $1 AND created_at > $2;

-- name: CountRecentTrialRequests :one
-- The same question asked of the whole deployment: how many trial messages
-- have been sent in the window, from anywhere, to anyone.
--
-- What this bounds is not abuse of one address but the total a demonstration
-- can be made to send — a sending quota, and a sender reputation, are shared
-- by every message that leaves.
SELECT count(*) FROM trial_requests WHERE created_at > $1;

-- The confirmed trials and the tenants they produced, for the command line.
--
-- A join rather than two reads because the interesting row is the pair: a
-- tenant that came from a trial, and the address that asked for it. Neither
-- table alone can say which tenants a stranger created.
-- name: ListConfirmedTrials :many
SELECT
    t.code AS tenant_code,
    t.name AS tenant_name,
    t.status AS tenant_status,
    r.email,
    r.industry,
    r.confirmed_at,
    r.request_ip
FROM trial_requests r
JOIN tenants t ON t.id = r.tenant_id
WHERE r.confirmed_at IS NOT NULL
ORDER BY r.confirmed_at DESC;

-- Whether a tenant came from a trial, which is what bounds what the command
-- line is allowed to delete. A tenant nobody can find here was provisioned by
-- hand and is not this command's business.
-- name: GetConfirmedTrialByTenantCode :one
SELECT r.id, r.email, r.tenant_id, r.confirmed_at
FROM trial_requests r
JOIN tenants t ON t.id = r.tenant_id
WHERE t.code = $1 AND r.confirmed_at IS NOT NULL;

-- Releases the address and the code together with the tenant they named. Kept
-- separate from deleting the tenant so that the order is visible at the call
-- site: this row is what the foreign key points from.
-- name: DeleteTrialRequestByTenant :execrows
DELETE FROM trial_requests WHERE tenant_id = $1;

-- name: DeleteTrialRequestByTenantCode :exec
-- Releases the code, the quota slot, and the applicant's address together.
--
-- The row survives confirmation on purpose: it is what the quota counts, what
-- reserves the tenant code, and what holds one-tenant-per-mailbox. So deleting
-- the tenant without deleting this leaves all three held by a tenant that no
-- longer exists — the quota never recovers, and the applicant can never come
-- back.
DELETE FROM trial_requests WHERE tenant_code = $1;
