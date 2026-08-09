-- name: CreateOAuthClient :exec
INSERT INTO oauth_clients (
    id, tenant_id, client_id, name, secret_hash,
    application_type, auth_method,
    redirect_uris, post_logout_redirect_uris, grant_types, scopes,
    launch_url, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE tenant_id = $1 AND client_id = $2 LIMIT 1;

-- name: ListOAuthClients :many
SELECT * FROM oauth_clients WHERE tenant_id = $1 ORDER BY created_at;

-- name: UpdateOAuthClientStatus :exec
UPDATE oauth_clients
SET status = $1, updated_at = $2
WHERE tenant_id = $3 AND client_id = $4;

-- The client_id is not in the SET list. It is the name the application
-- presents at the token endpoint, so changing it would silently break every
-- deployment of that application rather than reconfigure it.
-- name: UpdateOAuthClient :exec
UPDATE oauth_clients
SET name = $1,
    application_type = $2,
    redirect_uris = $3,
    post_logout_redirect_uris = $4,
    scopes = $5,
    launch_url = $6,
    updated_at = $7
WHERE tenant_id = $8 AND client_id = $9;

-- name: UpdateOAuthClientSecret :exec
UPDATE oauth_clients
SET secret_hash = $1, updated_at = $2
WHERE tenant_id = $3 AND client_id = $4;
