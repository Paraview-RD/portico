-- +goose Up

-- Where a person goes to use an application.
--
-- None of the addresses already registered is one. A redirect URI is where
-- an authorization code is delivered, an assertion consumer service is where
-- an assertion is posted, and a CAS URL prefix is a matching rule. All three
-- are places a protocol sends a browser mid-flow, and opening any of them
-- directly produces an error rather than the application.
--
-- A portal needs the other thing: the address a person would have typed. So
-- it is stored, optionally — an application without one is still registered
-- and still signs people in, it just does not appear as something to open.
--
-- Empty string rather than NULL, matching how this schema treats every other
-- optional text: there is one absent value, and queries do not have to
-- handle two.
--
-- A second migration rather than an edit to the first, though nothing has
-- been released. The first database somebody cares about is the point at
-- which editing history stops being free, and by now there is one.
ALTER TABLE oauth_clients
    ADD COLUMN launch_url TEXT NOT NULL DEFAULT '';
ALTER TABLE saml_service_providers
    ADD COLUMN launch_url TEXT NOT NULL DEFAULT '';
ALTER TABLE cas_services
    ADD COLUMN launch_url TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN oauth_clients.launch_url IS
    'Where a person opens this application. Not a redirect URI.';
COMMENT ON COLUMN saml_service_providers.launch_url IS
    'Where a person opens this application. Not an assertion consumer service.';
COMMENT ON COLUMN cas_services.launch_url IS
    'Where a person opens this application. Not the URL prefix.';

-- +goose Down

ALTER TABLE cas_services DROP COLUMN launch_url;
ALTER TABLE saml_service_providers DROP COLUMN launch_url;
ALTER TABLE oauth_clients DROP COLUMN launch_url;
