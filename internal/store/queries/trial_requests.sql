-- Self-service trial signups. Unscoped, unlike everything else here: these
-- rows are what exists before a tenant does. See migrations/00023.

-- name: CreateTrialRequest :one
-- Reserves the code and the address in one statement. A duplicate code or a
-- second confirmed request for the same address is refused by the indexes
-- rather than by a prior read, because two clicks arriving together would
-- both pass a read.
INSERT INTO trial_requests (
    id, email, company_name, tenant_code, industry,
    token_hash, expires_at, request_ip
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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
-- Whether this address already has a tenant.
--
-- Checked at request time as well as enforced by the index, because the index
-- is partial on confirmed rows: a second *pending* request for the same
-- address does not collide with it, so without this read the visitor is told
-- to check their email and only discovers the refusal after clicking — with a
-- tenant already created for their first request.
SELECT count(*) FROM trial_requests
WHERE email = $1 AND confirmed_at IS NOT NULL;
