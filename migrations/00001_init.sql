-- +goose Up

-- Tenants are the isolation boundary. Every other table carries a tenant_id
-- and no query crosses it: see docs/database-conventions.md, "Tenant
-- isolation", for how that is enforced and tested.
--
-- This table is the one exception — it is the root of the hierarchy, so it
-- has nothing to be scoped by. Only the provisioning CLI writes it; there is
-- deliberately no cross-tenant administrator role that could reach it over
-- HTTP (docs/requirements/v0.1-requirements.md §3.1).
CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE tenants IS 'Isolation boundary. Every other table is scoped to exactly one of these.';
COMMENT ON COLUMN tenants.code IS
    'What a user types at sign-in to say which tenant they belong to. Immutable once created, because it appears in sign-in URLs and downstream configuration.';
COMMENT ON COLUMN tenants.status IS 'DISABLED refuses sign-in without deleting anything.';

-- Organizations form a tree. The depth is bounded by the service layer
-- rather than by the schema, and cycles are refused there too: PostgreSQL
-- will happily let A be B's parent and B be A's, because each row on its own
-- satisfies the foreign key.
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT        NOT NULL REFERENCES tenants (id),
    name       TEXT        NOT NULL,
    code       TEXT        NOT NULL,
    remark     TEXT        NOT NULL DEFAULT '',

    -- NULL is a root. The reference is composite so the database refuses a
    -- parent in another tenant, for the same reason users' organization
    -- reference is: application code would not normally build such a row,
    -- and "would not normally" is not a guarantee.
    --
    -- ON DELETE is not declared because organizations are disabled, never
    -- deleted.
    parent_id TEXT,
    FOREIGN KEY (tenant_id, parent_id) REFERENCES organizations (tenant_id, id),
    status     TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    sort_order BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- Codes are unique within a tenant, not globally: two tenants
    -- independently naming an organization "SALES" is normal, and a global
    -- constraint would let one tenant's choices deny another's.
    CONSTRAINT uq_organizations_tenant_code UNIQUE (tenant_id, code),

    -- Redundant given the primary key, but it is what lets users declare a
    -- composite foreign key and so have the database itself refuse a
    -- cross-tenant membership.
    CONSTRAINT uq_organizations_tenant_id UNIQUE (tenant_id, id)
);

COMMENT ON TABLE organizations IS 'Groupings of users, arranged as a tree.';
COMMENT ON COLUMN organizations.parent_id IS
    'NULL for a root. Cycles and depth are enforced in the service layer: the foreign key alone cannot see a cycle, because every row in one is individually valid.';
COMMENT ON COLUMN organizations.code IS 'Stable identifier used by bulk import and downstream systems. Immutable once created.';
COMMENT ON COLUMN organizations.status IS 'DISABLED blocks new members but keeps existing ones.';

