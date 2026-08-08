# Portico

A self-hostable identity platform: standard single sign-on, multi-tenant
isolation, and a complete self-service flow — a single Go binary with the
web UI compiled in, backed by PostgreSQL.

> **Status: pre-release, under active development.** Nothing has been
> published yet, and the protocol support described below is being built.
> See [CHANGELOG.md](CHANGELOG.md) for what actually exists today.

## What it does

**Standard single sign-on** — OAuth 2.1, OpenID Connect 1.0, SAML 2.0, and
CAS, so existing business systems integrate without a bespoke protocol.

**Multi-tenant** — every table carries a tenant, and isolation is enforced
in the query layer rather than left to reviewer discipline. Each tenant has
its own administrators; tenants are provisioned from the command line, so no
role exists that can see across all of them.

**Accounts and organizations** — create, edit, enable/disable, bulk-import
from a spreadsheet. Accounts are disabled, never deleted, so the audit trail
stays intact. Users belong to one organization; organizations form a tree.

**Sign in three ways** — username, phone, or email, all producing the same
credential.

**Self-service** — registration (optional, off by default), password change,
password recovery by email, and profile maintenance, with no administrator in
the loop. Password rules are per tenant: a minimum length that no policy can
lower, plus optional composition rules, reuse checks, and expiry — the last
three off by default, and documented as the compliance features they are
rather than the security ones they are not. Recovery needs an SMTP relay; point `PORTICO_SMTP_HOST` at whatever
you already run. SMS recovery is defined as a provider interface and ships
without one.

**Sessions that revoke** — every sign-in is listed on your own profile with
the address and browser it came from, and any of them can be ended on its
own. Signing out ends that one; sign out everywhere, a password change, and
disabling an account each end all of them immediately, along with every
refresh token held by a federated application. What no identity
provider can withdraw is a credential somebody else is already holding — an
access token verified offline, or a session an application created for
itself after accepting an assertion. [docs/federation.md](docs/federation.md)
has a table of exactly what revocation reaches, per protocol, and it is
worth reading before deploying.

**Audit log** — sign-ins, operations, authorization, registrations,
organization changes, and every application registration, filterable by type
and time range.

**Bilingual UI** — English and 简体中文, switchable at runtime.

Deliberately **not** in this version: custom roles and permissions (there are
two fixed roles), third-party and social login, SCIM, webhooks, MFA, and
request rate limiting. The roadmap for those is in
[docs/requirements/v0.1-requirements.md](docs/requirements/v0.1-requirements.md).

> **Before exposing this to a network:** Portico serves plain HTTP and does
> not rate-limit requests, both deliberately. Accounts do lock after repeated
> failed sign-ins, but that is a different control: it stops one account's
> password being guessed and does nothing about the load a flood of attempts
> puts on the server. It must run behind a reverse proxy that terminates TLS
> and throttles `/api/v1/auth/*` — see
> [SECURITY.md](SECURITY.md) for why and
> [docs/access-guide.md](docs/access-guide.md) for working nginx and Caddy
> configurations.

## Running it

### Binary

Needs a PostgreSQL instance to point at. The frontend has to be built first —
it is compiled into the binary, so a Go-only build produces a working API
with no UI (the server says so rather than serving a blank page).

```bash
cd web && npm ci && npm run build && cd ..
go build -o portico ./cmd/server

PORTICO_DB_DSN=postgres://portico:portico@localhost:5432/portico?sslmode=disable \
PORTICO_JWT_SECRET=$(openssl rand -hex 32) ./portico
```

Requires Go 1.25.7+ (a dependency sets that floor) and Node 22+.

### Docker

Brings up PostgreSQL alongside the server; nothing else to install.

