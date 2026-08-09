-- name: CreateCASService :exec
INSERT INTO cas_services (
    id, tenant_id, name, url_prefix, launch_url,
    status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListCASServices :many
SELECT * FROM cas_services
WHERE tenant_id = $1
ORDER BY url_prefix;

-- name: GetCASService :one
SELECT * FROM cas_services
WHERE tenant_id = $1 AND url_prefix = $2
LIMIT 1;

-- Looked up by id for the same reason as a SAML service provider: a URL
-- prefix in a path has to be percent-encoded, and a normalizing proxy
-- decodes it.
-- name: GetCASServiceByID :one
SELECT * FROM cas_services
WHERE tenant_id = $1 AND id = $2
LIMIT 1;

-- name: UpdateCASServiceStatus :exec
UPDATE cas_services
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND url_prefix = $4;

-- Matched on the old prefix and able to set a new one, because a prefix is
-- a deployment address rather than an identity: an application that moves to
-- a new host has to be editable, or the only way to follow it is to
-- de-register and re-register.
-- name: UpdateCASService :exec
UPDATE cas_services
SET name = $1, url_prefix = $2, launch_url = $3, updated_at = $4
WHERE tenant_id = $5 AND url_prefix = $6;

-- name: CreateCASTicket :exec
INSERT INTO cas_tickets (
    id, tenant_id, ticket_hash, service, subject, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ConsumeCASTicket :one
-- Single use, as one statement. A read followed by a write would let two
-- validations of the same ticket both see it unconsumed and both succeed,
-- which is the whole property a service ticket has.
UPDATE cas_tickets
SET consumed_at = $1
WHERE tenant_id = $2
  AND ticket_hash = $3
  AND consumed_at IS NULL
  AND expires_at > $1
RETURNING *;

-- name: DeleteExpiredCASTickets :exec
DELETE FROM cas_tickets WHERE tenant_id = $1 AND expires_at < $2;
