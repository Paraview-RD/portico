-- +goose Up

-- Proves a signed-in user controls a phone number before it is written to
-- users.phone (self_service.go's RequestPhoneChange/ConfirmPhoneChange).
--
-- Tied to user_id rather than only phone, unlike sms_login_codes: a login
-- code verifies against whoever already holds a phone number, but this one
-- verifies a claim made by a specific, already-authenticated account about a
-- number nobody has bound yet -- there is no account to look the code up
-- against except the one that asked for it. Tenant-scoped like every other
-- table here.
CREATE TABLE phone_verification_codes (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    user_id   TEXT NOT NULL,

    -- Same format validateContactDetails already enforces on users.phone:
    -- digits only, optional leading '+'. The candidate number, not
    -- necessarily what ends up in users.phone -- it only gets there once
    -- ConfirmPhoneChange consumes this row.
    phone TEXT NOT NULL,

    -- sha256(code), not a password hash -- same reasoning as
    -- sms_login_codes.code_hash.
    code_hash TEXT NOT NULL,

    attempts    INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_phone_verification_codes_tenant_id UNIQUE (tenant_id, id)
);

-- The lookup ConfirmPhoneChange runs: the live code for one tenant's user,
-- for the specific phone they are trying to confirm.
CREATE INDEX idx_phone_verification_codes_tenant_user_phone
    ON phone_verification_codes (tenant_id, user_id, phone, created_at DESC);

-- RequestPhoneChange's per-account daily cap, same shape as
-- CountRecentPasswordResets.
CREATE INDEX idx_phone_verification_codes_tenant_user_created
    ON phone_verification_codes (tenant_id, user_id, created_at);

-- The retention sweep, same shape as DeleteExpiredSMSLoginCodes.
CREATE INDEX idx_phone_verification_codes_expires ON phone_verification_codes (expires_at);

COMMENT ON COLUMN phone_verification_codes.code_hash IS
    'sha256 of the 6-digit code. Not a password hash; see the CREATE TABLE comment.';

-- +goose Down
DROP TABLE phone_verification_codes;
