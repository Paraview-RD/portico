-- +goose Up

-- The OTP itself, for the standalone phone+code login (§2, §3.1 of
-- docs/superpowers/specs/2026-09-21-sms-otp-login-design.md). Tenant-scoped
-- like every other table here: a code is issued for one tenant's account
-- and must not verify against another tenant's. The abuse-prevention
-- counters (cooldown, per-phone/IP/deployment daily caps) deliberately do
-- NOT live here — see the spec's §3.2 for why they are in-memory instead of
-- a query that would have to skip the tenant_id filter every other query on
-- a scoped table carries.
CREATE TABLE sms_login_codes (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- Same format validateContactDetails already enforces on users.phone:
    -- digits only, optional leading '+'.
    phone TEXT NOT NULL,

    -- sha256(code), not a password hash: the code is 6 digits by design,
    -- there is nothing here for a slow hash to protect against that the
    -- attempt counter and the 5-minute TTL do not already bound. Hashing
    -- only keeps the code out of plaintext at rest.
    code_hash TEXT NOT NULL,

    attempts    INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    ip          TEXT NOT NULL,

    CONSTRAINT uq_sms_login_codes_tenant_id UNIQUE (tenant_id, id)
);

-- The lookup verification runs: the live code for one tenant's phone number.
CREATE INDEX idx_sms_login_codes_tenant_phone ON sms_login_codes (tenant_id, phone, created_at DESC);

-- The retention sweep, same shape as DeleteExpiredPasswordResets.
CREATE INDEX idx_sms_login_codes_expires ON sms_login_codes (expires_at);

COMMENT ON COLUMN sms_login_codes.code_hash IS
    'sha256 of the 6-digit code. Not a password hash; see the CREATE TABLE comment.';

-- +goose Down
DROP TABLE sms_login_codes;
