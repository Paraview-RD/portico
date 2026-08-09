-- +goose Up

-- A directory Portico reads accounts out of.
--
-- This runs opposite to everything else that brings accounts in. SCIM is a
-- server here: a directory pushes into /scim/v2 and Portico never reaches
-- out. LDAP is the other direction — Portico connects to somebody's AD or
-- OpenLDAP on a schedule and pulls. Same destination, opposite initiative,
-- and worth keeping straight because the failure modes differ: a push that
-- stops is silent at this end, while a pull that stops has a failed run to
-- point at.
--
-- Reconciliation is on external_id, exactly as SCIM does it, so an account
-- that arrives from a directory has one identity here however it was
-- delivered and a rename stays a rename rather than becoming a second
-- account. That is also why the attribute holding it is configuration: AD
-- has objectGUID, OpenLDAP has entryUUID, and a deployment that picks the
-- wrong one gets duplicate accounts on the first rename rather than an
-- error.
CREATE TABLE ldap_sources (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What an operator recognises it by: "Head office AD", "Contractors".
    name TEXT NOT NULL,

    host TEXT NOT NULL,
    port INTEGER NOT NULL,

    -- How the connection is protected. Plain LDAP is permitted because
    -- plenty of directories sit on a network segment where it is the
    -- deployed reality, and refusing it would only move the integration to
    -- a script nobody reviews. The console says what it costs.
    encryption TEXT NOT NULL CHECK (encryption IN ('none', 'starttls', 'tls')),

    -- The service account. An empty bind_dn is an anonymous bind, which some
    -- read-only directories allow.
    bind_dn TEXT NOT NULL DEFAULT '',

    -- Sealed by internal/secrets, never plaintext, and never returned by the
    -- API. This is the one credential in the schema that has to be
    -- recoverable rather than merely checkable: Portico sends it on every
    -- sync. A deployment with no PORTICO_ENCRYPTION_KEY cannot write a
    -- non-empty value here at all — the service refuses rather than storing
    -- it in the clear.
    bind_password TEXT NOT NULL DEFAULT '',

    -- Where the search starts and what it matches.
    base_dn     TEXT NOT NULL,
    user_filter TEXT NOT NULL,

    -- Which attribute carries which fact. No defaults in the schema: AD and
    -- OpenLDAP disagree on every one of them, so a wrong guess would import
    -- a directory's worth of accounts named after the wrong field.
    attr_username     TEXT NOT NULL,
    attr_display_name TEXT NOT NULL,
    attr_email        TEXT NOT NULL DEFAULT '',
    attr_phone        TEXT NOT NULL DEFAULT '',
    attr_external_id  TEXT NOT NULL,

    -- Accounts pulled from here land in this organization when it is set.
    -- Optional: a directory that carries no organization structure Portico
    -- understands should not force one to be invented.
    organization_id TEXT REFERENCES organizations (id),

    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    -- Answers "is this still running", which is what somebody asks when a
    -- directory has quietly stopped being the source of truth.
    last_synced_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_ldap_sources_tenant_name UNIQUE (tenant_id, name)
);

CREATE INDEX idx_ldap_sources_tenant ON ldap_sources (tenant_id, status);


-- What one synchronization actually did.
--
-- The reason this table exists rather than a last_result column: the
-- question asked after a directory integration misbehaves is never "what is
-- the state now", it is "when did this start". A single overwritten result
-- cannot answer that, and the answer is usually visible as the moment the
-- deactivated count jumped.
CREATE TABLE ldap_sync_runs (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    source_id TEXT NOT NULL REFERENCES ldap_sources (id),

    -- Who asked. Empty for the scheduler, which is not a user and must not
    -- be recorded as one.
    actor_name TEXT NOT NULL DEFAULT '',

    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,

    outcome TEXT NOT NULL CHECK (outcome IN ('RUNNING', 'SUCCEEDED', 'FAILED')),

    created_count     INTEGER NOT NULL DEFAULT 0,
    updated_count     INTEGER NOT NULL DEFAULT 0,
    deactivated_count INTEGER NOT NULL DEFAULT 0,
    -- Entries the directory returned that could not become an account: no
    -- username, no external id, a username another source already owns.
    -- Counted rather than failing the run, because one malformed entry in
    -- ten thousand should not stop the other nine thousand nine hundred.
    skipped_count INTEGER NOT NULL DEFAULT 0,

    -- Why it failed, in the operator's words rather than a stack trace.
    --
    -- Two columns, because the failures have two origins. A refusal Portico
    -- decided on has a code the console can render in the reader's language.
    -- An error the directory reported does not: it is the LDAP server's own
    -- wording, and translating "No Such Object" would take away the string
    -- an operator is going to paste into a search engine. So the code is
    -- empty for those and the console shows the text as it arrived.
    error_code TEXT NOT NULL DEFAULT '',
    error      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_ldap_sync_runs_source ON ldap_sync_runs (tenant_id, source_id, started_at DESC);


-- Which directory an account came from, and what it is called there.
--
-- external_id already exists on users and is what reconciliation matches on.
-- This says which source that id belongs to, because two directories may
-- legitimately hand out the same opaque identifier and because an account
-- that a directory owns must not be quietly re-owned by another one.
--
-- Null for everybody else: an administrator's account, a self-registration,
-- an import. Those are Portico's own and no sync may deactivate them.
ALTER TABLE users
    ADD COLUMN ldap_source_id TEXT REFERENCES ldap_sources (id);

COMMENT ON COLUMN users.ldap_source_id IS
    'The directory that owns this account, or null when Portico does.';

-- A fifth way for an account to exist.
--
-- Distinguished from SCIM although both mean "a directory owns this",
-- because what an operator does about them differs: a SCIM account stopped
-- arriving because the directory stopped pushing and there is nothing here
-- to look at, while an LDAP account stopped arriving because a run here
-- failed and there is a run record that says why.
ALTER TABLE users DROP CONSTRAINT users_source_check;
ALTER TABLE users ADD CONSTRAINT users_source_check
    CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION', 'SCIM', 'LDAP'));

CREATE INDEX idx_users_ldap_source ON users (ldap_source_id)
    WHERE ldap_source_id IS NOT NULL;

-- +goose Down

ALTER TABLE users DROP CONSTRAINT users_source_check;
ALTER TABLE users ADD CONSTRAINT users_source_check
    CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION', 'SCIM'));

DROP INDEX IF EXISTS idx_users_ldap_source;
ALTER TABLE users DROP COLUMN ldap_source_id;
DROP TABLE ldap_sync_runs;
DROP TABLE ldap_sources;