CREATE INDEX idx_organizations_sort ON organizations (tenant_id, sort_order, created_at);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants (id),
    username      TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    phone         TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'USER' CHECK (role IN ('SUPER_ADMIN', 'USER')),
    status        TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    -- A user belongs to at most one organization. ON DELETE is not declared
    -- because organizations are disabled, never deleted.
    --
    -- The reference is composite so that the database rejects a user in one
    -- tenant pointing at another tenant's organization. Application code
    -- would not normally construct such a row, but "would not normally" is
    -- not a guarantee, and this is the only place one can be had cheaply.
    organization_id TEXT,
    FOREIGN KEY (tenant_id, organization_id) REFERENCES organizations (tenant_id, id),

    token_version BIGINT  NOT NULL DEFAULT 1,
    source        TEXT    NOT NULL DEFAULT 'ADMIN' CHECK (source IN ('ADMIN', 'IMPORT', 'REGISTRATION', 'SCIM')),

    -- The identifier the provisioning system knows this account by.
    --
    -- SCIM's externalId, and the only stable correlation key an identity
    -- provider has: usernames and email addresses are exactly the attributes
    -- a sync is likely to be changing. Without somewhere to keep it, a
    -- provisioning run that cannot match an existing account creates a
    -- second one, which is the most common way a SCIM integration passes
    -- testing and duplicates a directory in production.
    --
    -- Nullable, because accounts created any other way do not have one, and
    -- unique per tenant through a partial index so that any number of them
    -- may leave it unset.
    external_id TEXT,

    -- Online guessing, counted per account.
    --
    -- This is a different control from the per-IP throttling a reverse proxy
    -- does, and neither substitutes for the other: a proxy rate limit stops
    -- one source hammering the endpoint, and does nothing about a slow spray
    -- from a botnet against one account. This stops the second.
    failed_login_attempts INT NOT NULL DEFAULT 0,
    last_failed_login_at  TIMESTAMPTZ,
    locked_until          TIMESTAMPTZ,

    -- When the current password was set, for expiry. Nullable rather than
    -- defaulted to the row's creation time: an account whose password has
    -- never been changed since import has no meaningful age, and treating
    -- "unknown" as "just now" would silently exempt exactly the accounts an
    -- expiry policy is introduced to catch.
    password_changed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- Usernames are unique per tenant. Two tenants both having an "admin" is
    -- the expected case, which is also why sign-in has to be told which
    -- tenant it is for.
    --
    -- Constraints are named rather than left to PostgreSQL's generator
    -- because the service layer discriminates on the constraint name to say
    -- which field collided. A generated name would make that mapping depend
    -- on a string nothing declares.
    CONSTRAINT uq_users_tenant_username UNIQUE (tenant_id, username),

    -- Redundant given the primary key, but it is what lets password_resets
    -- declare a composite foreign key and so have the database refuse a
    -- reset row pointing at another tenant's account.
    CONSTRAINT uq_users_tenant_id UNIQUE (tenant_id, id)
);

COMMENT ON COLUMN users.token_version IS
    'Bumped on logout, password change, and disable. A token carrying a stale value is rejected, which is how a stateless JWT is revoked immediately.';
COMMENT ON COLUMN users.source IS 'How the account came to exist, for the registration log.';
COMMENT ON COLUMN users.failed_login_attempts IS
    'Consecutive failures within the counting window. Reset by a successful sign-in, by a completed password recovery, and by an administrator unlocking the account.';
COMMENT ON COLUMN users.password_changed_at IS
    'When the current password was set. NULL means never changed since the account was created, which an expiry policy treats as due.';
COMMENT ON COLUMN users.locked_until IS
    'Set when the threshold is reached; expires on its own. Further failures while locked do not extend it, or locking somebody out would be a denial of service anyone could perform.';
COMMENT ON COLUMN users.email IS
    'Doubles as a sign-in identifier and as the destination for password recovery. Unique within the tenant when set; empty means not bound.';
COMMENT ON COLUMN users.phone IS
    'Doubles as a sign-in identifier and as the destination for password recovery. Unique within the tenant when set; empty means not bound.';

-- Phone and email are sign-in identifiers, so they have to be unambiguous.
-- The constraint is partial because empty is the default and means "not
-- bound" — a plain unique index would let exactly one account per tenant
-- leave either field blank.
--
-- Uniqueness is per tenant, like everything else. The same address existing
-- in two tenants is legitimate; a global constraint would let one tenant's
-- users deny another's.
CREATE UNIQUE INDEX uq_users_tenant_email ON users (tenant_id, email) WHERE email <> '';
CREATE UNIQUE INDEX uq_users_tenant_phone ON users (tenant_id, phone) WHERE phone <> '';

-- NULL rather than '' for "not provisioned", so the partial index reads as
-- what it is. The other two use '' because a blank email is a value a form
-- submits; an externalId is only ever set by a provisioning system.
CREATE UNIQUE INDEX uq_users_tenant_external_id
    ON users (tenant_id, external_id) WHERE external_id IS NOT NULL;

CREATE INDEX idx_users_organization ON users (tenant_id, organization_id);
CREATE INDEX idx_users_created ON users (tenant_id, created_at);

