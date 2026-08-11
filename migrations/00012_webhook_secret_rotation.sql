-- +goose Up

-- Rotating a webhook secret without dropping deliveries.
--
-- Before this, a leaked secret had one remedy: delete the subscription and
-- register a new one. That changes the subscription id, which is what the
-- delivery history hangs off and what a receiver deduplicating on delivery
-- ids has been keeping — so the cure discarded the evidence and broke
-- idempotency at the far end.
--
-- The overlap is what makes rotation safe, and it exists because of who
-- holds what. Portico produces the signature; the receiver verifies it. The
-- receiver therefore needs a window in which to deploy the new secret, and
-- the only way to give them one is to sign each delivery with both keys
-- until the window closes.
--
-- Nullable rather than a separate table: a subscription has at most one
-- previous secret at a time, and the pair expires together.
ALTER TABLE webhook_subscriptions
    ADD COLUMN previous_secret TEXT NOT NULL DEFAULT '',
    -- When the previous secret stops being sent. NULL means there is no
    -- rotation in flight, which is the ordinary state and the one every
    -- existing row is in.
    ADD COLUMN previous_secret_expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE webhook_subscriptions
    DROP COLUMN previous_secret,
    DROP COLUMN previous_secret_expires_at;
