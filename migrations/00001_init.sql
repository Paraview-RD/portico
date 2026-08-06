-- +goose Up

-- Organizations are a single flat tier in the MVP: no parent_id, no
-- hierarchy. See docs/requirements/mvp-requirements.md §3.4.
CREATE TABLE organizations (
    id          TEXT PRIMARY KEY,
    name        TEXT    NOT NULL,
    code        TEXT    NOT NULL UNIQUE,
    remark      TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

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

    -- A user belongs to at most one organization (§3.4.2). ON DELETE is not
    -- declared because organizations are disabled, never deleted.
    organization_id TEXT REFERENCES organizations (id),

    -- Bumped on logout, password change, and disable. A token carrying a
    -- stale value is rejected, which is how stateless JWTs are revoked
    -- immediately (§3.6).
    token_version INTEGER NOT NULL DEFAULT 1,

    -- How the account came to exist, for the registration log (§3.9).
    source TEXT NOT NULL DEFAULT 'ADMIN' CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION')),

    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_users_organization ON users (organization_id);
CREATE INDEX idx_users_created ON users (created_at);

-- All five log kinds from §3.9 share one table: they are queried the same
-- way (paged, filtered by time) and differ only in which columns are set.
CREATE TABLE audit_logs (
    id   TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('LOGIN', 'OPERATION', 'AUTH', 'REGISTRATION', 'ORGANIZATION')),

    -- A specific verb within the kind, e.g. LOGIN_SUCCESS, USER_CREATE.
    action TEXT NOT NULL,

    -- The actor's username is denormalized so the log stays readable, and so
    -- a failed login against a nonexistent account still records what was
    -- attempted.
    actor_id       TEXT,
    actor_username TEXT NOT NULL DEFAULT '',

    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',

    result  TEXT NOT NULL DEFAULT 'SUCCESS' CHECK (result IN ('SUCCESS', 'FAILURE')),
    detail  TEXT NOT NULL DEFAULT '',
    ip      TEXT NOT NULL DEFAULT '',

    created_at TEXT NOT NULL
);

CREATE INDEX idx_audit_logs_kind_created ON audit_logs (kind, created_at DESC);
CREATE INDEX idx_audit_logs_created ON audit_logs (created_at DESC);

-- Runtime-tunable settings (§3.10). Values are stored as text and parsed by
-- the settings service, which owns the defaults.
CREATE TABLE system_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE system_settings;
DROP TABLE audit_logs;
DROP TABLE users;
DROP TABLE organizations;
