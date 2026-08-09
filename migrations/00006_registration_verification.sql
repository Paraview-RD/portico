-- +goose Up

-- When a self-registered account proved it owns the address it gave.
--
-- Registration until now created a usable account immediately, and the email
-- on it was whatever was typed. That address is both a sign-in identifier and
-- where a password-reset link is sent, so an unverified one means somebody
-- can open an account under a colleague's address and receive their reset
-- links. Fine on a closed intranet, which is the only place self-registration
-- has been usable; not fine the moment it faces outward.
--
-- A timestamp rather than a status value, for the same reason closed_at is
-- one: `status` is shared with organizations and application registrations,
-- neither of which has any notion of a pending address, and "when did this
-- happen" is a timestamp.
ALTER TABLE users ADD COLUMN verified_at TIMESTAMPTZ;

COMMENT ON COLUMN users.verified_at IS
    'When a self-registered account''s contact address was accepted — either proven by a link, or accepted at registration because the tenant did not require proof. Null means the account still has to prove it. Only consulted for source = REGISTRATION.';

-- Everybody who already registered is grandfathered.
--
-- The service applies the same rule going forward: an account registered
-- while the requirement is off is marked accepted at that moment. So this is
-- not a one-off patch over history — it is the same rule, applied once to
-- the rows that predate the column.
--
-- This is the line that stops turning the feature on from locking out every
-- existing member of a deployment that has been running with open
-- registration. They were accepted under the rules in force at the time, and
-- a policy change is not grounds for revoking access somebody already has —
-- an administrator who wants that can disable the accounts deliberately.
--
-- Only REGISTRATION rows are touched. An administrator-created, imported, or
-- directory-synchronized account is vouched for by whoever created it and is
-- never gated on this column, so a value there would be noise that a later
-- reader would have to work out the meaning of.
UPDATE users SET verified_at = created_at WHERE source = 'REGISTRATION';


-- An outstanding request to prove an address.
--
-- Deliberately its own table rather than a reuse of password_resets. The two
-- look alike and mean opposite things: a reset token is a password
-- equivalent that lets somebody in, and a verification token only marks an
-- address as proven. Sharing a table would make it one query's mistake away
-- from a token issued for one purpose being redeemed for the other.
CREATE TABLE registration_verifications (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    user_id   TEXT NOT NULL,

    -- Composite, so a row cannot point at another tenant's account.
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    -- The SHA-256 of the token, never the token — same reasoning as
    -- password_resets, and the same lack of a work factor: the value is
    -- high-entropy and random, so there is no dictionary for slow hashing
    -- to defend against.
    token_hash TEXT NOT NULL UNIQUE,

    channel TEXT NOT NULL CHECK (channel IN ('EMAIL', 'SMS')),

    expires_at TIMESTAMPTZ NOT NULL,
    -- Set when spent, and also when a newer request supersedes it, so only
    -- the most recent message works.
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE registration_verifications IS
    'Outstanding address-verification requests from self-registration. Rows survive use as part of the trail; expiry and used_at are what make a token unusable, not deletion.';

CREATE INDEX idx_registration_verifications_user
    ON registration_verifications (tenant_id, user_id);

-- +goose Down

DROP TABLE registration_verifications;
ALTER TABLE users DROP COLUMN verified_at;
