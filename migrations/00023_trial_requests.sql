-- +goose Up

-- Somebody asking for a tenant of their own, before there is one.
--
-- This is the only table in the schema with no tenant_id, and that is the
-- whole difficulty rather than an oversight. Every other row here belongs to a
-- tenant and is reached through a scoped query; this one exists precisely
-- because the tenant does not exist yet, so it is written and read outside
-- that boundary. Nothing else may follow it there without a reason as
-- explicit as this one.
--
-- It only exists at all where PORTICO_TRIAL_SIGNUP is on, which is a
-- demonstration and never a deployment holding anybody's staff. The routes are
-- not registered otherwise, so on an ordinary installation this table stays
-- empty and unreachable.
CREATE TABLE trial_requests (
    id TEXT PRIMARY KEY,

    -- The address that has to be proven, and the one thing a visitor is
    -- identified by. Lower-cased by the service before it lands here: two
    -- requests differing only in case are the same person asking twice, and
    -- the quota below would otherwise be one per spelling.
    email TEXT NOT NULL,

    -- What they typed about themselves. Shown back to them and used for the
    -- tenant's display name; never parsed.
    company_name TEXT NOT NULL,

    -- The tenant code they will sign in with, decided and checked for
    -- collisions at request time rather than at confirmation.
    --
    -- The alternative — allocating it when the link is clicked — puts the one
    -- failure a visitor cannot fix ("that code is taken now") after they have
    -- already been told to check their email. Reserved early means a code can
    -- be held by a request nobody confirms, which the expiry below reclaims.
    tenant_code TEXT NOT NULL,

    -- Which seeded world they asked for. A free-text key rather than an
    -- enumeration: the industry packs are data, and a new one should not need
    -- a migration.
    industry TEXT NOT NULL,

    -- The verification link, as a hash. Never the token itself: this row is
    -- readable by anything that can read the database, and a stored token
    -- would let it confirm somebody else's request.
    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    -- When the link was followed, and what it produced. Both null while the
    -- request is pending; a confirmed row is kept rather than deleted so the
    -- quota can count it and a second click on the same link finds it already
    -- spent.
    confirmed_at TIMESTAMPTZ,
    tenant_id TEXT REFERENCES tenants (id),

    -- Where the request came from, for the rate limiter's slower cousin: a
    -- single address opening tenants steadily rather than quickly.
    request_ip TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One tenant per address, enforced rather than checked.
--
-- Partial, on the confirmed rows only: an address may ask again after a link
-- expires unused, and a check in application code would race two clicks on two
-- links issued to the same address.
CREATE UNIQUE INDEX trial_requests_one_per_email
    ON trial_requests (email) WHERE confirmed_at IS NOT NULL;

-- A code is reserved by any live request, confirmed or not, which is what
-- makes the early allocation above safe.
CREATE UNIQUE INDEX trial_requests_code_reserved
    ON trial_requests (tenant_code);

-- Reading a link means finding it by hash, and it is the only lookup on the
-- hot path.
CREATE INDEX trial_requests_by_token ON trial_requests (token_hash);

-- Sweeping expired unconfirmed requests, which is what returns a reserved
-- code to circulation.
CREATE INDEX trial_requests_expiry ON trial_requests (expires_at)
    WHERE confirmed_at IS NULL;

COMMENT ON TABLE trial_requests IS
    'Self-service trial signups, for a public demonstration only. The one table with no tenant_id: it is what exists before a tenant does. Unreachable unless PORTICO_TRIAL_SIGNUP is on.';

-- +goose Down
DROP TABLE trial_requests;
