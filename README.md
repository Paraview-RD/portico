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
stays intact. Users belong to one organization.

**Sign in three ways** — username, phone, or email, all producing the same
credential.

**Self-service** — registration (optional, off by default), password change,
password recovery by email or SMS, and profile maintenance, with no
administrator in the loop.

**Sessions that actually revoke** — logout, a password change, and disabling
an account each invalidate live sessions immediately, not at token expiry.

**Audit log** — sign-ins, operations, authorization, registrations, and
organization changes, filterable by type and time range.

**Bilingual UI** — English and 简体中文, switchable at runtime.

Deliberately **not** in this version: custom roles and permissions (there are
two fixed roles), third-party and social login, SCIM, webhooks, MFA, and rate
limiting. The roadmap for those is in
[docs/requirements/v0.1-requirements.md](docs/requirements/v0.1-requirements.md).

> **Before exposing this to a network:** Portico serves plain HTTP and does
> not rate-limit sign-in attempts, both deliberately. It must run behind a
> reverse proxy that terminates TLS and throttles `/api/v1/auth/*` — see
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
