-- +goose Up

-- What the receiver answered, so a failure can be diagnosed from the console
-- rather than from their logs.
--
-- The request body has been stored since deliveries existed — `payload`,
-- kept as sent. The response was not: only the status code and, when the
-- request never reached a server at all, the transport error. That is enough
-- to see *that* something failed and almost never enough to see why. A 400
-- from a receiver is a sentence explaining which field it did not like, and
-- until now that sentence was thrown away as the connection was drained.
--
-- Capped when written, not here. A receiver answering an error with a
-- megabyte of HTML is a real thing, and the point of this column is the
-- first paragraph of an error message; the dispatcher keeps the first 2 KiB
-- and discards the rest, which it was already discarding in full.
--
-- What is deliberately *not* stored, and should stay that way: the request
-- headers. A subscription's custom headers are credentials — sealed with
-- PORTICO_ENCRYPTION_KEY precisely so that a database dump does not yield
-- them — and copying them into a delivery row to make a debugging screen
-- more complete would undo that for every delivery ever made.
--
-- Empty for every row written before this, and for every delivery whose
-- receiver answered with nothing.
ALTER TABLE webhook_deliveries
    ADD COLUMN last_response TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN webhook_deliveries.last_response IS
    'The beginning of what the receiver answered on the most recent attempt, capped at 2 KiB when written. Never contains request headers: a subscription''s custom headers are credentials and are not copied here.';

-- +goose Down
ALTER TABLE webhook_deliveries DROP COLUMN last_response;
