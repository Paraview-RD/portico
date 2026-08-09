-- +goose Up

-- The picture on an application's tile.
--
-- Named after RFC 7591's `logo_uri`, which is what an OAuth client's own
-- metadata calls this, so an integrator reading a registration recognizes
-- the field. SAML says the same thing as mdui:Logo and CAS says nothing at
-- all; one name across the three keeps the portal from caring which
-- protocol a tile came from.
--
-- Optional, and absence is not a defect: an application without one gets a
-- tile bearing the first character of its name, which is legible, needs no
-- network, and cannot break. What the column buys is recognition — people
-- find an application on a portal by its logo far faster than by reading
-- six names.
--
-- The value may be an absolute http(s) address or a path on this server.
-- The second is the interesting one: a deployment that ships its own icons
-- under /icons keeps the portal working with no outbound network at all,
-- and tells no third party who opened it.
--
-- Empty string rather than NULL, matching launch_url and every other
-- optional text in this schema.
--
-- A third migration rather than an edit to the second, for the reason the
-- second gave for not editing the first: there is a database in use now.
ALTER TABLE oauth_clients
    ADD COLUMN logo_uri TEXT NOT NULL DEFAULT '';
ALTER TABLE saml_service_providers
    ADD COLUMN logo_uri TEXT NOT NULL DEFAULT '';
ALTER TABLE cas_services
    ADD COLUMN logo_uri TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN oauth_clients.logo_uri IS
    'Picture for this application''s portal tile. Absolute http(s) or a path on this server.';
COMMENT ON COLUMN saml_service_providers.logo_uri IS
    'Picture for this application''s portal tile. Absolute http(s) or a path on this server.';
COMMENT ON COLUMN cas_services.logo_uri IS
    'Picture for this application''s portal tile. Absolute http(s) or a path on this server.';

-- +goose Down

ALTER TABLE cas_services DROP COLUMN logo_uri;
ALTER TABLE saml_service_providers DROP COLUMN logo_uri;
ALTER TABLE oauth_clients DROP COLUMN logo_uri;
