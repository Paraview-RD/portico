-- +goose Up

-- Who is responsible for an organization.
--
-- A reference rather than a name, so it survives a rename, and composite so
-- the database refuses a manager from another tenant. Nullable: plenty of
-- organizations have nobody nominated, and inventing one would be worse than
-- leaving it blank.
--
-- It grants nothing. Being named here does not make somebody an
-- administrator, does not let them edit the organization, and is not
-- consulted by any authorization decision — this version has two fixed roles
-- and no permission model, and a field that quietly became a third role
-- would be the worst possible way to acquire one. It is a fact about the
-- organization chart, for display and for downstream systems that ask "who
-- runs this department".
ALTER TABLE organizations ADD COLUMN manager_id TEXT;
ALTER TABLE organizations ADD CONSTRAINT fk_organizations_manager
    FOREIGN KEY (tenant_id, manager_id) REFERENCES users (tenant_id, id);

CREATE INDEX idx_organizations_manager ON organizations (tenant_id, manager_id)
    WHERE manager_id IS NOT NULL;

COMMENT ON COLUMN organizations.manager_id IS
    'Who is responsible for this organization. Grants nothing: this version has two fixed roles, and a field that quietly became a third would be the worst way to acquire one.';


-- Additional organizations somebody is attached to.
--
-- **The primary membership does not move.** users.organization_id is still
-- the one authoritative answer to "where does this person sit": it is what
-- SCIM and the directory sync write, what an export names, and what places
-- them in the tree. These are attachments beside it — the case of somebody
-- in the platform team who also sits on a project — and they are advisory.
--
-- They grant nothing and synchronize nothing, for the same reason group
-- membership grants nothing: this version has no permission model to attach
-- them to, and a table that quietly became one would be a permission system
-- nobody designed.
--
-- Why this is not simply a group: a group is a flat set with a name, and an
-- attachment is a position in the tree with a code that downstream systems
-- already store. Somebody asking "which departments is this person involved
-- with" wants the second, and answering with the first would lose the
-- hierarchy that makes the question meaningful.
--
-- The distinction the documentation draws between an organization and a
-- group therefore survives, with one line added: primary membership is still
-- exactly one, and attachments are any number.
CREATE TABLE user_organization_attachments (
    tenant_id       TEXT NOT NULL REFERENCES tenants (id),
    user_id         TEXT NOT NULL,
    organization_id TEXT NOT NULL,

    -- Composite, so neither side can point outside the tenant.
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),
    FOREIGN KEY (tenant_id, organization_id) REFERENCES organizations (tenant_id, id),

    created_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (tenant_id, user_id, organization_id)
);

COMMENT ON TABLE user_organization_attachments IS
    'Additional organizations a person is attached to. Advisory: grants nothing, synchronizes nowhere, and does not change users.organization_id, which remains the one authoritative membership.';

CREATE INDEX idx_user_org_attachments_org
    ON user_organization_attachments (tenant_id, organization_id);

-- +goose Down

DROP TABLE user_organization_attachments;
DROP INDEX IF EXISTS idx_organizations_manager;
ALTER TABLE organizations DROP CONSTRAINT fk_organizations_manager;
ALTER TABLE organizations DROP COLUMN manager_id;
