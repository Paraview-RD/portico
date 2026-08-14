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

### SMTP — required for password recovery and for confirming a registration

Both send a message, so both need a way to send one; a tenant that requires
new accounts to confirm their address cannot turn that on without a relay
configured, and is told so at the point of turning it on. Portico
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

### Active Directory or OpenLDAP — optional, and the one Portico reaches out to

A directory connector reads accounts out of an AD or OpenLDAP. It is the only
integration where Portico opens the connection to a system somebody else
runs, and the only one where it stores a credential it later has to *use*
rather than merely check.

There is no vendor account and nothing to buy: it is your own directory, and
the account to use is a **read-only service account** you create in it.
Portico never writes to the directory, so a bind account with write access is
a standing risk for no benefit.

| Setting | Meaning |
|---|---|
| Server address, bind DN, bind password, base DN, filters, attribute map | Per connector, in the console under **Directory integration** — not environment variables, because a deployment may have several. |
| `PORTICO_ENCRYPTION_KEY` | 32 bytes of hex that AES-256-GCM seals the credentials this system has to be able to read back rather than merely check: a directory bind password, and the header values a webhook subscription sends. **Unset, saving either is refused** rather than storing it in the clear. Must differ from `PORTICO_JWT_SECRET`. |

The honest limit of that encryption: anyone who can read the process
environment can read the credential. What it defends against is the leak that
actually happens — a backup, a replica, a snapshot handed to somebody for
debugging.

**It is a data source, not a sign-in path.** Passwords are never read from
the directory and never written to it; an account synchronized in cannot be
authenticated with its AD password. Federating sign-in is a different feature
and does not exist. [ldap.md](ldap.md) has the attribute maps and the list of
what a synchronization refuses to do.

### Prometheus — optional, and it does the reaching

Setting `PORTICO_METRICS_ADDR` opens a second listener publishing metrics in
the text exposition format. There is no account, no token, and no
configuration beyond the address: Prometheus scrapes Portico, so nothing is
sent anywhere and no credential is held.

The direction is the point. This is not telemetry — nothing leaves unless
something inside your network comes and reads it, and if you never set the
address, the listener does not exist. See
[access-guide.md](access-guide.md#metrics) for why it is a separate port.

## Where the demonstration is hosted — this project's choice, not Portico's

Nothing below is a dependency of Portico. It is where *we* run the public
demonstration, recorded here because somebody has to know what the bill is
attached to and who can reach the database.

### Render — the public demo, free tier

| | |
|---|---|
| For | `render.yaml`: one web service built from `deploy/Dockerfile`, one Postgres |
| Auth | The Render account owning the Blueprint. No token is stored in this repository |
| Owner | The repository owner |
| Cost | **$0.** Two consequences below, both of them the price of that |
| Variables | `PORTICO_DB_DSN` (from the database), `PORTICO_JWT_SECRET` (generated), and `PORTICO_ENCRYPTION_KEY` / `PORTICO_PUBLIC_URL` entered by hand — see the comments in `render.yaml` |

**A free web service sleeps after 15 minutes without traffic.**
`.github/workflows/demo-keepalive.yml` asks it for `/api/v1/health` every ten
minutes, which helps and is not a fix: GitHub's scheduler is best-effort and
runs late under load, so the window sometimes elapses anyway and a visitor
pays for the cold start. Render's $7/month tier removes the problem outright.

**A free Postgres is deleted after 90 days.** Nothing works around that. The
demo and everything in it goes, and the Blueprint has to be applied again.

### GitHub Actions — the two jobs Render's free tier cannot run

Scheduled jobs are a paid feature on Render, and Actions minutes are unmetered
on a public repository, so both live here instead.

`demo-keepalive.yml` pokes the health endpoint. `demo-reseed.yml` drops the
schema and seeds a fresh one nightly, because a demo that signs everybody in
as an administrator becomes a demonstration of whatever the last visitor left
behind. It reaches the database directly over Render's external connection
string.

| Secret / variable | What it is |
|---|---|
| `DEMO_DATABASE_URL` (secret) | Render's **external** connection string, TLS required |
| `DEMO_SEED_PASSWORD` (secret) | What every seeded account signs in with. Deliberately not the published one: the address is public and the way in is not |
| `DEMO_ENCRYPTION_KEY`, `DEMO_JWT_SECRET` (secrets) | The same values the service has, so the seed writes credentials the service can open |
| `DEMO_URL` (variable, not secret) | The public address. A secret would be redacted out of the only logs these jobs produce |

## Nothing else

No message broker, no cache, no object store. No external identity provider
either: the directory above is read for accounts, never asked to
authenticate anybody, so no sign-in leaves this system.

Nothing phones home. Portico opens exactly three kinds of outbound
connection, all of them to addresses you configured: your database, your
SMTP relay, and a directory you pointed it at. Webhooks add a fourth, to
destinations a tenant administrator registers — restricted to public HTTPS
and re-checked at connection time so they cannot be aimed inward. The
metrics endpoint is read by your monitoring and pushed nowhere.

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
| `github.com/go-ldap/ldap/v3` | Speaks LDAP to the directory above. Pure Go, so it costs the deployment nothing: no OpenLDAP client library to install and the `scratch` container still works. |
| `github.com/xuri/excelize/v2` | Reads and writes the bulk-import workbooks, and the directory export. |
| `github.com/golang-jwt/jwt/v5` | Signs and verifies access tokens. |
| `github.com/zitadel/oidc/v3` | Both halves of OpenID Connect. It issues for the applications that trust Portico, and — through its relying-party half — spends the tokens of an external provider Portico trusts. Deliberately the same library for both: a mistake in ID token validation is an authentication bypass rather than a wrong answer, and the checks that get missed by hand are the subtle ones, an `iss` never compared or an `alg` taken from the token. |
| `golang.org/x/oauth2` | The authorization-code exchange under the relying party above. Direct rather than transitive since Portico began signing people in through somebody else's provider. |
| `github.com/prometheus/client_golang` | The metrics registry and exposition handler. Adds no runtime dependency: with `PORTICO_METRICS_ADDR` unset it registers collectors nothing reads and opens no listener. |
| `sqlc` | Development-time code generation from SQL. Contributors only need it when changing a query; the generated code is committed. |
