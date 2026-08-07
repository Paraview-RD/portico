# Keylite

A lightweight, self-hostable Identity & Access Management system. One Go
binary with the web UI compiled in, a SQLite file for storage, and no
external services — built for the "we just need accounts, sign-in, and two
roles" case that full IAM platforms over-serve.

Installing it is downloading one file and running it. The server creates its
database, applies its migrations, prints an administrator password once, and
serves the UI on <http://localhost:8410>.

```bash
# Pick the build for your platform from the releases page
curl -LO https://github.com/paraview/keylite/releases/latest/download/keylite_0.1.0_linux_amd64.tar.gz
tar -xzf keylite_0.1.0_linux_amd64.tar.gz
KEYLITE_JWT_SECRET=$(openssl rand -hex 32) ./keylite
```

Releases ship binaries for Linux, macOS, and Windows on amd64 and arm64, each
with an SPDX SBOM, plus a multi-architecture image at
`ghcr.io/paraview/keylite`. The checksum file is signed through Sigstore —
[verification instructions](https://github.com/paraview/keylite/releases/latest)
are on every release.

> **Before exposing this to a network:** Keylite serves plain HTTP and does
> not rate-limit sign-in attempts, both deliberately. It must run behind a
> reverse proxy that terminates TLS and throttles `/api/v1/auth/*` — see
> [SECURITY.md](SECURITY.md) for why and
> [docs/access-guide.md](docs/access-guide.md) for working nginx and Caddy
> configurations.

## What it does

- **Accounts** — create, edit, enable/disable, reset passwords, search.
  Accounts are never deleted, so the audit trail stays intact.
- **Bulk import** — migrate existing users from an `.xlsx` file. Rows are
  independent: valid rows import even when others fail, and you get a
  per-row report of what to fix.
- **Self-registration** — optional, off by default, toggled at runtime.
- **Two fixed roles** — administrator and user. No RBAC to configure.
- **Organizations** — a single flat tier for grouping users.
- **Sign-in and sessions** — self-issued JWTs. Logout, a password change,
  and disabling an account all revoke live sessions immediately.
- **Audit log** — sign-ins, operations, authorization, registrations, and
  organization changes, filterable by type and time range.
- **Downstream integration** — two endpoints let a business system identify
  the caller and sync their profile and organization.
- **Bilingual UI** — English and 简体中文, switchable at runtime.

Deliberately **not** included: OAuth2/OIDC/SAML, multi-application
isolation, third-party or LDAP sign-in, custom roles and permissions, MFA,
and rate limiting. See
[docs/requirements/mvp-requirements.md](docs/requirements/mvp-requirements.md)
for the full scope and the post-MVP direction.

> **Status:** feature-complete against the MVP scope and verified end to
> end, but not yet used in production anywhere. Treat it as early software.

## Running it

### Binary

The frontend has to be built first — it is compiled into the binary, so a
Go-only build produces a working API with no UI (the server says so rather
than serving a blank page).

```bash
cd web && npm ci && npm run build && cd ..
go build -o keylite ./cmd/server
KEYLITE_JWT_SECRET=$(openssl rand -hex 32) ./keylite
```

Requires Go 1.25.7+ (a dependency sets that floor) and Node 22+.

### Docker

Nothing to install but Docker — the image build runs both steps itself.

```bash
export KEYLITE_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

Configuration is entirely environment variables — see
[.env.example](.env.example) for the full list. Every one has a working
default except `KEYLITE_JWT_SECRET`, which you should set explicitly:
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
  model/           domain types
  web/             embeds the built frontend
migrations/        schema, embedded and applied at startup
web/               React + Vite frontend
docs/              conventions, requirements, access guide
deploy/            Dockerfile and compose
```

## Design notes

Two choices explain most of the rest:

**SQLite by default, behind a driver check.** The requirements ask for
single-node deployment with no operational burden, and a file-backed
database is what makes the one-binary install honest. The driver is pure Go,
so the binary cross-compiles and runs in a `scratch` container. The tradeoff
is real: SQLite allows one writer, so this does not scale horizontally.

**Stateless tokens with a revocation counter.** Each account carries a
`token_version` that logout, password changes, and disabling all increment.
The middleware re-reads the account per request and rejects stale versions.
That costs one indexed read and buys immediate revocation without a denylist
to keep consistent.

Conventions this project holds itself to:
[API](docs/api-conventions.md) ·
[database](docs/database-conventions.md) ·
[UI](docs/design-principles.md).

## Contributing

Issues and pull requests are welcome. Every commit needs a DCO sign-off
(`git commit -s`); CI enforces it. [CONTRIBUTING.md](CONTRIBUTING.md) has a
tested walkthrough from clone to running server.

Participation is governed by our
[Code of Conduct](CODE_OF_CONDUCT.md).

**Found a security problem?** Do not open an issue — report it privately
through [Security → Report a vulnerability](https://github.com/paraview/keylite/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
