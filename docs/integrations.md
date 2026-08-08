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

| Setting | Meaning |
|---|---|
| `PORTICO_SMTP_HOST` | Relay hostname. Empty means email recovery is unavailable, which is the default. |
| `PORTICO_SMTP_PORT` | Default `587`. |
| `PORTICO_SMTP_USERNAME` / `PORTICO_SMTP_PASSWORD` | Omit both to connect without authentication, which is normal for a relay on a private network. |
| `PORTICO_SMTP_FROM` | Envelope and header sender. Required once a host is set. |
| `PORTICO_SMTP_ENCRYPTION` | `starttls` (default), `tls`, or `none`. |
| `PORTICO_PUBLIC_URL` | Where the links in those messages point. |

STARTTLS is required rather than opportunistic. Opportunistic STARTTLS can be
stripped by anyone on the path, and the message carries a password-reset
link, so a silent downgrade is the whole threat.

No credential belongs in the repository. `.env.example` lists the variable
names only.

### SMS — optional

Password recovery by phone needs an SMS gateway, and unlike email there is
no universal protocol for one. Portico defines a small provider interface;
a deployment supplies an implementation, or leaves SMS off and uses email
only.

*Status: the interface and a no-op implementation exist; concrete providers
are not yet written.*

### Prometheus — optional, and it does the reaching

Setting `PORTICO_METRICS_ADDR` opens a second listener publishing metrics in
the text exposition format. There is no account, no token, and no
configuration beyond the address: Prometheus scrapes Portico, so nothing is
sent anywhere and no credential is held.

The direction is the point. This is not telemetry — nothing leaves unless
something inside your network comes and reads it, and if you never set the
address, the listener does not exist. See
[access-guide.md](access-guide.md#metrics) for why it is a separate port.

## Nothing else

No message broker, no cache, no object store, no external identity provider.
Nothing phones home — the metrics endpoint above is read by your monitoring,
never pushed by Portico.

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
| `github.com/jackc/pgx/v5` | The PostgreSQL driver, used through `database/sql`. Pure Go, so the binary still cross-compiles freely and runs in a `scratch` container with no libc. |
| `github.com/wneessen/go-mail` | Builds and sends the recovery messages. Chosen over `net/smtp` for correct address and header encoding — getting that wrong is how a crafted recipient turns into extra envelope commands. |
| `github.com/pressly/goose/v3` | Used as a library, not a CLI, so migrations run at startup and there is no separate tool to ship or run. |
| `github.com/xuri/excelize/v2` | Reads and writes the bulk-import workbooks. |
| `github.com/golang-jwt/jwt/v5` | Signs and verifies access tokens. |
| `github.com/prometheus/client_golang` | The metrics registry and exposition handler. Adds no runtime dependency: with `PORTICO_METRICS_ADDR` unset it registers collectors nothing reads and opens no listener. |
| `sqlc` | Development-time code generation from SQL. Contributors only need it when changing a query; the generated code is committed. |
