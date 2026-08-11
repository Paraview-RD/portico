-- +goose Up

-- How often a directory synchronizes itself, and when it was last tried.
--
-- Until now synchronization was a button, and the documented workaround for
-- wanting it nightly was a cron job calling the API — which needs an access
-- token, which expires, which means an administrator's password sitting in
-- the cron environment. That is a worse credential to leave lying about than
-- the bind password this table encrypts. A scheduler inside the process
-- needs no credential at all, and removing that trade is most of why these
-- two columns exist.
--
-- Zero means no automatic synchronization, and it is the default, so
-- upgrading changes the behaviour of exactly nothing. Automatic reads of
-- somebody's directory are opted into, never inherited.
--
-- The floor is enforced here as well as in the service because a full
-- enumeration is the only kind there is: working out who has disappeared
-- requires listing everybody, so a one-minute interval would be a
-- self-inflicted denial of service against the directory rather than a
-- fresher one.
ALTER TABLE ldap_sources
    ADD COLUMN sync_interval_minutes INTEGER NOT NULL DEFAULT 0
        CHECK (sync_interval_minutes = 0 OR sync_interval_minutes BETWEEN 15 AND 10080);

-- When a scheduled run was last *attempted*, which is deliberately not
-- last_synced_at.
--
-- last_synced_at records success and is what an operator reads to answer "is
-- this still running"; writing it on every attempt would have a directory
-- that has been failing for a week reporting that it synchronized two
-- minutes ago. So the schedule keeps its own timestamp, and the two answer
-- different questions.
--
-- It doubles as the claim. Advancing it is how one instance takes a
-- directory, and it is written before the directory is contacted rather than
-- after the run finishes: a failing source that stayed due would be retried
-- on every tick, so a misconfigured one would hammer somebody's AD every
-- minute. Waiting out the interval after a failure is the intended
-- behaviour, not an oversight.
ALTER TABLE ldap_sources
    ADD COLUMN last_sync_attempt_at TIMESTAMPTZ;

COMMENT ON COLUMN ldap_sources.last_sync_attempt_at IS
    'When a scheduled run was last claimed, success or not. See last_synced_at for the last one that worked.';

-- +goose Down
ALTER TABLE ldap_sources DROP COLUMN last_sync_attempt_at;
ALTER TABLE ldap_sources DROP COLUMN sync_interval_minutes;
