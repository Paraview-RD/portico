-- +goose Up

-- The one child table whose links did not carry the tenant they belong to.
--
-- docs/database-conventions.md states the rule, and twenty-four foreign keys
-- in this schema follow it: a child row stores tenant_id and references its
-- parent by (tenant_id, id), so a link that crosses a tenant is refused by
-- the database rather than left to whichever query is written next. That is
-- why organizations, users, groups and the rest carry an otherwise-redundant
-- UNIQUE (tenant_id, id).
--
-- user_attribute_values was the exception. It stored tenant_id and referenced
-- users (id) and user_attribute_definitions (id) alone, so nothing connected
-- the tenant on the row to the tenant its user actually belongs to. A row
-- claiming one tenant while holding another's user was accepted.
--
-- Nothing produced one. Every write takes tenant_id from the bound scope and
-- the user id from a path already resolved inside that tenant, and the
-- delete names all three columns. So this repairs a missing backstop rather
-- than a leak — but it was the single place in the schema where the
-- invariant rested on the query layer alone, which is precisely the shape of
-- mistake that survives review: the wrong row looks exactly like the right
-- one.
--
-- If this migration fails on a live database, it has found data that is
-- already inconsistent, and stopping is the correct outcome.

-- The parent needs the composite key to be referenced by one. Redundant
-- against the primary key on its own, and not redundant as a target: a
-- foreign key can only point at a unique constraint over exactly its columns.
ALTER TABLE user_attribute_definitions
    ADD CONSTRAINT uq_user_attribute_definitions_tenant_id UNIQUE (tenant_id, id);

ALTER TABLE user_attribute_values
    DROP CONSTRAINT user_attribute_values_user_id_fkey,
    DROP CONSTRAINT user_attribute_values_definition_id_fkey,
    -- ON DELETE CASCADE on both, as before: an attribute value is part of the
    -- account it describes, and of the question it answers. Neither outliving
    -- its parent is the behaviour this replaces, not a change of mind.
    ADD CONSTRAINT user_attribute_values_user_fkey
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES users (tenant_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT user_attribute_values_definition_fkey
        FOREIGN KEY (tenant_id, definition_id)
        REFERENCES user_attribute_definitions (tenant_id, id) ON DELETE CASCADE;

-- The primary key is deliberately left alone at (user_id, definition_id).
--
-- Widening it to include tenant_id was the first idea and it is the wrong
-- one: it would permit the same user and definition to appear twice under
-- different tenant_ids, which is weaker than what is there now — and with the
-- foreign keys above, that pair already cannot span tenants. It would also
-- break the ON CONFLICT target that SetUserAttributeValue upserts on.

COMMENT ON CONSTRAINT user_attribute_values_user_fkey ON user_attribute_values IS
    'Composite on purpose: a value cannot describe an account in another tenant. See docs/database-conventions.md.';

-- +goose Down

ALTER TABLE user_attribute_values
    DROP CONSTRAINT user_attribute_values_user_fkey,
    DROP CONSTRAINT user_attribute_values_definition_fkey,
    ADD CONSTRAINT user_attribute_values_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    ADD CONSTRAINT user_attribute_values_definition_id_fkey
        FOREIGN KEY (definition_id)
        REFERENCES user_attribute_definitions (id) ON DELETE CASCADE;

ALTER TABLE user_attribute_definitions
    DROP CONSTRAINT uq_user_attribute_definitions_tenant_id;
