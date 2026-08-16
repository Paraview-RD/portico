-- +goose Up

-- The mailbox a trial address actually reaches, as distinct from what was
-- typed into the form.
--
-- The rule this table has always claimed to enforce is one tenant per address,
-- and until now it enforced one tenant per *spelling*. Sub-addressing (RFC
-- 5233) means me+one@example.com and me+two@example.com are two strings and
-- one mailbox, so a single inbox could take the whole demonstration's quota
-- one plus-sign at a time — while every check in front of it reported that
-- each address was new, because as far as the index was concerned it was.
--
-- Lower-casing was already done in the service for the same reason, and this
-- is the rest of that thought.
--
-- Deliberately only the plus part. Ignoring dots would also collapse
-- me.one@gmail.com onto meone@gmail.com, which is true at Gmail and false
-- almost everywhere else — collapsing them would refuse two colleagues at a
-- provider that treats dots as ordinary characters, which is a worse failure
-- than the one being prevented.
--
-- The delivery address is untouched: `email` stays exactly as typed, because
-- that is where the link has to be sent.
ALTER TABLE trial_requests ADD COLUMN email_key TEXT NOT NULL DEFAULT '';

-- Existing rows, by the same rule the service now applies.
UPDATE trial_requests
SET email_key = lower(regexp_replace(email, '\+[^@]*@', '@'));

COMMENT ON COLUMN trial_requests.email_key IS
    'The mailbox `email` reaches: lower-cased and with any +sub-address removed. What "one tenant per address" is counted on. Never used for delivery.';

-- One tenant per mailbox, replacing one tenant per spelling.
DROP INDEX trial_requests_one_per_email;
CREATE UNIQUE INDEX trial_requests_one_per_email
    ON trial_requests (email_key) WHERE confirmed_at IS NOT NULL;

-- Counting what an address has asked for lately, which is what stops this
-- server being used to send somebody else repeated mail they never asked for:
-- the unique index above sees confirmed rows only, and a pending request is a
-- message already delivered.
CREATE INDEX trial_requests_by_email_key
    ON trial_requests (email_key, created_at);

-- And the same question asked of the whole deployment rather than one
-- address, which bounds how much mail a demonstration can be made to send in
-- an hour.
CREATE INDEX trial_requests_recent ON trial_requests (created_at);

-- +goose Down

DROP INDEX trial_requests_recent;
DROP INDEX trial_requests_by_email_key;
DROP INDEX trial_requests_one_per_email;
CREATE UNIQUE INDEX trial_requests_one_per_email
    ON trial_requests (email) WHERE confirmed_at IS NOT NULL;
ALTER TABLE trial_requests DROP COLUMN email_key;
