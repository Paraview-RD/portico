# Integrations

Third-party platforms, APIs, and services this project depends on at
runtime.

## Runtime services

**None.**

That is deliberate rather than an omission. Portico has no external
identity provider, no message broker, no cache, no object store, and no
separate database server — the database is a file, and the frontend is
compiled into the binary. A deployment is one process with one volume.

Consequences worth stating plainly:

- There is no third-party account to own, no API key to rotate, and no
  vendor bill.
- There is also no horizontal scaling story. SQLite permits a single
  writer, and the server holds one connection. This suits the "single-node,
  low-ops" case the requirements describe (§5.1) and does not suit a
  multi-instance deployment behind a load balancer.

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