-- All five log kinds share one table: they are queried identically (paged,
-- filtered by time) and differ only in which columns are populated.
CREATE TABLE audit_logs (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    kind      TEXT NOT NULL CHECK (kind IN ('LOGIN', 'OPERATION', 'AUTH', 'REGISTRATION', 'ORGANIZATION')),

    action TEXT NOT NULL,

    -- The actor's username is denormalized so the log stays readable, and so
    -- a failed sign-in against a nonexistent account still records what was
    -- attempted.
    actor_id       TEXT,
    actor_username TEXT NOT NULL DEFAULT '',

    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',

    result TEXT NOT NULL DEFAULT 'SUCCESS' CHECK (result IN ('SUCCESS', 'FAILURE')),
    detail TEXT NOT NULL DEFAULT '',
    ip     TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL
);

COMMENT ON COLUMN audit_logs.actor_id IS
    'Null when the actor could not be identified, such as a sign-in attempt against an unknown username.';
COMMENT ON COLUMN audit_logs.tenant_id IS
    'The tenant the event happened in. A sign-in naming a tenant that does not exist has no tenant to belong to and so is reported to the process log instead; see SECURITY.md.';

CREATE INDEX idx_audit_logs_kind_created ON audit_logs (tenant_id, kind, created_at DESC);
CREATE INDEX idx_audit_logs_created ON audit_logs (tenant_id, created_at DESC);

-- Outstanding password-recovery requests (§3.5).
CREATE TABLE password_resets (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    user_id   TEXT NOT NULL,

    -- Composite, so a reset row cannot point at another tenant's account.
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    -- The SHA-256 of the token, never the token. A reset token is a
    -- password-equivalent for the window it is live, and a database that
    -- leaks — a backup, a replica, a stray dump — would otherwise hand over
    -- working credentials for every outstanding request. Hashing costs
    -- nothing here because the token is high-entropy and random, so the
    -- slow-hash reasoning that applies to passwords does not apply.
    token_hash TEXT NOT NULL UNIQUE,

    channel TEXT NOT NULL CHECK (channel IN ('EMAIL', 'SMS')),

    expires_at TIMESTAMPTZ NOT NULL,
    -- Set when the token is spent, and also when a newer request supersedes
    -- it, so a request is always single-use and only the latest one works.
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE password_resets IS
    'Outstanding password-recovery requests. Rows are kept after use as part of the trail; expiry and used_at are what make a token unusable, not deletion.';

CREATE INDEX idx_password_resets_user ON password_resets (tenant_id, user_id);

-- Runtime-tunable settings, per tenant. Values are stored as text and parsed
-- by the settings service, which owns the defaults.
-- Passwords an account has used before, so a policy can refuse reuse.
--
-- Hashes only, and bcrypt hashes at that: checking whether a new password
-- matches a previous one means comparing against each stored hash in turn,
-- which is deliberately slow. That bounds how deep a history is worth
-- keeping far more than storage does — see MaxPasswordHistoryDepth.
--
-- Rows are trimmed to the configured depth when a password is set, so
-- lowering the depth takes effect on the next change rather than leaving
-- older entries to be consulted forever.
CREATE TABLE password_history (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    user_id   TEXT NOT NULL,
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);

-- Newest first: the check reads the top N and nothing else.
CREATE INDEX idx_password_history_user
    ON password_history (tenant_id, user_id, created_at DESC);

-- One row per sign-in, so a session can be listed and ended on its own.
--
-- Portico's own token is a JWT and stays one; this table does not replace
-- it. What it adds is identity: the token carries a session id, and the
-- middleware — which already re-reads the account on every request, so that
-- a disable takes effect immediately — checks this row at the same time.
-- Without it "sign out" can only mean "sign out everywhere", because there
-- is nothing to name a single session by.
--
-- The token is not stored, not even hashed. It is signed and self-describing
-- and this row is only ever consulted by the id inside it, so keeping a copy
-- would add a credential to steal and answer no question.
CREATE TABLE sessions (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    user_id   TEXT NOT NULL,
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    -- Recorded so somebody reviewing their own sessions can recognize them.
    -- Both are attacker-controlled strings that are only ever displayed, so
    -- they are stored as sent and escaped where rendered.
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',

    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);

COMMENT ON COLUMN sessions.last_seen_at IS
    'Updated lazily rather than on every request: a write per request would make every read a write, and minute-level accuracy is all a session list needs.';

CREATE INDEX idx_sessions_user ON sessions (tenant_id, user_id, created_at DESC);

CREATE TABLE system_settings (
    tenant_id  TEXT        NOT NULL REFERENCES tenants (id),
    key        TEXT        NOT NULL,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (tenant_id, key)
);

-- ── Federation ───────────────────────────────────────────────────────────
--
-- Portico is an OpenID Provider, and every table below serves that (§3.2).
--
-- Each tenant is its own issuer, at {public URL}/t/{code}, with its own
-- discovery document, its own keys, and its own clients. The alternative —
-- one issuer with the tenant carried in a claim — is only safe if every
-- relying party writes extra code to check that claim, and no standard
-- library does: they check `iss` and the matching key set. Per-tenant
-- issuers make cross-tenant token confusion structurally impossible rather
-- than merely discouraged, which is the same standard the rest of this
-- schema is held to.

-- Signing keys for ID tokens, per tenant.
--
-- Asymmetric, because a relying party verifies an ID token offline against
-- the published JWKS — there is nobody to ask. That is why these exist at
-- all: the HS256 secret that signs Portico's own sessions cannot be given
-- out, and a shared secret is not a key set.
CREATE TABLE oauth_signing_keys (
    -- The `kid` in the JWT header and in the JWKS. Generated, not derived,
    -- so a key can be replaced without its identifier being predictable.
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    algorithm   TEXT NOT NULL CHECK (algorithm IN ('RS256')),
    private_key TEXT NOT NULL,
    public_key  TEXT NOT NULL,

    -- RETIRED keys stay in the JWKS until every token they signed has
    -- expired. Removing one at the moment of rotation would invalidate live
    -- tokens that are perfectly valid, which is the failure people diagnose
    -- as "the login randomly broke".
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'RETIRED')),

    created_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ
);

