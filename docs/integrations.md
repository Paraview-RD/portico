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

### Resend — an alternative to SMTP, for hosts that block it

Optional, and off unless asked for. SMTP remains the default and is the right
answer for anybody with a relay of their own.

It exists because a great many places a small deployment can afford will not
let a process open an SMTP port at all. Render blocks outbound 25, 465 and 587
on free instances; port 25 stays blocked on paid ones, as it is on most cloud
providers. The failure is a connection timeout, so nothing in the relay
settings is wrong and no amount of correcting them helps. HTTPS is not blocked
anywhere, which is the whole argument.

Set `PORTICO_MAIL_TRANSPORT=resend` and the same messages go out over
`https://api.resend.com/emails` instead. Nothing above the mail interface
changes.

| Setting | Meaning |
|---|---|
| `PORTICO_MAIL_TRANSPORT` | `smtp` (default) or `resend`. |
| `PORTICO_RESEND_API_KEY` | A Resend API key. A send-only key is enough and is what should be used — nothing here reads or lists anything. |
| `PORTICO_MAIL_FROM` | Sender address, on a domain verified with Resend. |

- **Purpose**: delivering password-recovery, address-confirmation and trial
  messages from a host that blocks SMTP.
- **Auth**: a bearer API key, from the Resend dashboard.
- **Account owner**: whoever runs the deployment. This project holds no Resend
  account and ships no key; the public demo's key belongs to its operator.
- **Cost**: free at 3,000 messages a month and 100 a day at the time of
  writing, which is more than a demonstration sends. Paid above that.

With no verified domain a Resend account may only send *from*
`onboarding@resend.dev` and *to* the address that owns the account. That is
enough to walk the flow end to end and not enough to run anything real, so a
deployment that anybody else uses needs a domain verified first.

Both halves fail at startup rather than at the first message: asking for this
transport without a key or a sender is a misconfiguration, not a deployment
that chose to do without email.

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

## Demo hosting

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
minutes **between 00:03 and 10:53 UTC** — 09:00 to 19:00 in +08, with an
hour's warming before it, since a first poke at 09:00 would leave the first
visitor paying exactly the cold start this is meant to prevent.

The window is not about Actions minutes, which are free. It is Render's 750
free instance hours a calendar month: kept awake around the clock a service
spends about 730 of them, which works and leaves nothing over for a second
free service or for a month with a redeploy in it. Eleven hours a day is
roughly 340.

**It does not keep the service awake, and the gap is not marginal.** GitHub's
scheduler is best-effort, and on a cron asking for ten minutes it delivers
something closer to forty-five. Measured over one ordinary day on this
repository: 12 runs against the 66 the schedule asks for, the shortest gap 27
minutes and the longest 69 — every one of them longer than the fifteen
minutes it takes Render to put the service to sleep. The morning after, the
window opened at 00:03 UTC and the first run had still not fired at 00:52.

So read this as *raising the chance* that a visitor arrives at a warm
instance during the day, not as preventing cold starts. Somebody who opens
the address and waits fifty seconds has not found a broken deployment; they
have found the free tier. The README says the same thing to a visitor, in the
sentence about the cold start, and that sentence is the honest one.

What actually removes the problem is Render's $7/month tier, which does not
sleep. An external uptime probe on a five-minute interval would also fit
inside the fifteen, at the price of one more third-party account and one more
credential to keep.

`DEMO_KEEPALIVE=off` stops the schedule; running the workflow by hand still
checks the demo.

**A free Postgres expires after 30 days, with a 14-day grace period.** Nothing
works around that. The demo and everything in it goes, and the Blueprint has
to be applied again.

### GitHub Actions — the two jobs Render's free tier cannot run

Scheduled jobs are a paid feature on Render, and Actions minutes are unmetered
on a public repository, so both live here instead.

`demo-keepalive.yml` pokes the health endpoint. That is the only scheduled
job, and it is the only one there has ever been in practice.

There was a second, `demo-reseed.yml`, which emptied the schema nightly and
seeded a fresh one. It is gone, and what replaced it is not another job: a
tenant a trial creates now carries its own deadline — a fortnight, then a
week of grace — so the thing that had to be swept up cleans itself up, per
tenant, on the clock of whoever asked for it. Nothing has to reach into the
database from outside to make that happen.

| Secret / variable | What it is |
|---|---|
| `DEMO_URL` (variable, not secret) | The public address. A secret would be redacted out of the only logs this job produces |
| `DEMO_KEEPALIVE` (variable, optional) | Set it to `off` to stop the keepalive schedule; remove it to resume. Running that workflow by hand still checks the demo, which is the one thing worth doing while it is off |

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
