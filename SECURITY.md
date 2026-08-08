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

### There is no request rate limiting, and that has an availability consequence

Portico locks an account after repeated failed sign-ins — five within
fifteen minutes by default, configurable per tenant in **Settings**, and
switchable off. That is a control against *guessing one account's password*.
It is not a rate limit, and it does not address the availability problem
below.

Every sign-in attempt costs a bcrypt evaluation, including attempts against
usernames that do not exist and attempts against accounts that are already
locked — both intentional, because skipping the work would let the response
time say which accounts are real and which are locked. Combined with a
single-writer database, enough concurrent sign-in requests from one source
can exhaust CPU and stall writes. Account lockout does nothing about this:
the attempts still arrive and are still evaluated.

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

- **No MFA**, no CAPTCHA, and no login risk analysis. Account lockout does
  exist; see the availability section above for what it is and is not.
- **No breach-list checking.** Length is the floor nothing can go below;
  complexity rules, password history, and expiry are all available and all
  **off by default**, because NIST SP 800-63B recommends against the first
  and the third. Turning them on is a compliance decision, not a security
  improvement — the reasoning is in `internal/service/password_policy.go`.
- **No forced change on first sign-in as its own control.** The nearest
  thing is expiry: with a maximum age configured, a password that has never
  been changed counts as due, so an account created with a password the
  importing administrator chose must be changed at the next sign-in. With
  expiry off — the default — that administrator's knowledge of the initial
  password persists until the account holder changes it.
- **Custom roles and permissions.** There are two fixed roles per tenant:
  administrator and user. There is no RBAC to configure, and therefore no
  way to grant a subset of administrative capability.
- **No third-party or social sign-in**, and no SCIM.
- **Registration reveals whether a username is taken.** The sign-in endpoint
  is deliberately uniform about this; the registration endpoint is not,
  because reporting the collision is how a user knows to pick another name.
  If that matters to you, leave self-registration disabled (the default).
- **Audit logs are kept forever unless you say otherwise.** They contain
  usernames and IP addresses. A retention period is configurable per tenant
  in **Settings** and the hourly sweep enforces it, but the default is to
  keep everything: the trail is the record of what happened, and a product
  that quietly began deleting it on a timer would be doing the worst thing
  an audit log can do. Set it to whatever your jurisdiction requires.

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

### Password recovery

The reset token is 32 bytes from `crypto/rand`, stored as a SHA-256 digest
and never in the clear — a leaked backup or replica would otherwise hand
over working credentials for every outstanding request. It is single-use,
expires in 30 minutes, and a new request invalidates any earlier one.

The request endpoint answers identically whether or not an account matched,
so it cannot be used to ask whether someone has an account here. The three
misses — no such account, an account with nothing bound on that channel, and
a successful send — are indistinguishable.

The account is resolved against the channel's own column, not the
username-inclusive lookup sign-in uses, and the message goes to the address
the account has bound rather than the one in the request. Both matter: if one
account's email address equals another's username, resolving across columns
would deliver a reset for the second account to whoever typed that address.

None of that work happens on the request path. A hit writes two rows and
dials an SMTP server while a miss does neither, so doing it before replying
would leak through response time — seconds, not microseconds — whatever the
body said. Everything past the account lookup is detached, and a test
measures the gap.

Three things this deliberately does not do. It does not rate-limit requests
— that is the reverse proxy's job, along with the rest of `/api/v1/auth/*`;
account lockout is a different control and covers a different attack. It does not
verify a changed email address, so a user may point their own recovery at an
inbox they do not read; that locks them out rather than letting anyone in,
and the per-tenant unique index stops them taking an address another account
holds. And it does not report a delivery failure to the caller, since "we
could not send it" is only reachable once an account has been found, which
would put the oracle back.

### Registration does disclose what recovery conceals

`POST /auth/register` returns `EMAIL_TAKEN` or `USERNAME_TAKEN`, which tells
an anonymous caller that an identifier is in use. That is a real asymmetry
with the endpoint above and it is deliberate: a sign-up form that refuses
without saying why is unusable, and every system with self-service
registration makes the same trade.

Two things bound it. Registration is off by default, so a deployment that
has not enabled it discloses nothing. And enabling it is a decision to accept
public sign-ups, which is the same decision as accepting that the form will
say when a name is taken.

Closing it properly means not creating the account until the address is
verified, which is a V0.2 item. Until then, a deployment that cannot afford
the disclosure should leave registration off and create accounts
administratively.

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