COMMENT ON COLUMN oauth_signing_keys.private_key IS
    'PKCS#8 PEM. Stored as the database stores everything else; protecting it is the deployment''s job, the same as the password hashes beside it.';

CREATE INDEX idx_oauth_signing_keys_tenant ON oauth_signing_keys (tenant_id, status);

-- Relying parties. Registered from the command line, like tenants: a client
-- registration is a decision about who may ask this server for tokens, and
-- V0.1 has no role that could be authorized to make it over HTTP.
CREATE TABLE oauth_clients (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What the relying party sends as client_id. Unique per tenant, not
    -- globally, for the same reason usernames are.
    client_id TEXT NOT NULL,
    name      TEXT NOT NULL,

    -- Null for a public client. A browser or mobile app cannot keep a
    -- secret, so it does not get one — it uses PKCE instead, which OAuth 2.1
    -- requires of every client anyway.
    secret_hash TEXT,

    application_type TEXT NOT NULL DEFAULT 'WEB'
        CHECK (application_type IN ('WEB', 'NATIVE', 'USER_AGENT')),
    auth_method TEXT NOT NULL DEFAULT 'client_secret_basic'
        CHECK (auth_method IN ('none', 'client_secret_basic', 'client_secret_post')),

    -- Matched exactly, never by prefix or pattern. A redirect URI that is
    -- matched loosely is how an authorization code ends up at an attacker's
    -- endpoint, and it is the single most exploited weakness of this flow.
    redirect_uris             TEXT[] NOT NULL DEFAULT '{}',
    post_logout_redirect_uris TEXT[] NOT NULL DEFAULT '{}',

    grant_types TEXT[] NOT NULL DEFAULT '{authorization_code}',
    scopes      TEXT[] NOT NULL DEFAULT '{openid,profile,email}',

    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_oauth_clients_tenant_client_id UNIQUE (tenant_id, client_id)
);

