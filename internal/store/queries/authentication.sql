-- The one query on a tenant-scoped table that is not itself tenant-scoped.
--
-- It runs before the tenant is known: a bearer token identifies a user, and
-- this read is how the tenant that user belongs to is established. Filtering
-- it by tenant would be circular.
--
-- Two things keep it from being a hole. The caller compares the row's
-- tenant_id against the tid claim in the token and rejects a mismatch, so a
-- token cannot be replayed against a tenant it was not issued for. And
-- TestExactlyOneUnscopedQueryIsAllowed asserts that this is the only unscoped read of a
-- scoped table — adding a second one fails the build rather than passing
-- review unnoticed.

-- name: GetUserForAuthentication :one
SELECT * FROM users WHERE id = $1 LIMIT 1;
