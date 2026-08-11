-- name: CreateLDAPSource :exec
INSERT INTO ldap_sources (
    id, tenant_id, name, host, port, encryption,
    bind_dn, bind_password, base_dn, user_filter,
    attr_username, attr_display_name, attr_email, attr_phone, attr_external_id,
    organization_id, status, sync_interval_minutes, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
);

-- name: ListLDAPSources :many
SELECT * FROM ldap_sources WHERE tenant_id = $1 ORDER BY created_at;

-- name: GetLDAPSource :one
SELECT * FROM ldap_sources WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- The bind password is not in the SET list. It is rotated through its own
-- statement so that an update which omits it leaves it alone — a form that
-- cannot show the current value must not be able to blank it by submitting.
-- name: UpdateLDAPSource :exec
UPDATE ldap_sources
SET name = $1,
    host = $2,
    port = $3,
    encryption = $4,
    bind_dn = $5,
    base_dn = $6,
    user_filter = $7,
    attr_username = $8,
    attr_display_name = $9,
    attr_email = $10,
    attr_phone = $11,
    attr_external_id = $12,
    organization_id = $13,
    sync_interval_minutes = $14,
    updated_at = $15
WHERE tenant_id = $16 AND id = $17;

-- name: UpdateLDAPSourceBindPassword :exec
UPDATE ldap_sources
SET bind_password = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: UpdateLDAPSourceStatus :exec
UPDATE ldap_sources
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;

-- name: MarkLDAPSourceSynced :exec
UPDATE ldap_sources
SET last_synced_at = $1, updated_at = $1
WHERE tenant_id = $2 AND id = $3;

-- name: ClaimNextDueLDAPSource :one
-- One directory whose interval has elapsed, claimed by the act of asking.
--
-- The claim is the same write that records the attempt. Two instances of
-- Portico reach this every minute; SKIP LOCKED means the second takes a
-- different row rather than waiting for the first, and the attempt timestamp
-- means neither takes the same directory twice in one interval. So a
-- directory is enumerated once per interval however many instances are
-- running, without a lease table, a leader election, or a status column that
-- a crash could leave stuck.
--
-- Deliberately not a status flag written in advance, for the same reason the
-- webhook dispatcher avoids one: a claim that outlives a crash is a claim
-- nothing will ever release.
--
-- One row rather than every due row, although the caller loops and would
-- happily take a batch. A synchronization takes as long as reading somebody's
-- directory takes, so claiming ten and then working through them means nine
-- claims recording an attempt that has not happened yet — and a process
-- stopped halfway would leave those nine with their schedules advanced and no
-- run to show for it. Claimed immediately before it is read, a directory the
-- pass never reached is simply still due.
--
-- Interval arithmetic in SQL rather than in Go because the interval is a
-- property of each row, so the comparison cannot be a single cutoff computed
-- by the caller.
--
-- Oldest first, nulls before those: a directory that has never been read is
-- the one somebody has just configured and is waiting on.
UPDATE ldap_sources
SET last_sync_attempt_at = $1
WHERE ldap_sources.tenant_id = $2
  AND ldap_sources.id = (
      SELECT due.id FROM ldap_sources AS due
      WHERE due.tenant_id = $2
        AND due.status = 'ACTIVE'
        AND due.sync_interval_minutes > 0
        AND (
            due.last_sync_attempt_at IS NULL
            OR due.last_sync_attempt_at + make_interval(mins => due.sync_interval_minutes) <= $1
        )
      ORDER BY due.last_sync_attempt_at ASC NULLS FIRST
      LIMIT 1
      FOR UPDATE SKIP LOCKED
  )
RETURNING ldap_sources.id;

-- name: StartLDAPSyncRun :exec
INSERT INTO ldap_sync_runs (
    id, tenant_id, source_id, actor_name, started_at, outcome
) VALUES ($1, $2, $3, $4, $5, 'RUNNING');

-- name: FinishLDAPSyncRun :exec
UPDATE ldap_sync_runs
SET finished_at = $1,
    outcome = $2,
    created_count = $3,
    updated_count = $4,
    deactivated_count = $5,
    skipped_count = $6,
    skipped_detail = $7,
    error_code = $8,
    error = $9
WHERE tenant_id = $10 AND id = $11;

-- name: ListLDAPSyncRuns :many
SELECT * FROM ldap_sync_runs
WHERE tenant_id = $1 AND source_id = $2
ORDER BY started_at DESC
LIMIT $3;

-- Every account this directory owns, which is what a sync compares against
-- to work out what has vanished from it.
-- name: ListUsersFromLDAPSource :many
SELECT * FROM users
WHERE tenant_id = $1 AND ldap_source_id = $2;

-- name: BindUserToLDAPSource :exec
UPDATE users
SET ldap_source_id = $1, updated_at = $2
WHERE tenant_id = $3 AND id = $4;
