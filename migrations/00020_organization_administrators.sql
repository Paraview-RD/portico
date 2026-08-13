-- +goose Up

-- Who would administer an organization, recorded before anything can act on
-- it.
--
-- This grants nothing today, and that is the point rather than a caveat.
-- Delegated administration is a planned feature; what a planned feature
-- cannot do is invent, on the day it ships, the facts it needed to have been
-- collecting all along. An organization chart is entered by people over
-- months. If the place to write "Zhang administers Engineering" does not
-- exist until the permission model does, then the permission model arrives
-- to an empty table and every customer starts by re-entering what they
-- already told somebody.
--
-- So: the shape now, the enforcement later. Two columns here exist only for
-- the later half and are worth defending, because both are impossible to
-- reconstruct after the fact.
--
--   scope       The question delegated administration will actually ask is
--               "does this person's authority reach organization Y", and
--               "this organization" and "this organization and everything
--               under it" are different answers. A row that did not record
--               which was meant is a row whose intent is unrecoverable — the
--               feature would ship and every existing assignment would have
--               to be guessed at or re-entered. NOT NULL, no default: the
--               person assigning has to say.
--
--   granted_by  This will be a privilege grant. Provenance can only be
--               written down as it happens, and an audit that cannot say who
--               conferred an authority is not an audit. The audit log
--               records it too; this column is what makes the row itself
--               answerable.
--
-- A table rather than a column on organizations, because both directions are
-- many: an organization may have several administrators, and a person may
-- administer several organizations. organizations.manager_id stays as it is
-- and means something else — who is *responsible* for a department, a fact
-- about the chart. Somebody may well be both, and neither implies the other.
--
-- Nothing reads this for an authorization decision, and
-- TestOrganizationAdministratorGrantsNothing fails if that changes: it
-- assigns an ordinary account and requires every administrative action in
-- that organization to be refused exactly as before. When delegated
-- administration is built, that test is the thing to come and edit
-- deliberately — which is the whole reason it is written that way.
CREATE TABLE organization_administrators (
    tenant_id       TEXT NOT NULL REFERENCES tenants (id),
    organization_id TEXT NOT NULL,
    user_id         TEXT NOT NULL,

    -- Composite, so neither side can point outside the tenant. The database
    -- refuses a cross-tenant assignment rather than trusting that the
    -- application would not build one.
    FOREIGN KEY (tenant_id, organization_id) REFERENCES organizations (tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    -- SELF is this organization only; SUBTREE is it and every descendant.
    -- Two values, deliberately: a third dimension — "may manage people but
    -- not the structure" — would be a permission model being designed here,
    -- one row at a time, without the judgement that belongs with it.
    scope TEXT NOT NULL CHECK (scope IN ('SELF', 'SUBTREE')),

    granted_by TEXT NOT NULL,
    FOREIGN KEY (tenant_id, granted_by) REFERENCES users (tenant_id, id),
    granted_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (tenant_id, organization_id, user_id)
);

COMMENT ON TABLE organization_administrators IS
    'Who would administer an organization once delegated administration exists. Grants nothing today: no authorization decision reads it, and a test enforces that. The scope and granted_by columns are recorded now because neither can be reconstructed later.';

COMMENT ON COLUMN organization_administrators.scope IS
    'SELF is this organization only; SUBTREE is it and every descendant. Required, because a row that does not say which was meant cannot be interpreted when the feature that reads it arrives.';

-- The query delegated administration will make on every request: which
-- organizations does this person administer. Indexed from the day the rows
-- exist, so the answer does not have to be a table scan the first time
-- anything asks.
CREATE INDEX idx_organization_administrators_user
    ON organization_administrators (tenant_id, user_id);

-- +goose Down

DROP INDEX IF EXISTS idx_organization_administrators_user;
DROP TABLE organization_administrators;
