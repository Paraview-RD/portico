-- +goose Up

-- The attributes a person has beyond a name and a way to sign in.
--
-- Taken from SCIM 2.0's core User schema (RFC 7643 §4.1) and its enterprise
-- extension (§4.3), rather than invented. Portico is already a SCIM server,
-- so using those names means a directory's fields land where they belong,
-- the meaning of each is settled by a specification rather than by this
-- project, and adding a second directory later does not reopen the question.
--
-- Columns rather than a key/value table. This is a fixed set from a
-- standard: the database can constrain it, an index can be built on it, and
-- sqlc generates types for it. A key/value table is what tenant-defined
-- attributes would need, and that is a different feature — the two cannot be
-- swapped for one another afterwards, which is why the choice is recorded
-- here and in the requirements rather than left implicit.
--
-- Every one is optional and defaults to empty. An account that had only a
-- username and a display name before this migration is a complete account
-- after it.
--
-- Deliberately absent: SCIM's `organization` and `division`, which are free
-- text. This system has an organization tree, and a hand-typed string beside
-- it drifts — and when the two disagree nobody can say which to believe.

-- --- Name, in the parts a directory actually sends (RFC 7643 §4.1.1) ------
--
-- displayName already exists and stays the one thing every screen shows. The
-- parts are for systems that need them separately, and for the far-from-rare
-- case of a directory that has no single display name to give.
ALTER TABLE users ADD COLUMN name_formatted     TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN family_name        TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN given_name         TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN middle_name        TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN honorific_prefix   TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN honorific_suffix   TEXT NOT NULL DEFAULT '';

-- --- How they are shown and reached --------------------------------------
ALTER TABLE users ADD COLUMN nick_name   TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN profile_url TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN photo_url   TEXT NOT NULL DEFAULT '';

-- --- Job and preferences -------------------------------------------------
ALTER TABLE users ADD COLUMN title              TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN user_type          TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN preferred_language TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN locale             TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN timezone           TEXT NOT NULL DEFAULT '';

-- --- Address, in SCIM's six parts (§4.1.2) -------------------------------
ALTER TABLE users ADD COLUMN address_formatted TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN street_address    TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN locality          TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN region            TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN postal_code       TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN country           TEXT NOT NULL DEFAULT '';

-- --- Enterprise extension (§4.3) -----------------------------------------
ALTER TABLE users ADD COLUMN employee_number TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN cost_center     TEXT NOT NULL DEFAULT '';
-- Free text, and distinct from organization_id on purpose: a directory often
-- sends a department string that does not correspond to anything in this
-- tenant's tree, and dropping it would lose information an operator can use
-- to place the person later.
ALTER TABLE users ADD COLUMN department TEXT NOT NULL DEFAULT '';

-- The person this one reports to.
--
-- A reference rather than a name, so it survives a rename — the same reason
-- everything else here points at an id. Composite, so the database refuses a
-- manager in another tenant.
--
-- No check against reporting cycles. One would need a recursive query on
-- every write, and unlike the organization tree — where a cycle makes the
-- structure unrenderable — a cycle here is a data-quality problem in
-- somebody's HR system that Portico is reflecting rather than causing.
ALTER TABLE users ADD COLUMN manager_id TEXT;
ALTER TABLE users ADD CONSTRAINT fk_users_manager
    FOREIGN KEY (tenant_id, manager_id) REFERENCES users (tenant_id, id);

CREATE INDEX idx_users_manager ON users (tenant_id, manager_id)
    WHERE manager_id IS NOT NULL;

-- Employee numbers are unique within a tenant where they are set. They are
-- how an HR system names a person, so two accounts claiming one is a
-- reconciliation error worth refusing rather than storing. Partial, because
-- empty means "not recorded" and any number of accounts may leave it so.
CREATE UNIQUE INDEX uq_users_tenant_employee_number
    ON users (tenant_id, employee_number) WHERE employee_number <> '';

COMMENT ON COLUMN users.department IS
    'Free text as a directory sends it. Distinct from organization_id, which is this tenant''s own tree; keeping both loses nothing and lets an operator place somebody later.';
COMMENT ON COLUMN users.manager_id IS
    'Who this person reports to. Not checked for cycles: one is a data-quality problem in the source system rather than something this schema can prevent.';

-- +goose Down

DROP INDEX IF EXISTS uq_users_tenant_employee_number;
DROP INDEX IF EXISTS idx_users_manager;
ALTER TABLE users DROP CONSTRAINT fk_users_manager;
ALTER TABLE users
    DROP COLUMN name_formatted, DROP COLUMN family_name, DROP COLUMN given_name,
    DROP COLUMN middle_name, DROP COLUMN honorific_prefix, DROP COLUMN honorific_suffix,
    DROP COLUMN nick_name, DROP COLUMN profile_url, DROP COLUMN photo_url,
    DROP COLUMN title, DROP COLUMN user_type, DROP COLUMN preferred_language,
    DROP COLUMN locale, DROP COLUMN timezone,
    DROP COLUMN address_formatted, DROP COLUMN street_address, DROP COLUMN locality,
    DROP COLUMN region, DROP COLUMN postal_code, DROP COLUMN country,
    DROP COLUMN employee_number, DROP COLUMN cost_center, DROP COLUMN department,
    DROP COLUMN manager_id;
