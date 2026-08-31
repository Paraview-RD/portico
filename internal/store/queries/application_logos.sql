-- name: CreateApplicationLogo :exec
INSERT INTO application_logos (
    id, tenant_id, content_type, bytes, sha256, byte_size, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetApplicationLogo :one
SELECT * FROM application_logos
WHERE tenant_id = $1 AND id = $2;

-- Orphans: uploaded, old enough that the form which would have referenced them
-- is long gone, and named by none of the four places a logo_uri-shaped value
-- can live.
--
-- An upload has to be stored before the registration form is saved, so
-- cancelling that form strands a row, and changing a logo strands the one it
-- replaced. The alternative to sweeping is reference counting across every
-- table that can hold a reference, which is more machinery than a few
-- kilobytes deserves.
--
-- The paths are matched by suffix rather than reconstructed, because the column
-- holds whatever was written into it and this query should not have to know how
-- the mount is spelled.
--
-- The fourth branch is system_settings, checked under both branding keys
-- that can hold a logo path (the login-page logo and the login-page
-- background image). Without it this sweep runs every ORPHAN_RETENTION_HOURS
-- and cannot see that a picture is in use, because the three tables above
-- are application registrations and a branding logo belongs to no
-- application — it is referenced from a tenant's settings, not from a row
-- in any of them.
-- name: DeleteOrphanedApplicationLogos :execrows
DELETE FROM application_logos
WHERE application_logos.tenant_id = $1
  AND application_logos.created_at < $2
  AND NOT EXISTS (
      SELECT 1 FROM oauth_clients c
      WHERE c.tenant_id = application_logos.tenant_id
        AND c.logo_uri LIKE '%/logos/' || application_logos.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM saml_service_providers p
      WHERE p.tenant_id = application_logos.tenant_id
        AND p.logo_uri LIKE '%/logos/' || application_logos.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM cas_services s
      WHERE s.tenant_id = application_logos.tenant_id
        AND s.logo_uri LIKE '%/logos/' || application_logos.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM system_settings ss
      WHERE ss.tenant_id = application_logos.tenant_id
        AND ss.key IN ('branding_logo_url', 'branding_bg_image_url')
        AND ss.value LIKE '%/logos/' || application_logos.id
  );