```bash
export POSTGRES_PASSWORD=$(openssl rand -hex 16)
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

Configuration is entirely environment variables — see
[.env.example](.env.example) for the full list. Every one has a working
default except `PORTICO_JWT_SECRET`, which you should set explicitly:
without it a random secret is generated per start and every session dies on
restart.

### Tenants

First start creates one tenant, code `default`. Sign-in that names no tenant
lands there, so a single-tenant deployment can ignore this section entirely.

More are provisioned from the command line — there is no cross-tenant role
for an API to authorize:

```bash
portico tenant create --code acme --name "Acme Corp"
portico tenant list
portico tenant disable --code acme     # refuses sign-in, deletes nothing
```

Each gets its own administrator; the password is printed once unless you
pass `--admin-password`. Its users sign in with the tenant code, typed into
the **Tenant** field or carried by a link: `/login?tenant=acme`.

### Applications

Three protocols, one set of accounts. Which to use is decided by what the
application already speaks; all three answer with the same facts under the
same names.

Register them in the console under **Applications** — one tab per protocol,
with an integration panel that hands back the issuer, discovery document,
SAML metadata and certificate, and CAS server URL to paste into the other
side. Or from the command line, which is the same service underneath and so
the same rules and the same audit trail:

```bash
# OpenID Connect / OAuth 2.1 — point the library at the issuer and register
portico client register --id grafana --name Grafana \
  --redirect-uri https://grafana.example.com/login/generic_oauth

# SAML 2.0 — exchange metadata documents, in both directions
portico sp register --metadata ./sp-metadata.xml --name Confluence

# CAS 2.0/3.0 — register the URL prefix a ticket may be delivered to
portico cas register --url https://wiki.example.com/ --name Wiki
```

Everything hangs off `PORTICO_PUBLIC_URL` for the default tenant and
`PORTICO_PUBLIC_URL/t/<code>` for any other: the OIDC issuer at the root,
SAML metadata at `/saml/metadata`, CAS at `/cas`.
[docs/federation.md](docs/federation.md) covers the details, including what
is deliberately not implemented and why.

## Developing

```bash
# Backend, with live reload of your own choosing
go run ./cmd/server

# Frontend, proxying /api to the backend above
cd web && npm install && npm run dev   # http://localhost:5410
```

```bash
go test ./...                 # backend
cd web && npm run build       # frontend typecheck + build
```

Changing a SQL query means regenerating the query layer:

```bash
sqlc generate    # brew install sqlc
```

The generated code is committed, so contributors only need `sqlc` when they
touch `internal/store/queries/`.

## Layout

```
cmd/server/        entry point
internal/
  auth/            passwords, JWTs, authentication middleware
  handler/         HTTP handlers
  service/         business rules
  store/           database access; sqlcgen/ is generated
  testdb/          throwaway PostgreSQL for tests
  model/           domain types
  casp/            the CAS protocol, implemented directly
  oidcp/           adapts Portico to the OpenID Provider interface
  samlp/           adapts Portico to the SAML identity provider role
  provision/       tenant and client provisioning, for the CLI
  web/             embeds the built frontend
migrations/        schema, embedded and applied at startup
web/               React + Vite frontend
docs/              conventions, requirements, access guide
deploy/            Dockerfile and compose
```

## Design notes

Two choices explain most of the rest:

**PostgreSQL, reached through the pure-Go pgx driver.** An earlier version
used SQLite, and a file-backed database was the right trade while the scope
was a single-tenant intranet tool. Multi-tenancy and public-facing SSO
changed that: tenant isolation wants real constraints, and a single-writer
database is the wrong shape for an identity provider several systems depend
on. The driver needs no cgo, so the binary still cross-compiles and still
ships in a `scratch` container — the cost is that a deployment is now two
processes rather than one.

**Stateless tokens with a revocation counter.** Each account carries a
`token_version` that logout, password changes, and disabling all increment.
The middleware re-reads the account per request and rejects stale versions.
That costs one indexed read and buys immediate revocation without a denylist
to keep consistent.

Conventions this project holds itself to — all in [docs/](docs/):
[code](docs/code-conventions.md) ·
[config](docs/configuration-conventions.md) ·
[API](docs/api-conventions.md) ·
[federation](docs/federation.md) ·
[database](docs/database-conventions.md) ·
[errors](docs/error-conventions.md) ·
[logging](docs/logging-conventions.md) ·
[i18n](docs/i18n-conventions.md) ·
[UI](docs/design-principles.md).

## Contributing

Issues and pull requests are welcome. Every commit needs a DCO sign-off
(`git commit -s`); CI enforces it. [CONTRIBUTING.md](CONTRIBUTING.md) has a
tested walkthrough from clone to running server.

Participation is governed by our
[Code of Conduct](CODE_OF_CONDUCT.md).

**Found a security problem?** Do not open an issue — report it privately
through [Security → Report a vulnerability](https://github.com/paraview/portico/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
