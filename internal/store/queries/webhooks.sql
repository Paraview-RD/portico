-- name: CreateWebhookSubscription :exec
INSERT INTO webhook_subscriptions (
    id, tenant_id, name, url, secret, events, headers, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'ACTIVE', $8, $8);

-- name: ListWebhookSubscriptions :many
SELECT * FROM webhook_subscriptions
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: ListActiveWebhookSubscriptions :many
SELECT * FROM webhook_subscriptions
WHERE tenant_id = $1 AND status = 'ACTIVE';

-- name: GetWebhookSubscription :one
SELECT * FROM webhook_subscriptions
WHERE tenant_id = $1 AND id = $2;

-- name: UpdateWebhookSubscription :exec
UPDATE webhook_subscriptions
SET name = $1, url = $2, events = $3, headers = $4, updated_at = $5
WHERE tenant_id = $6 AND id = $7;

-- name: SetWebhookSubscriptionStatus :exec
UPDATE webhook_subscriptions
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: RotateWebhookSubscriptionSecret :exec
-- The old key moves aside rather than being discarded, and is sent alongside
-- the new one until it expires. Portico produces the signature and the
-- receiver verifies it, so the receiver is the side that needs a window in
-- which to deploy the new secret.
UPDATE webhook_subscriptions
SET secret = $1,
    previous_secret = secret,
    previous_secret_expires_at = $2,
    updated_at = $3
WHERE tenant_id = $4 AND id = $5;

-- name: DeleteWebhookSubscription :exec
-- Deleted rather than disabled, like a SCIM credential and unlike an
-- account: what the audit trail refers to is the event, not the row.
DELETE FROM webhook_subscriptions
WHERE tenant_id = $1 AND id = $2;

-- name: EnqueueWebhookDelivery :exec
INSERT INTO webhook_deliveries (
    id, tenant_id, subscription_id, event_type, payload,
    status, next_attempt_at, created_at
) VALUES ($1, $2, $3, $4, $5, 'PENDING', $6, $6);

-- name: ClaimDueWebhookDeliveries :many
-- What is due, oldest first, claimed so two workers cannot take the same row.
--
-- FOR UPDATE SKIP LOCKED rather than a status flag written in advance: a
-- claim that is a write is a claim that outlives a crash, and a delivery
-- stuck in "SENDING" because a process died mid-attempt is a delivery
-- nothing will ever retry. Here the lock disappears with the transaction.
--
-- Not tenant-scoped, and it is the only sweep query that is not: the worker
-- runs per tenant and passes its own id. See internal/webhook/dispatcher.go.
SELECT * FROM webhook_deliveries
WHERE tenant_id = $1
  AND status = 'PENDING'
  AND next_attempt_at <= $2
ORDER BY next_attempt_at
LIMIT $3
FOR UPDATE SKIP LOCKED;

-- name: MarkWebhookDelivered :exec
UPDATE webhook_deliveries
SET status = 'DELIVERED',
    attempts = attempts + 1,
    last_status = $1,
    last_error = '',
    last_response = $2,
    next_attempt_at = NULL,
    delivered_at = $3
WHERE tenant_id = $4 AND id = $5;

-- name: MarkWebhookAttemptFailed :exec
-- One failed attempt, with when to try again. A NULL next_attempt_at and
-- status FAILED is the end of the road.
UPDATE webhook_deliveries
SET status = $1,
    attempts = attempts + 1,
    last_status = $2,
    last_error = $3,
    last_response = $4,
    next_attempt_at = $5
WHERE tenant_id = $6 AND id = $7;

-- name: ListWebhookDeliveries :many
-- One page, newest first, optionally starting after a row the caller
-- already has.
--
-- A cursor rather than an offset. This table is written to continuously —
-- every event, every retry — so an offset walked backwards through it
-- returns rows twice and skips others as it goes, and the reader has no way
-- to tell. (created_at, id) is the same order the index is in, and id
-- breaks the tie between two deliveries queued in the same instant.
--
-- @cursor_created_at and @cursor_id are the last row of the previous page.
-- The first page passes store.AfterEverything, a timestamp no row can hold,
-- so the same comparison serves both cases: every real created_at is below
-- it, and a tuple comparison decided by its first element never reaches the
-- id.
--
-- @sync_filter: 'all' is everything, 'live' hides the sync.* pages a full
-- sync produces, 'sync' shows only those. A full sync of a large tenant is
-- a hundred deliveries in a few seconds, which is not many rows but is
-- enough to push every ordinary event off the first page of what somebody
-- is reading.
SELECT * FROM webhook_deliveries
WHERE tenant_id = $1 AND subscription_id = $2
  AND (created_at, id) < (sqlc.arg(cursor_created_at)::timestamptz, sqlc.arg(cursor_id)::text)
  AND (
    sqlc.arg(sync_filter)::text = 'all'
    OR (sqlc.arg(sync_filter)::text = 'sync' AND event_type LIKE 'sync.%')
    OR (sqlc.arg(sync_filter)::text = 'live' AND event_type NOT LIKE 'sync.%')
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: GetWebhookDelivery :one
-- One delivery with everything it holds, including the bodies. Fetched only
-- when somebody opens it: a sync page's payload is five hundred objects, and
-- putting that in a list would make the list unusable to save a click.
SELECT * FROM webhook_deliveries
WHERE tenant_id = $1 AND subscription_id = $2 AND id = $3;

-- name: DeleteOldWebhookDeliveries :exec
-- Past the retention window, whatever state it reached.
--
-- Pending used to be excluded, on the reasoning that a row still waiting is
-- a row with work left. At this age it is not: five attempts span under half
-- an hour, so anything pending thirty days later is pending against a
-- subscription that was disabled while it was queued — the worker skips
-- those without recording an attempt, so they are never marked failed and
-- nothing else would ever remove them. Excluding them meant one forgotten
-- subscription grew this table for the life of the deployment.
DELETE FROM webhook_deliveries
WHERE tenant_id = $1 AND created_at < $2;

-- name: CountPendingSnapshotDeliveries :one
-- How much of a snapshot is still queued for one subscription.
--
-- The guard against two snapshots at once. Asking the queue rather than
-- keeping a "running" flag is deliberate: a flag needs clearing, and a
-- process that dies mid-snapshot would leave one set forever with nothing
-- to notice. The queue empties on its own, whether the deliveries succeed
-- or are given up on.
SELECT count(*) FROM webhook_deliveries
WHERE tenant_id = $1 AND subscription_id = $2
  AND status = 'PENDING'
  AND event_type LIKE 'sync.%';
