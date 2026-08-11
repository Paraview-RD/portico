-- +goose Up

-- Headers a subscription sends with every delivery.
--
-- The case this exists for is a receiver behind an API gateway, which
-- refuses anything without an Authorization header of its own — the
-- signature proves who sent the body, and the gateway wants to know whether
-- to let the request past at all. Those are different questions and the
-- signature cannot answer the second.
--
-- Sealed with PORTICO_ENCRYPTION_KEY, on the same footing as a directory
-- bind password and for the same reason: it is a credential this server has
-- to store and later present, so a digest is useless. Without a key
-- configured, saving one is refused rather than written in the clear —
-- storing somebody's bearer token in plaintext would be worse than not
-- offering the feature.
--
-- One column holding the whole set rather than a table of name/value rows.
-- They are written and read together, always, and the values are ciphertext
-- either way — a row per header would mean a nonce per header and a join to
-- assemble something that is only ever used whole.
ALTER TABLE webhook_subscriptions
    ADD COLUMN headers TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhook_subscriptions DROP COLUMN headers;
