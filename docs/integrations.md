# Integrations

Third-party platforms, APIs, and services this project depends on at
runtime.

## Runtime services

### PostgreSQL — required

The only hard dependency. Version 17 is what is tested; 14 and later should
work.

An earlier version stored everything in a SQLite file, which made a
deployment one process with one volume. Multi-tenancy and public-facing SSO
changed the calculus: isolation wants real constraints, and a single-writer
database is the wrong shape for an identity provider that other systems
depend on being available.

Configured entirely through `PORTICO_DB_DSN`. There is no default — a
connection string for someone else's database is not something to guess.

### SMTP — required for password recovery

Password recovery sends a message, so it needs a way to send one. Portico
speaks plain SMTP rather than any provider's SDK: a self-hosted deployment
should be able to point at whatever it already runs, whether that is a
company relay, Amazon SES, Postmark, Resend, or a local Postfix.

There is no vendor account to create and no API key held by this project.

### SMS — optional

Password recovery by phone needs an SMS gateway, and unlike email there is
no universal protocol for one. Portico defines a small provider interface;
a deployment supplies an implementation, or leaves SMS off and uses email
only.

*Status: the interface and a no-op implementation exist; concrete providers
are not yet written.*

## Nothing else

No message broker, no cache, no object store, no external identity provider,
no telemetry endpoint. Nothing phones home.

## Adding one

If a future change introduces an external dependency, add it here in the
same commit, with: what it is for, how it authenticates, who owns the
account, what it costs, and which environment variables it needs. Also add
those variables to `.env.example`.

## Notable build-time dependencies

Not third-party *services*, but worth naming because they shape the
deployment.

| Dependency | Why it matters |
|---|---|
| `modernc.org/sqlite` | A pure-Go SQLite driver. No cgo means the binary cross-compiles freely and runs in a `scratch` container with no libc. A cgo-based driver would forfeit both. |
| `github.com/pressly/goose/v3` | Used as a library, not a CLI, so migrations run at startup and there is no separate tool to ship or run. |
| `github.com/xuri/excelize/v2` | Reads and writes the bulk-import workbooks. |
| `github.com/golang-jwt/jwt/v5` | Signs and verifies access tokens. |
| `sqlc` | Development-time code generation from SQL. Contributors only need it when changing a query; the generated code is committed. |
