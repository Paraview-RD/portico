-- +goose Up

-- An account that must replace its password before it can be used.
--
-- This exists because a shipped release now creates its first administrator
-- with a documented default password rather than a random one printed to
-- stderr. The random password was safer in one narrow sense and much worse
-- in every practical one: it appeared exactly once, in a stream that a
-- container runtime may not have kept, and an operator who missed it had no
-- supported way back in. A documented default is one anybody can look up —
-- which is precisely why the account has to be unusable until it is changed.
--
-- Deliberately not carried by password_changed_at. That column feeds the
-- expiry policy, which is off by default (max age 0), so a NULL there forces
-- nothing; and widening "never changed" to mean "must change" would sweep in
-- directory-sourced accounts, which have no local password to change and
-- would be locked out permanently.
--
-- NOT NULL DEFAULT FALSE so that every existing row keeps working. Only the
-- bootstrap administrator is created with it set, and only when it took the
-- default password: an operator who chose one through
-- PORTICO_INITIAL_ADMIN_PASSWORD has already picked a secret nobody can look
-- up, and forcing a change on them would break unattended installs for no
-- gain.
ALTER TABLE users
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN users.must_change_password IS
    'Sign-in refuses this account until the password is replaced. Set for a bootstrap administrator that took the documented default password; cleared by any path that sets a new one.';

-- +goose Down
ALTER TABLE users DROP COLUMN must_change_password;
