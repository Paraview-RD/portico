-- +goose Up

-- Organizations are a single flat tier: no parent_id, no hierarchy.
-- See docs/requirements/v0.1-requirements.md §3.7.
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,
    name       TEXT        NOT NULL,
    code       TEXT        NOT NULL UNIQUE,
    remark     TEXT        NOT NULL DEFAULT '',
    status     TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    sort_order BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE organizations IS 'Flat groupings of users. No hierarchy in this version.';
COMMENT ON COLUMN organizations.code IS 'Stable identifier used by bulk import and downstream systems. Immutable once created.';
COMMENT ON COLUMN organizations.status IS 'DISABLED blocks new members but keeps existing ones.';

CREATE INDEX idx_organizations_sort ON organizations (sort_order, created_at);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    phone         TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'USER' CHECK (role IN ('SUPER_ADMIN', 'USER')),
    status        TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    -- A user belongs to at most one organization. ON DELETE is not declared
    -- because organizations are disabled, never deleted.
    organization_id TEXT REFERENCES organizations (id),

    token_version BIGINT  NOT NULL DEFAULT 1,
    source        TEXT    NOT NULL DEFAULT 'ADMIN' CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

COMMENT ON COLUMN users.token_version IS
    'Bumped on logout, password change, and disable. A token carrying a stale value is rejected, which is how a stateless JWT is revoked immediately.';
COMMENT ON COLUMN users.source IS 'How the account came to exist, for the registration log.';

CREATE INDEX idx_users_organization ON users (organization_id);
CREATE INDEX idx_users_created ON users (created_at);

-- All five log kinds share one table: they are queried identically (paged,
-- filtered by time) and differ only in which columns are populated.
CREATE TABLE audit_logs (
    id   TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('LOGIN', 'OPERATION', 'AUTH', 'REGISTRATION', 'ORGANIZATION')),

    action TEXT NOT NULL,

    -- The actor's username is denormalized so the log stays readable, and so
    -- a failed sign-in against a nonexistent account still records what was
    -- attempted.
    actor_id       TEXT,
    actor_username TEXT NOT NULL DEFAULT '',

    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',

    result TEXT NOT NULL DEFAULT 'SUCCESS' CHECK (result IN ('SUCCESS', 'FAILURE')),
    detail TEXT NOT NULL DEFAULT '',
    ip     TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL
);

COMMENT ON COLUMN audit_logs.actor_id IS
    'Null when the actor could not be identified, such as a sign-in attempt against an unknown username.';

CREATE INDEX idx_audit_logs_kind_created ON audit_logs (kind, created_at DESC);
CREATE INDEX idx_audit_logs_created ON audit_logs (created_at DESC);

-- Runtime-tunable settings. Values are stored as text and parsed by the
-- settings service, which owns the defaults.
CREATE TABLE system_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE system_settings;
DROP TABLE audit_logs;
DROP TABLE users;
DROP TABLE organizations;
