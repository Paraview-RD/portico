# Access Guide

How to reach a running Portico instance, where credentials come from, and
what each role can actually do.

## Entry points

| What | URL | Notes |
|---|---|---|
| Web UI | `http://<host>:8410/` | Served by the same process as the API |
| API | `http://<host>:8410/api/v1/` | See [api-conventions.md](api-conventions.md) |
| Health check | `http://<host>:8410/api/v1/health` | No authentication; safe for load balancers |
| OpenID discovery | `http://<host>:8410/.well-known/openid-configuration` | The default tenant. Others at `/t/<code>/…` |
| SAML metadata | `http://<host>:8410/saml/metadata` | Hand this to a service provider. Others at `/t/<code>/saml/metadata` |
| CAS | `http://<host>:8410/cas` | The client's "CAS server URL". Others at `/t/<code>/cas` |

The port is `PORTICO_ADDR` (default `:8410`). In development the frontend
also runs a Vite server on `:5410` that proxies `/api` to `:8410`; in
production there is only the one port.

## Tenants

Every account belongs to exactly one tenant, and no account can act outside
its own. A fresh deployment gets one tenant, code `default`, created on
first start — sign-in that names no tenant lands there, so a single-tenant
deployment never has to know tenants exist.

More tenants are provisioned from the command line, because there is no
cross-tenant role for the API to authorize:

```sh
export PORTICO_DB_DSN=postgres://portico:secret@localhost:5432/portico?sslmode=disable

portico tenant create --code acme --name "Acme Corp"   # prints a generated
                                                       # admin password once
portico tenant list
portico tenant disable --code acme                     # refuses sign-in,
portico tenant enable  --code acme                     # deletes nothing
```

Each tenant gets its own administrator at creation. Users of a non-default
tenant sign in with its code — either typed into the **Tenant** field, or
via a link that carries it:

```
http://<host>:8410/login?tenant=acme
```

## Credentials

Portico stores its own accounts — there is no external identity provider in
the MVP.

| Credential | Where it comes from |
|---|---|
| Bootstrap administrator | Created on first start in the `default` tenant. Username from `PORTICO_INITIAL_ADMIN_USERNAME` (default `admin`). |
| A further tenant's administrator | Created by `portico tenant create`; the password is printed once unless `--admin-password` is given. |
| Bootstrap password | `PORTICO_INITIAL_ADMIN_PASSWORD` if set. Otherwise a random one is generated and **printed once** in the startup log — capture it then, it is stored nowhere. |
| JWT signing secret | `PORTICO_JWT_SECRET`. If unset, a random secret is generated per start, which silently invalidates every session on restart. Set it. |
| Everyone else's password | Set by an administrator at creation, chosen by the user at self-registration, or set through a password-recovery link. |

No credential values belong in this file, in the repository, or in a
committed `.env`. `.env.example` lists the variable names only.

## First run

1. Start the server (`./portico`, or `docker compose -f deploy/docker-compose.yml up -d`).
2. Read the startup log for the generated administrator password, unless you
   set one.
3. Open the UI, sign in, and change that password from **My profile**.
   Changing it signs you out — that is expected, since a password change
   revokes every existing token.

## Password recovery

A user who cannot sign in asks for a reset link from the sign-in screen. The
link is single-use and expires in 30 minutes.

**It needs a mail relay.** With `PORTICO_SMTP_HOST` unset, the recovery
screen says so plainly rather than accepting a request it cannot fulfil, and
the only way back in is an administrator resetting the password. Set
`PORTICO_SMTP_HOST`, `PORTICO_SMTP_FROM`, and `PORTICO_PUBLIC_URL` — the
last one because the link has to point somewhere, and a server behind a
proxy cannot work that out for itself.

While developing, point it at a local catcher rather than a real relay:

```sh
docker run -d -p 1025:1025 -p 8025:8025 axllent/mailpit

export PORTICO_SMTP_HOST=127.0.0.1
export PORTICO_SMTP_PORT=1025
export PORTICO_SMTP_ENCRYPTION=none
export PORTICO_SMTP_FROM=portico@example.com
export PORTICO_PUBLIC_URL=http://localhost:8410
```

Messages then appear at <http://localhost:8025> and nothing leaves the
machine.

Recovery by SMS is defined but has no provider in this version, so the
sign-in screen offers email only.

## Roles

The MVP has exactly two roles and no way to define more (requirements §3.3).

### Administrator (`SUPER_ADMIN`)

Can do everything: create/edit users, enable and disable accounts, reset
other people's passwords, bulk-import from a spreadsheet, manage
organizations, register the applications that sign in through Portico, read
the audit log, and change system settings.

Everything an administrator does is scoped to their own tenant. There is no
cross-tenant role.

