-- +goose Up

-- Somebody else's OpenID Provider, trusted to say who a person is.
--
-- This is the opposite direction from everything in oidcp: there, Portico
-- issues the tokens and other applications trust them. Here it spends them,
-- which is a different job with a different threat model — the assertions
-- arrive from outside, and what is done with them is the whole security
-- question.
--
-- Per tenant, on the same footing as ldap_sources: one deployment may serve
-- an organization that signs in through its own Entra tenant and another
-- that uses Google, and neither should see the other's configuration.
CREATE TABLE external_identity_providers (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What an operator recognises it by: "Company Google", "Partner Entra".
    name TEXT NOT NULL,
    -- What a person clicks. Separate from name because the button says
    -- "Sign in with Google" and the settings list says which Google.
    button_label TEXT NOT NULL DEFAULT '',

    -- The issuer, exactly as it appears in an id_token's `iss`. Discovery
    -- hangs off it, and so does the check that a token came from who it
    -- claims: comparing `iss` against this string is the cheapest half of
    -- validating an assertion, and skipping it is how a token minted by one
    -- provider gets accepted as another's.
    issuer TEXT NOT NULL,

    client_id TEXT NOT NULL,
    -- Sealed with PORTICO_ENCRYPTION_KEY, the third thing to need it after
    -- directory bind passwords and webhook custom headers. A credential this
    -- server has to present rather than compare, so a digest is useless —
    -- and without a key configured, saving one is refused rather than
    -- written in the clear.
    client_secret TEXT NOT NULL DEFAULT '',

    -- Space-separated, `openid` always included by the service whatever is
    -- stored here.
    scopes TEXT NOT NULL DEFAULT 'openid profile email',

    -- Whether this provider's `email_verified` may be believed well enough
    -- to offer a first-time link by address.
    --
    -- Off, and it has to stay a decision somebody makes per provider. An
    -- identity provider that does not verify addresses lets anybody register
    -- ceo@your-company.example and arrive here holding a token that says so
    -- — and if an address is enough to be let into an existing account, that
    -- is the whole account. With this off, an external identity reaches an
    -- account only when somebody already signed in binds it.
    trust_verified_email BOOLEAN NOT NULL DEFAULT FALSE,

    status TEXT NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'DISABLED')),

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- One configuration per issuer per tenant. Two rows for the same issuer
    -- would mean two buttons that authenticate the same people, and an
    -- identity bound through one of them not being recognised through the
    -- other.
    CONSTRAINT uq_external_idp_tenant_issuer UNIQUE (tenant_id, issuer),
    CONSTRAINT uq_external_idp_tenant_id UNIQUE (tenant_id, id)
);

COMMENT ON TABLE external_identity_providers IS
    'An OpenID Provider this deployment sends people to. Portico is the relying party here, not the issuer.';
COMMENT ON COLUMN external_identity_providers.trust_verified_email IS
    'Whether email_verified from this provider may bind a first-time login to an existing account by address. Off by default: it delegates account security to whoever runs that provider.';

-- Which external identity belongs to which account.
--
-- Its own table rather than users.external_id, which is already the
-- reconciliation key SCIM and LDAP use and is unique per tenant. One person
-- may be provisioned by a directory and also sign in through Google, and
-- those are two different foreign identifiers for the same account — one
-- column could hold only one of them.
--
-- The identity is the pair (issuer, subject), not the subject alone. `sub`
-- is unique within an issuer and nowhere else, so two providers can and do
-- hand out the same string.
CREATE TABLE external_identities (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    provider_id TEXT NOT NULL,
    FOREIGN KEY (tenant_id, provider_id)
        REFERENCES external_identity_providers (tenant_id, id) ON DELETE CASCADE,

    user_id TEXT NOT NULL,
    FOREIGN KEY (tenant_id, user_id)
        REFERENCES users (tenant_id, id) ON DELETE CASCADE,

    subject TEXT NOT NULL,
    -- What the provider said the address was at binding time, for the
    -- profile screen to show. Never used to find an account: that is what
    -- the subject is for, and an address that changed at the provider must
    -- not silently repoint a binding.
    email TEXT NOT NULL DEFAULT '',

    created_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,

    -- One account per identity. Without this, two accounts could both claim
    -- the same external person and which one a sign-in reached would depend
    -- on row order.
    CONSTRAINT uq_external_identity UNIQUE (tenant_id, provider_id, subject)
);

CREATE INDEX idx_external_identities_user
    ON external_identities (tenant_id, user_id);

COMMENT ON COLUMN external_identities.subject IS
    'The provider''s `sub`. Unique within that issuer only, which is why the provider is part of the key.';

-- +goose Down
DROP TABLE external_identities;
DROP TABLE external_identity_providers;
