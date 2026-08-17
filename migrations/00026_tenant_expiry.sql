-- +goose Up

-- When a tenant stops being allowed to sign in, if it ever does.
--
-- Added for self-service trials, which until now created tenants that lived
-- forever. That is not a leak so much as a ceiling: the deployment caps how
-- many trial tenants may exist at once, nothing ever gave one back, and so the
-- demonstration closed itself the moment the cap was reached — permanently,
-- and without saying so. Fifty ordinary visitors were enough. No attacker
-- needed.
--
-- NULL rather than a default, and NULL means never. Every tenant that exists
-- today gets NULL, so the default tenant and anything provisioned by hand
-- behave exactly as they did: this column changes nothing for a tenant nobody
-- gave a deadline to.
--
-- On `tenants` rather than on `trial_requests`, which is where a
-- trial-specific column would naturally go. Two reasons, both about the read
-- path. Resolving a tenant at sign-in already reads this row and nothing else,
-- so a deadline here costs no join; on the other table it would cost one on
-- every sign-in, for every tenant, to answer a question that is NULL for
-- almost all of them. And expiry is not a trial idea — a tenant handed to a
-- customer for an evaluation has the same shape — so the general table is
-- where it belongs even though a trial is what asked for it.
ALTER TABLE tenants ADD COLUMN expires_at TIMESTAMPTZ;

COMMENT ON COLUMN tenants.expires_at IS
    'When sign-in stops being allowed. NULL means never, which is every tenant not created by a self-service trial. Reaching it disables the tenant rather than deleting it, so an operator can extend and re-enable; deletion happens after a further grace period and is what returns the tenant code, the quota slot, and the applicant''s address to circulation.';

-- Partial, because the sweep asks one question — "which deadlines have
-- passed?" — and the answer is drawn from the few rows that have a deadline at
-- all. A full index would carry a NULL entry for every tenant that will never
-- expire, which on an ordinary single-tenant deployment is the entire table.
CREATE INDEX idx_tenants_expires_at ON tenants (expires_at)
    WHERE expires_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_tenants_expires_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS expires_at;