Typical journey — onboard a batch of existing users:

1. **Organizations** → create the organizations first, noting their codes.
2. **Users** → **Import** → download the template, fill it in (the
   `organizationCode` column matches the codes from step 1), upload.
3. Read the per-row result. Failed rows are listed with the row number and
   reason; fix those rows and re-upload just them.
4. **Audit logs** → filter to *Registration* to confirm what landed.

Typical journey — connect an application:

1. **Applications** → pick the protocol tab the application speaks.
2. **Register** — for OAuth/OIDC give it a client id and its redirect URIs;
   for SAML upload or paste the service provider's metadata document; for
   CAS give its URL prefix.
3. A confidential OAuth client's secret is shown **once**. Copy it then —
   only a hash is stored, so the only way to get another is **Rotate
   secret**, which invalidates the current one immediately.
4. **Integration endpoints** → copy the issuer, discovery document, SAML
   metadata address, or CAS server URL into the application's own
   configuration. These come from the running deployment, so they always
   match what is actually served.
5. **Audit logs** → the registration is recorded with the redirect URIs or
   assertion consumer services it permits.

Disabling an application stops it immediately — it can no longer sign anyone
in, and its credentials stop authenticating at the introspection and
revocation endpoints too. Nothing is deleted, and there is no delete: the
audit trail keeps pointing at something that still exists.

### User (`USER`)

Can sign in, view their own profile, and change their own password. Nothing
else — the navigation only shows **My profile**, and the server rejects
administrative calls regardless of what the UI shows.

Self-registered accounts always get this role; it cannot be requested at
sign-up.

## Before you expose this

Portico serves plain HTTP and does not rate-limit sign-in attempts. Both are
deliberate — it delegates them to the reverse proxy rather than
reimplementing them — but that makes the proxy mandatory, not optional, for
anything reachable beyond localhost. See [SECURITY.md](../SECURITY.md) for
why.

The bundled compose file binds to `127.0.0.1` so the default configuration
is not directly reachable.

### nginx

```nginx
limit_req_zone $binary_remote_addr zone=portico_auth:10m rate=10r/m;

server {
    listen 443 ssl http2;
    server_name id.example.com;

    ssl_certificate     /etc/letsencrypt/live/id.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/id.example.com/privkey.pem;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    # Throttle the credential endpoints; everything else is already
    # authenticated.
    location ~ ^/api/v1/auth/(login|register)$ {
        limit_req zone=portico_auth burst=5 nodelay;
        proxy_pass http://127.0.0.1:8410;
        include /etc/nginx/proxy_params;
    }

    location / {
        proxy_pass http://127.0.0.1:8410;
        include /etc/nginx/proxy_params;
    }
}
```

### Caddy

```caddyfile
id.example.com {
    @auth path /api/v1/auth/login /api/v1/auth/register
    rate_limit @auth {
        zone portico_auth {
            key    {remote_host}
            events 10
            window 1m
        }
    }
    reverse_proxy 127.0.0.1:8410
}
```

With either in place, set `PORTICO_TRUST_PROXY_HEADERS=true` so the audit log
records real client addresses rather than the proxy's. Do **not** set it
without a proxy: callers could then forge the IP attributed to their own
actions.

## Guard rails worth knowing

- **Accounts are never deleted.** Disabling is the substitute, so the audit
  trail stays intact. A disabled user is signed out immediately.
- **The last administrator cannot be disabled or demoted**, and nobody can
  disable their own account. Both are otherwise unrecoverable without
  database surgery.
- **Registration is off by default.** Turn it on in **Settings** when you
  want it; an instance exposed before anyone finishes setup will not accept
  sign-ups.
- **Disabling an organization** blocks new members but keeps existing ones.
- **A reset link ends every session.** Completing recovery changes the
  password, and a password change revokes every live token — which is the
  point, since the reason for recovering is often that somebody else had it.
- **Asking for a second link invalidates the first.** Only the newest one
  works.
- **Nothing crosses a tenant.** Users, organizations, audit entries, and
  settings all belong to one, and an administrator of one tenant cannot see
  or change another's — including by id, and including by sending a tenant
  header. There is no account anywhere that can.

## Integrating a downstream system

A downstream application receives the user's token and calls:

- `GET /api/v1/users/me` — the user's profile and organization, which is
  everything needed to create or update a local record (requirements §3.8.2).
- `GET /api/v1/auth/permission-check` — whether this caller is an
  administrator, for gating the downstream system's own admin screens.

Both take the user's own bearer token. There are no service accounts or
machine-to-machine credentials in the MVP.

That path suits a system that already has the user's Portico token. A
system that needs to sign people in itself should use OpenID Connect
instead — point its library at the issuer and register it with
`portico client register`. See [federation.md](federation.md).
