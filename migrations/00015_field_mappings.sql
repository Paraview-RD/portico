-- +goose Up

-- What each recipient receives, and under what name.
--
-- Until now the answer was written into four places — oidcp's privateClaims,
-- samlp's attributes, casp's writeSuccess, and whatever model.User happened to
-- marshal to for a webhook — and was the same for every recipient. That is a
-- reasonable default and a poor rule: a service provider maps by the name it is
-- given, so an integration whose expected name differs from Portico's had no
-- way in but a code change, and the twenty-five profile attributes had no way
-- out at all.
--
-- One table for all four recipients rather than a column on each of their
-- registration tables. The mapping logic is one thing; four copies of it would
-- drift, and the repository has the receipt for that — the tile-picture field
-- was copied into the three registration forms and that is precisely how they
-- first went out of step. It also makes "which recipients receive department"
-- answerable, which is the question asked after a disclosure review and not
-- before it.
CREATE TABLE field_mappings (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- Exactly one of these is set. Four nullable foreign keys rather than a
    -- kind column and a loose id, because this way the database enforces that
    -- the recipient exists and takes the mappings with it when it goes; a
    -- loose id would be a row pointing at nothing the first time somebody
    -- deleted a registration.
    oauth_client_id         TEXT REFERENCES oauth_clients (id) ON DELETE CASCADE,
    saml_sp_id              TEXT REFERENCES saml_service_providers (id) ON DELETE CASCADE,
    cas_service_id          TEXT REFERENCES cas_services (id) ON DELETE CASCADE,
    -- A webhook subscription is not an application, which is why this table is
    -- named for recipients rather than for applications. It is the one
    -- recipient Portico pushes to rather than answers, and it is the one an
    -- administrator means by "synchronising to a downstream system".
    webhook_subscription_id TEXT REFERENCES webhook_subscriptions (id) ON DELETE CASCADE,

    CONSTRAINT ck_field_mappings_one_recipient CHECK (
        (oauth_client_id IS NOT NULL)::int
        + (saml_sp_id IS NOT NULL)::int
        + (cas_service_id IS NOT NULL)::int
        + (webhook_subscription_id IS NOT NULL)::int = 1
    ),

    -- The catalogue key this maps from. Not a foreign key: half the catalogue
    -- is built into the binary, so there is no table for it to reference. The
    -- service refuses a key the catalogue does not hold, and a mapping that
    -- names a tenant-defined attribute is left alone when that attribute is
    -- retired — it reads as "configured but switched off" rather than as a
    -- mapping to nothing.
    --
    -- Deliberately not qualified by a subject. A key like organization_code
    -- refers to the signing-in account's organization in an assertion and to
    -- the organization itself in an organization.* event, and that is decided
    -- at delivery by what is being delivered — the event type is already the
    -- discriminator, so a column repeating it would be a second source of
    -- truth for a question that is never actually ambiguous.
    source_key TEXT NOT NULL,

    -- The name the recipient expects. For SAML this is the Name, which is
    -- what a service provider actually maps on; the friendly name is beside it.
    -- For a webhook it is a key at the top level of the event's `data` object.
    target_name   TEXT NOT NULL DEFAULT '',
    friendly_name TEXT NOT NULL DEFAULT '',

    -- Suppressed removes a name the built-in default would have sent, rather
    -- than adding one. It is a flag rather than an empty target because the two
    -- are different intentions and an empty string cannot hold both: "send
    -- nothing" and "send under a name I have not decided yet" would otherwise
    -- be the same row.
    suppressed BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- One row per source per recipient. Renaming a fact twice for the same
    -- audience is not a thing anybody means to do, and if it were possible the
    -- winner would be whichever row was read first.
    CONSTRAINT uq_field_mappings_oauth   UNIQUE (oauth_client_id, source_key),
    CONSTRAINT uq_field_mappings_saml    UNIQUE (saml_sp_id, source_key),
    CONSTRAINT uq_field_mappings_cas     UNIQUE (cas_service_id, source_key),
    CONSTRAINT uq_field_mappings_webhook UNIQUE (webhook_subscription_id, source_key)
);

CREATE INDEX idx_field_mappings_oauth
    ON field_mappings (tenant_id, oauth_client_id)
    WHERE oauth_client_id IS NOT NULL;
CREATE INDEX idx_field_mappings_saml
    ON field_mappings (tenant_id, saml_sp_id)
    WHERE saml_sp_id IS NOT NULL;
CREATE INDEX idx_field_mappings_cas
    ON field_mappings (tenant_id, cas_service_id)
    WHERE cas_service_id IS NOT NULL;
CREATE INDEX idx_field_mappings_webhook
    ON field_mappings (tenant_id, webhook_subscription_id)
    WHERE webhook_subscription_id IS NOT NULL;

-- Answers "which recipients receive this fact", across all four at once. That
-- is the disclosure question, and it is the reason this is one table.
CREATE INDEX idx_field_mappings_source
    ON field_mappings (tenant_id, source_key);

COMMENT ON TABLE field_mappings IS
    'Per-recipient renames, additions, and suppressions. The defaults stay in code — see internal/service/field_catalogue.go.';

-- The defaults are deliberately not rows here.
--
-- Three reasons, and the third is the one that matters. An upgrade must change
-- nothing, so an empty table has to mean "behave exactly as before". The
-- documented table of what each protocol sends is checked by a test, and if the
-- defaults were rows a tenant could delete them and leave that document
-- describing something no longer true. And a row in this table means somebody
-- decided something — pre-filling ten per recipient would destroy the only
-- signal there is for telling a deliberate mapping from an inherited one.

-- +goose Down
DROP TABLE field_mappings;
