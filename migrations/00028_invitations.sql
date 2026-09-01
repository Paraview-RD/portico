-- +goose Up

-- Invitation-gated registration: an administrator hands out a code, and
-- self-registration proceeds without administrator involvement in every
-- other respect (see docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md).
--
-- status is ACTIVE/DISABLED only, exactly like oauth_clients and
-- organizations. "Exhausted" (used_count reached quota) and "expired" (past
-- expires_at) are deliberately not stored here — they are computed at
-- validation time from used_count/quota and expires_at, which are the
-- authoritative values. A third and fourth status value would be a second
-- copy of the same fact, kept in sync by nothing.
CREATE TABLE invitations (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What somebody types on the registration form. Unique per tenant, not
    -- globally, on the same grounds as oauth_clients.client_id: two tenants
    -- independently choosing "WELCOME2026" is normal.
    code TEXT NOT NULL,

    -- Optional pre-assigned organization and groups, applied to the new
    -- account in the same transaction that redeems the code — see the ADR
    -- for why this bypasses the administrator-assignment path that
    -- ordinary registration leaves for later. Composite FK so the database
    -- refuses an organization from another tenant.
    organization_id TEXT,
    FOREIGN KEY (tenant_id, organization_id) REFERENCES organizations (tenant_id, id),

    -- Groups have no status column of their own (see the groups table), so
    -- there is nothing to reference with a foreign key on an array column
    -- anyway; existence and tenancy are checked in the service layer at
    -- creation time and again at redemption time.
    group_ids TEXT[] NOT NULL DEFAULT '{}',

    quota      INTEGER NOT NULL CHECK (quota > 0),
    used_count INTEGER NOT NULL DEFAULT 0 CHECK (used_count >= 0),

    -- NULL means never expires.
    expires_at TIMESTAMPTZ,

    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_invitations_tenant_code UNIQUE (tenant_id, code),

    -- Redundant given the primary key, but it is what would let a future
    -- table declare a composite foreign key against this one.
    CONSTRAINT uq_invitations_tenant_id UNIQUE (tenant_id, id)
);

CREATE INDEX idx_invitations_tenant ON invitations (tenant_id, created_at);

COMMENT ON COLUMN invitations.used_count IS
    'Incremented atomically by the RedeemInvitation query, in the same transaction as the account it pays for. Never updated any other way.';
COMMENT ON COLUMN invitations.status IS
    'ACTIVE or DISABLED only. Exhausted and expired are derived at validation time from used_count/quota and expires_at, not stored here.';

-- +goose Down
DROP TABLE invitations;
