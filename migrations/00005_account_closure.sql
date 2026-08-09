-- +goose Up

-- When somebody closed their own account.
--
-- Not a new value of `status`, deliberately. That column is shared with
-- organizations and all three kinds of application registration, none of
-- which has any notion of a person closing something — widening a shared
-- enum for one entity's lifecycle is where enums start to rot, and it would
-- make INVALID_STATUS's own message ("must be active or disabled") false.
-- This is also genuinely a "when did it happen", which is a timestamp.
--
-- A closed account is `DISABLED` and additionally carries this. An
-- administrator can see the difference, which matters: "this person left" and
-- "we suspended this person" call for different responses, and a single
-- status cannot say which happened.
--
-- Closure deactivates rather than deletes, matching every other decision in
-- this schema. That is reversible, which anonymizing deletion is not — and
-- whether a deployment needs the irreversible kind depends on whether it
-- faces personal-data erasure requests, which is a product decision rather
-- than an engineering one. Choosing it later is an added migration, not a
-- redesign.
ALTER TABLE users ADD COLUMN closed_at TIMESTAMPTZ;

COMMENT ON COLUMN users.closed_at IS
    'When the account holder closed this account. Null for every other reason it might be disabled.';

-- +goose Down

ALTER TABLE users DROP COLUMN closed_at;
