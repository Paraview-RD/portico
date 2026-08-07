-- +goose Up

-- Tenants are the isolation boundary. Every other table carries a tenant_id
-- and no query crosses it: see docs/database-conventions.md, "Tenant
-- isolation", for how that is enforced and tested.
--
-- This table is the one exception — it is the root of the hierarchy, so it
-- has nothing to be scoped by. Only the provisioning CLI writes it; there is
-- deliberately no cross-tenant administrator role that could reach it over
-- HTTP (docs/requirements/v0.1-requirements.md §3.1).
CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE tenants IS 'Isolation boundary. Every other table is scoped to exactly one of these.';
COMMENT ON COLUMN tenants.code IS
    'What a user types at sign-in to say which tenant they belong to. Immutable once created, because it appears in sign-in URLs and downstream configuration.';
COMMENT ON COLUMN tenants.status IS 'DISABLED refuses sign-in without deleting anything.';

-- Organizations are a single flat tier: no parent_id, no hierarchy.
-- See docs/requirements/v0.1-requirements.md §3.7.
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT        NOT NULL REFERENCES tenants (id),
    name       TEXT        NOT NULL,
    code       TEXT        NOT NULL,
    remark     TEXT        NOT NULL DEFAULT '',
    status     TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    sort_order BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- Codes are unique within a tenant, not globally: two tenants
    -- independently naming an organization "SALES" is normal, and a global
    -- constraint would let one tenant's choices deny another's.
    UNIQUE (tenant_id, code),

    -- Redundant given the primary key, but it is what lets users declare a
    -- composite foreign key and so have the database itself refuse a
    -- cross-tenant membership.
    UNIQUE (tenant_id, id)
);

COMMENT ON TABLE organizations IS 'Flat groupings of users. No hierarchy in this version.';
COMMENT ON COLUMN organizations.code IS 'Stable identifier used by bulk import and downstream systems. Immutable once created.';
COMMENT ON COLUMN organizations.status IS 'DISABLED blocks new members but keeps existing ones.';

CREATE INDEX idx_organizations_sort ON organizations (tenant_id, sort_order, created_at);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants (id),
    username      TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    phone         TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'USER' CHECK (role IN ('SUPER_ADMIN', 'USER')),
    status        TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    -- A user belongs to at most one organization. ON DELETE is not declared
    -- because organizations are disabled, never deleted.
    --
    -- The reference is composite so that the database rejects a user in one
    -- tenant pointing at another tenant's organization. Application code
    -- would not normally construct such a row, but "would not normally" is
    -- not a guarantee, and this is the only place one can be had cheaply.
    organization_id TEXT,
    FOREIGN KEY (tenant_id, organization_id) REFERENCES organizations (tenant_id, id),

    token_version BIGINT  NOT NULL DEFAULT 1,
    source        TEXT    NOT NULL DEFAULT 'ADMIN' CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- Usernames are unique per tenant. Two tenants both having an "admin" is
    -- the expected case, which is also why sign-in has to be told which
    -- tenant it is for.
    UNIQUE (tenant_id, username)
);

COMMENT ON COLUMN users.token_version IS
    'Bumped on logout, password change, and disable. A token carrying a stale value is rejected, which is how a stateless JWT is revoked immediately.';
COMMENT ON COLUMN users.source IS 'How the account came to exist, for the registration log.';

CREATE INDEX idx_users_organization ON users (tenant_id, organization_id);
CREATE INDEX idx_users_created ON users (tenant_id, created_at);

-- All five log kinds share one table: they are queried identically (paged,
-- filtered by time) and differ only in which columns are populated.
CREATE TABLE audit_logs (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    kind      TEXT NOT NULL CHECK (kind IN ('LOGIN', 'OPERATION', 'AUTH', 'REGISTRATION', 'ORGANIZATION')),

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
COMMENT ON COLUMN audit_logs.tenant_id IS
    'The tenant the event happened in. A sign-in naming a tenant that does not exist has no tenant to belong to and so is reported to the process log instead; see SECURITY.md.';

CREATE INDEX idx_audit_logs_kind_created ON audit_logs (tenant_id, kind, created_at DESC);
CREATE INDEX idx_audit_logs_created ON audit_logs (tenant_id, created_at DESC);

-- Runtime-tunable settings, per tenant. Values are stored as text and parsed
-- by the settings service, which owns the defaults.
CREATE TABLE system_settings (
    tenant_id  TEXT        NOT NULL REFERENCES tenants (id),
    key        TEXT        NOT NULL,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (tenant_id, key)
);

-- +goose Down
DROP TABLE system_settings;
DROP TABLE audit_logs;
DROP TABLE users;
DROP TABLE organizations;
DROP TABLE tenants;
