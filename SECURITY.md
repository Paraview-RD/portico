# Security Policy

Portico is authentication software, so a bug here is more likely to matter
than the same bug elsewhere. Please read the known limitations before
deploying it — several are deliberate, and one of them may disqualify
Portico for your situation.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Report privately through GitHub's
[private vulnerability reporting](https://github.com/paraview/portico/security/advisories/new)
on this repository.

What to expect:

| | |
|---|---|
| First response | within 5 working days |
| Assessment and plan | within 10 working days |
| Fix and disclosure | coordinated with you; 90 days is the default ceiling |

Please include what you did, what happened, and what you expected —
a proof of concept helps enormously. If you would like credit in the
advisory, say so and how you want to be named.

We will not pursue legal action against anyone who reports in good faith,
tests only against their own instance, and does not access other people's
data.

## Supported versions

Pre-1.0: **only the latest release** receives security fixes. There are no
backports to older tags. Once 1.0 ships this section will be replaced with a
real support matrix.

## Known limitations — read before deploying

These are deliberate MVP scope decisions, not oversights. They are listed
here rather than only in the requirements document because they change how
you must deploy Portico.

### There is no rate limiting, and that has an availability consequence

Portico does not throttle sign-in attempts. This is stated in the README as a
scope decision, but the practical effect is larger than "passwords can be
guessed slowly":

Every sign-in attempt costs a bcrypt evaluation, including attempts against
usernames that do not exist (that is intentional — it stops the endpoint
from being used to discover which accounts are real). Combined with a
single-writer database, enough concurrent sign-in requests from one source
can exhaust CPU and stall writes.

**You must place Portico behind a reverse proxy that rate-limits
`/api/v1/auth/*`.** See [docs/access-guide.md](docs/access-guide.md) for a
worked nginx and Caddy configuration. The bundled `docker-compose.yml` binds
to `127.0.0.1` for this reason — do not change that to `0.0.0.0` without a
proxy in front.

### There is no TLS

Portico serves plain HTTP and does not terminate TLS. Bearer tokens,
passwords, and password-reset forms all cross that connection. Terminate TLS
at the reverse proxy; never expose Portico directly.

### Other deliberate exclusions

- **No MFA**, no CAPTCHA, no login risk analysis, no account lockout.
- **Password policy is length only** (minimum 8 characters). No complexity
  rules, no breach-list checking, no rotation, no forced change on first
  sign-in — including for accounts created by bulk import, whose initial
  passwords the importing administrator knows.
- **Custom roles and permissions.** There are two fixed roles per tenant:
  administrator and user. There is no RBAC to configure, and therefore no
  way to grant a subset of administrative capability.
- **No third-party or social sign-in**, and no SCIM.
- **Registration reveals whether a username is taken.** The sign-in endpoint
  is deliberately uniform about this; the registration endpoint is not,
  because reporting the collision is how a user knows to pick another name.
  If that matters to you, leave self-registration disabled (the default).
- **Audit logs have no retention policy.** They grow without bound and
  contain usernames and IP addresses. Plan for pruning and for whatever your
  jurisdiction requires of that data.

### Operational notes with security consequences

- **The bootstrap administrator password is printed to the startup log** when
  you do not set one. Under Docker that log usually ends up in a log
  aggregator where it persists and is broadly readable. Prefer setting
  `PORTICO_INITIAL_ADMIN_PASSWORD` explicitly, and change it after first
  sign-in either way.
- **`PORTICO_JWT_SECRET` must be at least 32 bytes**; the server refuses to
  start otherwise. Generate it with `openssl rand -hex 32`. Changing it
  signs everyone out.
- **Forwarding headers are not trusted by default.** If Portico runs behind a
  proxy and you want real client IPs in the audit log, set
  `PORTICO_TRUST_PROXY_HEADERS=true` — but only when a proxy you control is
  guaranteed to be in front, since otherwise callers can forge the address
  recorded against their own actions.
- **The session token is stored in browser `localStorage`.** This is a
  deliberate trade: bearer tokens in the `Authorization` header are why
  Portico has no CSRF exposure. The mitigation for the XSS side of that trade
  is the Content-Security-Policy the server sets on every response.

### Multi-tenancy is enforced in the query layer

Every tenant-scoped query goes through a wrapper that applies the tenant
filter, and an automated test fails the build on any query that bypasses it.
This is deliberate: a single missed filter is a cross-tenant data leak, and
that is not a class of bug that survives being left to reviewer attention.

There is **no cross-tenant administrator role**. Tenants are provisioned from
the command line by whoever operates the deployment. A role able to read
across tenants would be the single largest risk to the isolation the rest of
the design is spent on.

One consequence worth stating plainly: `audit_logs.tenant_id` is `NOT NULL`,
so a sign-in attempt naming a tenant that **does not exist** has no trail to
be written to. Those go to the process log instead. It is a security-relevant
event leaving the audit trail, and it is a deliberate trade — the alternative
is an unscoped audit table, which is a far larger hole than the one it
closes.

### Federation protocols

Portico acts as an identity provider for OAuth 2.1, OIDC, SAML 2.0, and CAS.
Two consequences worth stating:

- **Redirect URIs are matched exactly** against a registered allow-list. Open
  redirect is the classic way an authorization-code flow is turned into
  account takeover.
- **SAML signature verification uses a maintained library**, never
  hand-written XML processing. Signature-wrapping attacks against
  hand-rolled SAML have produced a long series of authentication bypasses,
  and it is not a place to be original.

## What is covered

In scope: authentication bypass, privilege escalation, session-handling
flaws, injection, information disclosure, and anything that lets one user
act as another.

Out of scope: the deliberate exclusions above; findings that require an
already-compromised administrator account; vulnerabilities in a reverse
proxy or host you operate; and automated scanner output without a
demonstrated impact.
