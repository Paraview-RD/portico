-- +goose Up

-- A one-time secret that binds a SAML sign-in to the browser that performed
-- it.
--
-- The callback that mints an assertion is a plain browser navigation, so it
-- carries no credential — the session lives in a token the console holds,
-- and a top-level navigation cannot send one. What it carried instead was
-- the request id, which is enough: present a completed request's id and the
-- server mints an assertion for whoever signed it in and hands it to
-- whoever asked.
--
-- The id is not a secret. It travels in the sign-in URL the browser is sent
-- to, which means browser history, a proxy log, a screenshot of an address
-- bar. What made that reachable rather than theoretical is the shape of the
-- delivery: unlike the OpenID Connect callback, which hands its code to the
-- relying party's registered address, this one hands the assertion to the
-- browser that asked for it, to be posted onward.
--
-- So the id stops being sufficient. This column holds the SHA-256 of a value
-- generated when the request is completed — inside the authenticated API
-- call, where the person is known — and returned only in that response. The
-- callback must present it. An id recovered from a log is now an id, and the
-- one place the secret ever appeared is the response to a request that
-- required the session it belongs to.
--
-- Stored as a hash for the same reason authorization codes and refresh
-- tokens are: a value that grants something should not be readable in a
-- database dump.
--
-- NOT NULL DEFAULT '' rather than nullable, and the empty string never
-- matches. A request completed before this migration and still in flight
-- during the upgrade is refused at its callback and the person signs in
-- again; the alternative — treating "no secret recorded" as "no secret
-- required" — is the check being optional in a way nothing would notice.
ALTER TABLE saml_auth_requests
    ADD COLUMN completion_secret TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN saml_auth_requests.completion_secret IS
    'SHA-256 of a one-time value issued when the request is completed and required at the callback, so knowing the request id is not enough to be handed an assertion. Empty means no assertion can be minted.';

-- +goose Down
ALTER TABLE saml_auth_requests DROP COLUMN completion_secret;