-- An authorization request in flight: created when the browser arrives at
-- /authorize, completed when the person signs in, exchanged at /token.
CREATE TABLE oauth_auth_requests (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    client_id TEXT NOT NULL,

    -- Which issuer the request arrived at. A tenant is reachable at
    -- /t/<code>, and the default tenant additionally at the root, so the
    -- same tenant has two issuers and the sign-in screen cannot work out
    -- from the tenant alone where to send the browser back. Recording it
    -- here means the answer comes from the request rather than from a
    -- parameter somebody could supply.
    issuer TEXT NOT NULL,

    -- Null until the person has signed in. A request that is exchanged for a
    -- token without one would be a token for nobody.
    subject TEXT,
    FOREIGN KEY (tenant_id, subject) REFERENCES users (tenant_id, id),

    redirect_uri  TEXT   NOT NULL,
    response_type TEXT   NOT NULL,
    response_mode TEXT   NOT NULL DEFAULT '',
    scopes        TEXT[] NOT NULL DEFAULT '{}',
    audience      TEXT[] NOT NULL DEFAULT '{}',
    state         TEXT   NOT NULL DEFAULT '',
    nonce         TEXT   NOT NULL DEFAULT '',

    -- PKCE. Not nullable in practice: OAuth 2.1 requires it of every client,
    -- including confidential ones, so a request without a challenge is
    -- rejected before it reaches this table.
    code_challenge        TEXT NOT NULL DEFAULT '',
    code_challenge_method TEXT NOT NULL DEFAULT '',

    auth_time TIMESTAMPTZ,
    amr       TEXT[] NOT NULL DEFAULT '{}',
    done      BOOLEAN NOT NULL DEFAULT FALSE,

    -- The authorization code, once issued. Hashed for the same reason a
    -- reset token is: it is a bearer credential for its lifetime, and it is
    -- worth nothing to an attacker who only has the database.
    code_hash TEXT UNIQUE,

    created_at TIMESTAMPTZ NOT NULL,
    -- Minutes, not hours. A code is exchanged immediately by a machine; a
    -- long window only widens the interception opportunity.
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_oauth_auth_requests_expiry ON oauth_auth_requests (expires_at);

-- Issued refresh tokens.
--
-- Access tokens are not stored: they are signed JWTs the resource server
-- verifies offline, which is the whole point of issuing them. Refresh tokens
-- are stored because they must be revocable and single-use.
CREATE TABLE oauth_refresh_tokens (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),
    client_id TEXT NOT NULL,

    subject TEXT NOT NULL,
    FOREIGN KEY (tenant_id, subject) REFERENCES users (tenant_id, id),

    token_hash TEXT NOT NULL UNIQUE,

    scopes   TEXT[] NOT NULL DEFAULT '{}',
    audience TEXT[] NOT NULL DEFAULT '{}',
    amr      TEXT[] NOT NULL DEFAULT '{}',

    auth_time TIMESTAMPTZ NOT NULL,

    -- Rotation. Each use issues a replacement and marks this one spent; the
    -- chain ties them together so that presenting a spent token — which
    -- means a copy leaked — can revoke every descendant rather than just
    -- failing the one call.
    replaced_by TEXT REFERENCES oauth_refresh_tokens (id),
    used_at     TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_oauth_refresh_tokens_subject ON oauth_refresh_tokens (tenant_id, subject, client_id);

-- The signing identity a tenant's SAML assertions carry.
--
-- Separate from oauth_signing_keys, which the JWKS publishes, because the
-- two have incompatible rotation contracts. A relying party refetches the
-- key set, so retiring an OIDC key and deleting it a day later is safe. A
-- SAML service provider pins the certificate in its own configuration and
-- has no way to learn of a new one — so a certificate that disappeared on
-- the OIDC schedule would break every service provider silently, and
-- rotating for an OIDC reason would take SAML down with it.
CREATE TABLE saml_signing_keys (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    private_key TEXT NOT NULL,
    -- The self-signed certificate, PEM. SAML carries the whole certificate
    -- in metadata and in every signature, where OIDC publishes a bare
    -- public key, so this is stored rather than derived on demand.
    certificate TEXT NOT NULL,

    status     TEXT NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED')),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_saml_signing_keys_active
    ON saml_signing_keys (tenant_id) WHERE status = 'ACTIVE';

-- A registered SAML service provider.
--
-- Registered out of band, like an OAuth client and for the same reason: it
-- decides who may receive assertions about this tenant's people.
CREATE TABLE saml_service_providers (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- The entity id the service provider puts in its requests. Unique
    -- within a tenant, like every other identifier here.
    entity_id TEXT NOT NULL,
    name      TEXT NOT NULL,

    -- The service provider's metadata document, stored whole. The protocol
    -- library reads the assertion consumer service endpoints, the NameID
    -- formats, and the signing certificates out of it, and keeping the
    -- document rather than a handful of extracted fields means a service
    -- provider that offers three endpoints keeps all three.
    metadata_xml TEXT NOT NULL,

    status     TEXT NOT NULL CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_saml_sp_tenant_entity UNIQUE (tenant_id, entity_id)
);

-- A SAML authentication request in flight, held while somebody signs in.
--
-- The same shape as oauth_auth_requests and for the same reason: the
-- protocol hands the browser to Portico's own sign-in, which has to be able
-- to hand it back.
CREATE TABLE saml_auth_requests (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- Which issuer the request arrived at, so the browser is returned to the
    -- mount it came from rather than one a parameter chose.
    issuer TEXT NOT NULL,

    -- The decoded AuthnRequest, exactly as received. Re-validated on resume
    -- by the protocol library rather than picked apart here.
    request_xml TEXT NOT NULL,
    relay_state TEXT NOT NULL DEFAULT '',

    -- The service provider that sent it, for the audit entry and for
    -- reporting a disabled one before the browser is redirected onward.
    sp_entity_id TEXT NOT NULL,

    subject TEXT,
    FOREIGN KEY (tenant_id, subject) REFERENCES users (tenant_id, id),
    done BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL,
    -- The real sign-in deadline. The protocol library independently refuses
    -- a request more than 90 seconds older than its issue instant, which is
    -- far shorter than a person takes to type a password — so freshness is
    -- judged against the moment Portico accepted the request, and this is
    -- what bounds how long it may then sit.
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_saml_auth_requests_expiry ON saml_auth_requests (expires_at);

-- A registered CAS service.
--
-- CAS's `service` parameter is its redirect_uri: it is where a ticket is
-- delivered, so anything that may appear there has to be registered first.
CREATE TABLE cas_services (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    name TEXT NOT NULL,
    -- A URL prefix, not a pattern. There are no wildcards: a service URL
    -- matches when it begins with this value and the next character is a
    -- path or query separator, so a registration for
    -- https://app.example.com/ can never match https://app.example.com.evil.
    -- Prefixes rather than exact URLs because a CAS client legitimately
    -- appends its own return-to parameters.
    url_prefix TEXT NOT NULL,

    status     TEXT NOT NULL CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_cas_services_tenant_prefix UNIQUE (tenant_id, url_prefix)
);

-- An issued CAS service ticket.
--
-- Single use and short lived. There is no ticket-granting ticket: CAS
-- single sign-on here rides on Portico's own session rather than on a
-- second long-lived cookie, so signing out, changing a password, and
-- disabling an account already end it — there is no third credential that
-- could outlive them.
CREATE TABLE cas_tickets (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- Hashed, like every other bearer credential here: it is worth nothing
    -- to somebody who only has the database.
    ticket_hash TEXT NOT NULL UNIQUE,

    -- The service it was issued for. Validation refuses a ticket presented
    -- with a different one, which is what stops a service that receives a
    -- ticket from spending it somewhere else.
    service TEXT NOT NULL,

    subject TEXT NOT NULL,
    FOREIGN KEY (tenant_id, subject) REFERENCES users (tenant_id, id),

    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_cas_tickets_expiry ON cas_tickets (expires_at);

-- The credential a provisioning system authenticates with.
--
-- Its own table and its own kind of principal, rather than a service account
-- in users. A SCIM client is not a person: it has no session, no password to
-- recover, no organization, and must never be able to sign in to the
-- console. Modelling it as a user would mean a row that every account query,
-- every role check, and every listing has to remember to exclude — and the
-- one that forgets is a directory sync that can administer the tenant.
--
-- Scope is not a column. This credential authenticates for /scim/v2 and
-- nothing else, enforced by the route it is accepted on, so there is no
-- setting that could be widened by mistake.
CREATE TABLE scim_credentials (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What an operator recognises it by: "Okta production", "Entra test".
    name TEXT NOT NULL,

    -- SHA-256 of the token, never the token. Not bcrypt, deliberately, and
    -- this is the one place that reasoning differs from passwords: the token
    -- is 32 bytes from crypto/rand, so there is no dictionary to attack and
    -- nothing for a work factor to buy — while a SCIM client presents it on
    -- every request of a sync that may run to thousands, and a deliberately
    -- slow comparison there is a denial of service against the operator's
    -- own directory push.
    token_hash TEXT NOT NULL,

    -- The first characters of the token, shown in the console so an operator
    -- can tell two credentials apart without being able to reconstruct one.
    token_prefix TEXT NOT NULL,

    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    -- Answers "is this integration still running", which is the question
    -- asked when a directory has quietly stopped syncing. Written on use,
    -- and deliberately the only thing a request updates.
    last_used_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_scim_credentials_tenant_name UNIQUE (tenant_id, name)
);

-- The lookup every SCIM request makes: by hash, across tenants, because the
-- credential is what establishes which tenant the request is for. This is
-- the same shape as GetUserForAuthentication and for the same reason — the
-- tenant cannot be a filter on the query that determines the tenant.
CREATE UNIQUE INDEX uq_scim_credentials_token ON scim_credentials (token_hash);

CREATE INDEX idx_scim_credentials_tenant ON scim_credentials (tenant_id, status);


-- Where a tenant wants to be told about changes here.
--
-- The URL is supplied by an administrator and this process usually runs
-- inside a network, so the destination rules are part of the schema's
-- meaning rather than a detail of the caller: https only, never an address
-- that resolves inside. See internal/webhook for the checks and for why one
-- of them has to happen at connection time rather than at registration.
CREATE TABLE webhook_subscriptions (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What an operator recognises it by.
    name TEXT NOT NULL,

    url TEXT NOT NULL,

    -- The HMAC key, in the clear.
    --
    -- Unlike a client secret or a SCIM token, this one is not compared
    -- against something a caller presents — it is used to *produce* a
    -- signature, so a digest would be useless and there is nothing to hash.
    -- It is therefore in the same category as the signing keys two tables
    -- above: a database dump contains it, and docs/backup-and-restore.md
    -- says so.
    secret TEXT NOT NULL,

    -- Which events to send, comma-separated, or '*' for all of them.
    --
    -- A list rather than a row per event: a subscription is one endpoint
    -- with one secret and one delivery history, and splitting it would make
    -- "this integration has stopped working" a question about several rows.
    events TEXT NOT NULL DEFAULT '*',

    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_webhook_subscriptions_tenant_name UNIQUE (tenant_id, name),

    -- Redundant given the primary key, and what lets webhook_deliveries
    -- declare a composite foreign key so a delivery cannot point at another
    -- tenant's subscription.
    CONSTRAINT uq_webhook_subscriptions_tenant_id UNIQUE (tenant_id, id)
);

CREATE INDEX idx_webhook_subscriptions_tenant ON webhook_subscriptions (tenant_id, status);

-- One attempt to tell somebody something, and its history.
--
-- Rows are kept after success, not deleted, because "did they receive the
-- deactivation" is exactly the question asked afterwards — and answering it
-- from the recipient's logs means asking the recipient. The sweep removes
-- them on the same schedule as everything else.
CREATE TABLE webhook_deliveries (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    subscription_id TEXT NOT NULL,
    FOREIGN KEY (tenant_id, subscription_id)
        REFERENCES webhook_subscriptions (tenant_id, id) ON DELETE CASCADE,

    event_type TEXT NOT NULL,
    -- The rendered body, stored as sent. Re-rendering it at delivery time
    -- from the current state of the account would send what is true now
    -- rather than what happened — an event that says "disabled" would
    -- arrive describing an account somebody has since re-enabled.
    payload TEXT NOT NULL,

    status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'DELIVERED', 'FAILED')),

    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT '',
    last_status  INT,

    -- When to try next. NULL once the delivery is finished either way.
    next_attempt_at TIMESTAMPTZ,

    created_at   TIMESTAMPTZ NOT NULL,
    delivered_at TIMESTAMPTZ
);

-- The claim query: what is due, oldest first.
CREATE INDEX idx_webhook_deliveries_due
    ON webhook_deliveries (next_attempt_at)
    WHERE status = 'PENDING';

CREATE INDEX idx_webhook_deliveries_subscription
    ON webhook_deliveries (tenant_id, subscription_id, created_at DESC);



-- Groups are sets of people, and are not the organization chart.
--
-- Both exist because they answer different questions and have incompatible
-- shapes. An organization is where somebody sits: one of them, arranged as a
-- tree, with a stable code that downstream systems store. A group is a set
-- somebody belongs to: any number of them, flat, and maintained by whatever
-- directory pushes them. Folding groups into organizations would mean either
-- breaking single membership — which the org chart depends on — or silently
-- reassigning people when a directory adds them to a second group.
--
-- Membership grants nothing. A directory says who somebody is; it does not
-- say what they may do, and there is nothing here for it to say that with:
-- roles are two fixed values and there is no RBAC to attach a group to. If
-- that changes, it changes deliberately and not by a directory writing an
-- attribute.
CREATE TABLE groups (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- SCIM's displayName, and the name a person recognises. Unique per
    -- tenant so a push that renames rather than creates has something to
    -- collide with.
    display_name TEXT NOT NULL,

    description TEXT NOT NULL DEFAULT '',

    -- SCIM's externalId, on exactly the same terms as users': the only
    -- stable correlation key when the display name is the thing being
    -- changed. Nullable, unique per tenant through a partial index.
    external_id TEXT,

    -- How the group came to exist, so an administrator can see that a
    -- directory owns it before wondering why their edit was overwritten.
    source TEXT NOT NULL DEFAULT 'ADMIN' CHECK (source IN ('ADMIN', 'SCIM')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_groups_tenant_display_name UNIQUE (tenant_id, display_name),

    -- Redundant given the primary key, and what lets group_members declare a
    -- composite foreign key so a membership cannot cross tenants.
    CONSTRAINT uq_groups_tenant_id UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX uq_groups_tenant_external_id
    ON groups (tenant_id, external_id) WHERE external_id IS NOT NULL;

CREATE INDEX idx_groups_tenant ON groups (tenant_id, display_name);

-- Membership. Many-to-many, which is the whole reason groups are a separate
-- table from organizations.
CREATE TABLE group_members (
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    group_id TEXT NOT NULL,
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups (tenant_id, id) ON DELETE CASCADE,

    user_id TEXT NOT NULL,
    -- Composite, so the database refuses a membership pointing at another
    -- tenant's account. ON DELETE is absent because accounts are disabled,
    -- never deleted — a disabled member stays in the group, since removing
    -- them would lose what a directory said and make re-enabling incomplete.
    FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id),

    added_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (tenant_id, group_id, user_id)
);

-- The reverse lookup: which groups is this person in. Needed by the user
-- detail screen and by SCIM's GET /Users/{id}, which returns them so a
-- client can read back that its push landed.
CREATE INDEX idx_group_members_user ON group_members (tenant_id, user_id);

-- +goose Down
DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE webhook_deliveries;
DROP TABLE webhook_subscriptions;
DROP TABLE scim_credentials;
DROP TABLE cas_tickets;
DROP TABLE cas_services;
DROP TABLE saml_auth_requests;
DROP TABLE saml_service_providers;
DROP TABLE saml_signing_keys;
DROP TABLE oauth_refresh_tokens;
DROP TABLE oauth_auth_requests;
DROP TABLE oauth_clients;
DROP TABLE oauth_signing_keys;
DROP TABLE password_resets;
DROP TABLE system_settings;
DROP TABLE audit_logs;
DROP TABLE users;
DROP TABLE organizations;
DROP TABLE tenants;
