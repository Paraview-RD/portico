-- +goose Up

-- A sign-in that has left for somebody else's provider and not come back.
--
-- The row exists because the callback arrives as a bare HTTP request from a
-- browser that has been elsewhere, and everything needed to judge it was
-- decided before it left: which tenant, which provider, what nonce the ID
-- token must carry, and what the code verifier was. Keeping any of that in a
-- cookie would make the browser the authority on its own sign-in.
CREATE TABLE external_auth_requests (
    -- The `state` parameter, and the primary key. One value rather than two
    -- so a callback cannot name a row it did not open: a state that is not a
    -- key here is a state nobody issued.
    state TEXT PRIMARY KEY,

    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    provider_id TEXT NOT NULL,
    FOREIGN KEY (tenant_id, provider_id)
        REFERENCES external_identity_providers (tenant_id, id) ON DELETE CASCADE,

    -- What the returning ID token must carry. Compared on the way back; a
    -- token without it answers some other request.
    nonce TEXT NOT NULL,

    -- The PKCE verifier. Stored rather than sealed: it is single-use, lives
    -- for minutes, and is deleted the moment the callback is judged — and it
    -- is worthless without the authorization code, which never touches this
    -- table. Sealing it would put a key on the path of every sign-in to buy
    -- nothing a dump could not already get from the row's short life.
    code_verifier TEXT NOT NULL,

    -- Who is signing in, when somebody already is.
    --
    -- Null means an ordinary sign-in: whoever comes back is whoever the
    -- provider says. Set means a person in this console asked to bind an
    -- external identity to their own account, and the binding must land on
    -- that account rather than on whatever the callback would otherwise
    -- resolve to. Without this the same callback would serve both, and a
    -- crafted link could bind an attacker's identity to a signed-in
    -- victim's account.
    user_id TEXT,
    FOREIGN KEY (tenant_id, user_id)
        REFERENCES users (tenant_id, id) ON DELETE CASCADE,

    created_at TIMESTAMPTZ NOT NULL,
    -- Minutes, not hours. This is the window in which a stolen state is
    -- useful, and a person who wandered off mid-sign-in can start again for
    -- the price of one click.
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_external_auth_requests_expiry
    ON external_auth_requests (expires_at);

COMMENT ON TABLE external_auth_requests IS
    'One outstanding sign-in through an external provider. Deleted when the callback is judged, whichever way it goes.';
COMMENT ON COLUMN external_auth_requests.user_id IS
    'Set when a signed-in person is binding an identity to their own account, null for an ordinary sign-in. It is what stops one callback serving both.';

-- +goose Down
DROP TABLE external_auth_requests;
