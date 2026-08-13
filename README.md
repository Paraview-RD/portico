# Portico

*English · [简体中文](README.zh.md)*

A self-hostable identity platform: standard single sign-on, multi-tenant
isolation, and a complete self-service flow — a single Go binary with the
web UI compiled in, backed by PostgreSQL.

> **v0.1.0 is tagged; this branch is 0.2 in progress.** What is described
> below is what exists in the tree, not what is planned — the difference
> between the tag and here is at the top of
> [CHANGELOG.md](CHANGELOG.md), under Unreleased, and its foot lists what is
> deliberately absent from both. There are no published binaries yet; build
> from source with either recipe under [Running it](#running-it).

## Try it

PostgreSQL and the server, with nothing else to install:

```bash
export POSTGRES_PASSWORD=$(openssl rand -hex 16)
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

Then <http://localhost:8410>, and sign in as `admin` with `Portico@1` — the
documented default, which the first sign-in refuses until you replace it. The
manual is at `/docs` inside the same binary, so it describes the version you
just started rather than the newest one.

[Running it](#running-it) has the from-source recipe, what each variable
does, and what a backup has to include. Everything below is what the thing
does and why it does it that way; the manual — [in this repository](docs/),
and at `/docs` in anything you start — is the reference.

## What it does

**Standard single sign-on** — OAuth 2.1, OpenID Connect 1.0, SAML 2.0, and
CAS, so existing business systems integrate without a bespoke protocol.

**Multi-tenant** — every table carries a tenant, and isolation is enforced
in the query layer rather than left to reviewer discipline. Each tenant has
its own administrators; tenants are provisioned from the command line, so no
role exists that can see across all of them.

**Accounts, organizations, and groups** — create, edit, enable/disable in
bulk, import from a spreadsheet and export back to one. An account carries
the attributes a directory actually has for it — job title, department,
employee number, name parts, locale, address — named after SCIM 2.0's
schema, so what your directory holds lands in the right field instead of
being dropped. What it holds that has no field here, a tenant defines for
itself — **System → User attributes**, as text, a number, a yes/no, a date,
or one of a list it writes out — and from then on the attribute is on every
account form and in the field catalogue, addressable exactly like a built-in
one. Describing somebody and deciding their access are separate
endpoints, which is what lets people maintain their own details without that
being a way to change a role. Accounts are disabled, never deleted, so the
audit trail stays intact. An organization is where somebody sits: one of
them, arranged as a tree. A group is a set they belong to: any number of
them, flat, and usually maintained by whatever directory pushes it. They are
separate concepts because they have incompatible shapes, and group
membership grants nothing. A person may also be *attached* to any number of
further organizations — the platform engineer who also sits on a project —
which is advisory: it grants nothing, synchronizes nowhere, and leaves the
one authoritative membership alone. Each organization may name whoever is
responsible for it, which likewise grants nothing: this version has two fixed
roles, and a field that quietly became a third would be the worst way to
acquire one. [docs/organizations.md](docs/organizations.md) puts the two
shapes side by side, for deciding which of them a given fact belongs in.

**Reading accounts out of a directory** — connect to an Active Directory or
OpenLDAP and pull users in, reconciled on the directory's own stable
identifier so a rename stays a rename. Accounts that stop appearing are
deactivated; ones that reappear come back. A run that gets an empty result
refuses to act on it, because a wrong base DN looks identical to a directory
everybody has left. [docs/ldap.md](docs/ldap.md) has the attribute maps for
both and the list of what a synchronization will not do.

**Provisioning from a directory** — SCIM 2.0 for users and groups, with the
PATCH shapes Okta and Entra actually send rather than only the ones the
specification lists first. Accounts are reconciled on `externalId`, so a
rename in the directory is a rename here and not a second account.
[docs/scim.md](docs/scim.md) says plainly what is *not* provisioned, which is
the part an integrator needs first.

**Webhooks** — a signed POST when an account, organization, or group
changes, with retries and a delivery history you can read when a subscriber
says they received nothing. Destinations are restricted to public HTTPS and
re-checked at connection time, so a tenant administrator cannot use Portico
as a proxy into the network it runs in.
[docs/webhooks.md](docs/webhooks.md) has the event list, the signature a
receiver verifies, and how long a failing one is retried for.

**Fields under the names the other side reads** — a service provider matches
on the name it is given, so a system looking for `dept` throws away the
`department` it was sent, and the field looks absent from both ends. Any
field in the catalogue can be renamed or added on the way out, per
application and separately per webhook subscription. Which of the two a rule
does is not a choice: the ten claims OpenID Connect already sends get
renamed, and everything else — the twenty-five SCIM profile attributes, the
organization, whatever the tenant defined — is added by naming it. A rule
reaches that one application or that one subscriber and nothing else.
[docs/field-mappings.md](docs/field-mappings.md) has the catalogue, the ten,
and what a mapping cannot do.

**Metrics** — Prometheus, on a separate listener that only exists if you
configure one, with no tenant or request-path labels.

**Sign in three ways** — username, phone, or email, all producing the same
credential.

**Self-service** — registration (optional, off by default, and able to
require a confirmed email address before the account works), password change,
password recovery by email, and profile maintenance, with no administrator in
the loop. Password rules are per tenant: a minimum length that no policy can
lower, plus optional composition rules, reuse checks, and expiry — the last
three off by default, and documented as the compliance features they are
rather than the security ones they are not. Recovery needs an SMTP relay;
point `PORTICO_SMTP_HOST` at whatever you already run. SMS recovery is
defined as a provider interface and ships without one.

**A home screen, for everybody** — signing in lands on the applications you
can open, your account at a glance, and your last few sign-ins, rather than
on an administrative screen most people cannot use. The applications on it
are the tenant's and not the reader's, and the screen says so plainly: this
version has two fixed roles and no notion of who may use what, so the list is
identical for everybody. An application appears there once it has a launch
address — none of the addresses a protocol already stores is one, since a
redirect URI and an assertion consumer service are places a browser is sent
mid-flow — and carries whatever icon was registered for it, or a tile bearing
the first letter of its name if none was.

**Leaving** — anyone can close their own account, confirming with their
password. It is the one place self-disabling is allowed; everywhere else it
is refused so an administrator cannot lock themselves out by accident.
Closing deactivates rather than deletes, so an administrator can reinstate it
and the audit trail keeps pointing at an account that exists.

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
and time range. [docs/settings.md](docs/settings.md) covers what it records,
and is honest about what it will not tell you.

**Screens that explain themselves** — every administrative screen opens with
a few sentences on what it is for and what a change there reaches, each
linking into the manual for the rest. A tenant whose operators have read them
four hundred times turns them off in its own settings; they are on by
default, because a screen nobody can interpret costs more than an
explanation somebody no longer needs.

**Bilingual UI** — English and 简体中文, switchable at runtime.

Deliberately **not** in this version: custom roles and permissions (there are
two fixed roles), third-party and social login, MFA, and any rate limiting
beyond the sign-in endpoints. The roadmap for those is in
[docs/requirements/v0.1-requirements.md](docs/requirements/v0.1-requirements.md).

> **Before exposing this to a network:** Portico serves plain HTTP,
> deliberately. `/api/v1/auth/*` is throttled per client address, but that
> is a floor rather than a defence — it counts per address and per process,
> so it does nothing about an attacker with many of either. Accounts also
> lock after repeated failed sign-ins, which is a third control again: it
> stops one account's password being guessed. It must run behind a reverse
> proxy that terminates TLS and throttles `/api/v1/auth/*` — see
> [SECURITY.md](SECURITY.md) for why and
> [docs/access-guide.md](docs/access-guide.md) for working nginx and Caddy
> configurations.

## Trying it without installing it

[**Open a Codespace**](https://codespaces.new/Paraview-RD/portico) — GitHub
builds the console, the manual and the server, seeds a database with people,
organizations, applications and history, and opens the console in a browser
tab. Nothing to install and nothing to configure.

Sign in as `admin` (super administrator) or `liyan` (ordinary user); every
seeded account shares the password `Portico@1`. `zhangwei` is a second
administrator, and the one most of the seeded history is attributed to, so
sign in as that one to see an account with a past. The same names exist
in a second tenant, `acme`, with almost nothing carried across, which is the
shortest way to see what multi-tenant means here. Mail goes to a Mailpit
inbox on a second forwarded port rather than anywhere real, so a password
reset link is something you read rather than wait for.

Two things worth knowing before you click:

- **It is yours, not a shared demo.** The Codespace runs in your own GitHub
  account and its forwarded ports are private to you — there is no address
  that can be sent to somebody else. Anyone who wants to look opens their
  own from the same button, and it costs them their own free quota rather
  than yours. A free personal account gets 120 core-hours and 15 GB-months
  a month, paid plans more; a 2-core machine spends two of those core-hours
  per hour it is awake.
- **Stop it when you are done.** It suspends itself after 30 minutes idle,
  but a forgotten one still holds storage. `gh codespace delete` or the list
  at [github.com/codespaces](https://github.com/codespaces).

To bill an organization instead of the people opening them, the organization
owner enables Codespaces for the repository and sets spending on it; the
button is then the same button and the meter points elsewhere.

## Running it

### Binary

Needs a PostgreSQL instance to point at. The frontend has to be built first —
it is compiled into the binary, so a Go-only build produces a working API
with no UI (the server says so rather than serving a blank page).

```bash
cd web && npm ci && npm run build && cd ..
go build -o portico ./cmd/server

PORTICO_DB_DSN=postgres://portico:portico@localhost:5443/portico?sslmode=disable \
PORTICO_JWT_SECRET=$(openssl rand -hex 32) ./portico
```

Requires Go 1.26+ and Node 22+ — what `go.mod` declares and what CI and the
release build run, so the floor is one answer rather than three. Both are
floors, not pins: the from-source Docker image below builds the console on a
newer Node than this asks for.

### Docker

Brings up PostgreSQL alongside the server; nothing else to install.

```bash
export POSTGRES_PASSWORD=$(openssl rand -hex 16)
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

Configuration is entirely environment variables — see
[.env.example](.env.example) for the full list. Every one has a working
default except `PORTICO_DB_DSN`, which has none: unset, the server says so
and exits. Set `PORTICO_JWT_SECRET` explicitly too — it does not stop a
start, but without it a random secret is generated per start and every
session dies on restart.

Keep both of those wherever you keep secrets, because neither is in the
database and a restore needs them: `PORTICO_JWT_SECRET` signs the sessions and
`PORTICO_ENCRYPTION_KEY` opens the directory bind passwords a dump only holds
the ciphertext of. A `pg_dump` on its own is therefore not a backup of this
system — [docs/backup-and-restore.md](docs/backup-and-restore.md) says what to
copy and what each omission costs when you use the copy.

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

Each gets its own administrator. Without `--admin-password` it takes the
documented default and cannot sign in until that is replaced; pass one and
it signs in normally. Its users sign in with the tenant code, typed into the
**Tenant** field or carried by a link: `/login?tenant=acme`.

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

All three also take `--launch-url` and `--logo-uri`, which is what puts an
application on the home screen with a recognizable tile. Both are optional
and neither affects signing in — an application without a launch address
still works, it simply is not offered as something to open.

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
cmd/seed/          fills a development database with ninety days of use
internal/
  config/          environment variables, read and validated once
  server/          routing, and the version the build reports
  handler/         HTTP handlers
  service/         business rules
  store/           database access; sqlcgen/ is generated
  model/           domain types
  auth/            passwords, JWTs, token verification
  httpx/           the response envelope, the error type it carries, and
                   request logging and the security headers
  secrets/         AES-GCM, for the one credential that has to be readable
                   back rather than merely checkable
  i18n/            the message catalogues, English and Chinese
  casp/            the CAS protocol, implemented directly
  oidcp/           adapts Portico to the OpenID Provider interface
  oidcrp/          the other direction: signing in through somebody else's
  samlp/           adapts Portico to the SAML identity provider role
  scim/            the SCIM 2.0 endpoints a directory provisions through
  directory/       the other direction: reading accounts out of LDAP
  webhook/         outbound delivery, signing, and retries
  notify/          email and the SMS interface that ships without a provider
  metrics/         Prometheus, on its own listener when one is configured
  provision/       tenant and client provisioning, for the CLI
  seed/            a development database that looks used, for cmd/seed
  testdb/          throwaway PostgreSQL for tests
  web/             embeds the built frontend
  docs/            embeds this documentation, served from the binary
migrations/        schema, embedded and applied at startup
web/               React + Vite frontend
docs/              the manual, plus the conventions and the requirements
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

All of it is published as a manual at
[paraview-rd.github.io/portico](https://paraview-rd.github.io/portico/) —
the same pages rendered, searchable, and in both languages. A running
Portico serves its own copy at `/docs`, built into that binary; when a
version is in front of you, that is the copy that describes it.

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
through [Security → Report a vulnerability](https://github.com/Paraview-RD/portico/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
