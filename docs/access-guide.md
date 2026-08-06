# Access Guide

How to reach a running Keylite instance, where credentials come from, and
what each role can actually do.

## Entry points

| What | URL | Notes |
|---|---|---|
| Web UI | `http://<host>:8410/` | Served by the same process as the API |
| API | `http://<host>:8410/api/v1/` | See [api-conventions.md](api-conventions.md) |
| Health check | `http://<host>:8410/api/v1/health` | No authentication; safe for load balancers |

The port is `KEYLITE_ADDR` (default `:8410`). In development the frontend
also runs a Vite server on `:5410` that proxies `/api` to `:8410`; in
production there is only the one port.

## Credentials

Keylite stores its own accounts — there is no external identity provider in
the MVP.

| Credential | Where it comes from |
|---|---|
| Bootstrap administrator | Created on first start against an empty database. Username from `KEYLITE_INITIAL_ADMIN_USERNAME` (default `admin`). |
| Bootstrap password | `KEYLITE_INITIAL_ADMIN_PASSWORD` if set. Otherwise a random one is generated and **printed once** in the startup log — capture it then, it is stored nowhere. |
| JWT signing secret | `KEYLITE_JWT_SECRET`. If unset, a random secret is generated per start, which silently invalidates every session on restart. Set it. |
| Everyone else's password | Set by an administrator at creation, or chosen by the user at self-registration. |

No credential values belong in this file, in the repository, or in a
committed `.env`. `.env.example` lists the variable names only.

## First run

1. Start the server (`./keylite`, or `docker compose -f deploy/docker-compose.yml up -d`).
2. Read the startup log for the generated administrator password, unless you
   set one.
3. Open the UI, sign in, and change that password from **My profile**.
   Changing it signs you out — that is expected, since a password change
   revokes every existing token.

## Roles

The MVP has exactly two roles and no way to define more (requirements §3.3).

### Administrator (`SUPER_ADMIN`)

Can do everything: create/edit users, enable and disable accounts, reset
other people's passwords, bulk-import from a spreadsheet, manage
organizations, read the audit log, and change system settings.

Typical journey — onboard a batch of existing users:

1. **Organizations** → create the organizations first, noting their codes.
2. **Users** → **Import** → download the template, fill it in (the
   `organizationCode` column matches the codes from step 1), upload.
3. Read the per-row result. Failed rows are listed with the row number and
   reason; fix those rows and re-upload just them.
4. **Audit logs** → filter to *Registration* to confirm what landed.

### User (`USER`)

Can sign in, view their own profile, and change their own password. Nothing
else — the navigation only shows **My profile**, and the server rejects
administrative calls regardless of what the UI shows.

Self-registered accounts always get this role; it cannot be requested at
sign-up.

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

## Integrating a downstream system

A downstream application receives the user's token and calls:

- `GET /api/v1/users/me` — the user's profile and organization, which is
  everything needed to create or update a local record (requirements §3.8.2).
- `GET /api/v1/auth/permission-check` — whether this caller is an
  administrator, for gating the downstream system's own admin screens.

Both take the user's own bearer token. There are no service accounts or
machine-to-machine credentials in the MVP.
