-- +goose Up

-- Attributes a tenant defines for itself.
--
-- 00007 added twenty-five attribute columns and said why they were columns:
-- they are a fixed set from a specification, so the database can constrain
-- them and sqlc can generate types for them. It also said what the other
-- case would need — "a key/value table is what tenant-defined attributes
-- would need, and that is a different feature" — and this is that feature.
-- The two live side by side rather than one replacing the other, because the
-- reasons that made columns right for a standard set are all still true.
--
-- What this is for: a tenant has a fact about its people that SCIM's schema
-- has no name for — a badge number, a contract end date, a site code — and
-- needs it to arrive from its directory and leave in a token. Without this,
-- the answer is to overload `costCenter` and hope nobody notices.

CREATE TABLE user_attribute_definitions (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- The stable identifier. It becomes a claim name, a mapping target, and
    -- a sub-attribute name, so it is constrained to what all three can carry
    -- and is immutable after creation: renaming it would silently stop a
    -- mapping that names it, in a system Portico does not own.
    --
    -- It must also not collide with a built-in catalogue entry, which this
    -- table cannot check because those live in Go. The service refuses it and
    -- a test holds that shut.
    key TEXT NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,38}[a-z0-9]$'),

    -- What an operator sees. The tenant's own words, in the tenant's own
    -- language, which is why it is stored rather than translated: a message
    -- catalogue cannot hold a string somebody types in.
    label       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    -- Five kinds, which is what a form needs to draw the right control and
    -- what validation needs to refuse the wrong value. No multi-valued kind:
    -- the thing people want multiple of is group membership, and that already
    -- exists as its own concept with its own screen.
    kind TEXT NOT NULL CHECK (kind IN ('TEXT', 'NUMBER', 'BOOLEAN', 'DATE', 'SELECT')),

    -- The permitted values, for SELECT. Empty for every other kind.
    allowed_values JSONB NOT NULL DEFAULT '[]',

    -- Required means an administrator's form will not save without it. It
    -- deliberately does not apply to a directory synchronization: a required
    -- attribute that no LDAP entry carries would turn one form rule into a
    -- refusal to import anybody.
    required BOOLEAN NOT NULL DEFAULT FALSE,

    sort_order INTEGER NOT NULL DEFAULT 0,

    -- Disabled hides it from forms and stops it being sent, and keeps every
    -- value already recorded. That is the ordinary way to retire one; DELETE
    -- exists too and discards the values with it, which is why it is not the
    -- ordinary way.
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_user_attribute_definitions_tenant_key UNIQUE (tenant_id, key)
);

CREATE INDEX idx_user_attribute_definitions_tenant
    ON user_attribute_definitions (tenant_id, status, sort_order);

COMMENT ON TABLE user_attribute_definitions IS
    'Tenant-defined user attributes. The fixed, specification-derived ones are columns on users; see migration 00007.';

-- How many a tenant may define is bounded, and the bound is enforced in the
-- service because it is a count rather than a row-level rule.
--
-- The reason is not storage. Every definition is a candidate for outbound
-- mapping, and a mapped attribute is bytes in an id_token — so an unbounded
-- number of definitions makes token size something a tenant chooses by
-- accident. Fifty is far past any real use and near enough to notice.


-- The values themselves.
--
-- One TEXT column for all five kinds, typed on the way in by the service and
-- rendered by kind on the way out. Five typed columns would buy
-- database-level typing at the cost of a five-way coalesce on every read.
--
-- The honest limit of that choice: a number stored as text cannot be sorted
-- or compared numerically in SQL, so filtering the user list by a numeric
-- custom attribute is not available and would need this revisited. Nothing
-- in this feature needs it — a value here exists to arrive from a directory
-- and to leave in a token.
CREATE TABLE user_attribute_values (
    tenant_id     TEXT NOT NULL REFERENCES tenants (id),
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    definition_id TEXT NOT NULL REFERENCES user_attribute_definitions (id) ON DELETE CASCADE,

    value TEXT NOT NULL,

    updated_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (user_id, definition_id)
);

-- Answers "who has filled this in", which is the question asked before
-- retiring a definition or mapping it outward.
CREATE INDEX idx_user_attribute_values_definition
    ON user_attribute_values (tenant_id, definition_id);

-- +goose Down
DROP TABLE user_attribute_values;
DROP TABLE user_attribute_definitions;
