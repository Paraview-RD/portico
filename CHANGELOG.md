# Changelog

Notable changes to this project. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Working toward 0.1.0 — the first release. Nothing has been published yet.

### Changed

- **Sign-in accepts a username, an email address, or a phone number.** One
  `identifier` field, one credential check, one token — the identifier is a
  way of naming an account, not a kind of sign-in. Resolution has a declared
  precedence (username, then email, then phone) because a username may look
  like an address.
- Phone and email are unique within a tenant, through partial indexes so
  that "not bound" stays the empty string and any number of accounts may
  leave either blank.
- A collision now reports which field collided rather than always saying the
  username was taken. The constraints are named in the migration and the
  service matches on the name.
- **Everything is tenant-scoped.** Users, organizations, audit entries, and
  settings all belong to exactly one tenant, and nothing crosses the
  boundary. Usernames and organization codes are unique per tenant rather
  than globally, so two tenants may each have an `admin`.
- Sign-in takes an optional tenant code. Omitting it resolves to the default
  tenant, which is what a single-tenant deployment always does.
- Settings are per tenant — every one of them, listed under Added below.
- Tokens carry the tenant, and it is checked against the account record on
  every request, so a token cannot act outside the tenant it was issued in.
- **Signing out, changing a password, and disabling an account now revoke
  the federated sessions too**, not only Portico's own. Bumping
  `token_version` never reached a relying party's refresh token, which is a
  separate credential in a separate table and would have stayed valid for
  its full month.
- Audit writes no longer inherit the request's cancellation. A client that
  closed the tab used to take the entry with it, which was worst for exactly
  the events worth recording.
- The `filters` builder used by list endpoints now binds the tenant as `$1`;
  callers write `WHERE tenant_id = $1` into their own SQL so the constraint
  is visible and testable.
- Database errors are classified by SQLSTATE rather than message text. The
  previous check matched SQLite's wording and had been returning false for
  every PostgreSQL error since the migration, turning duplicate-key
  conflicts into 500s instead of 409s.
- **Storage is PostgreSQL.** An earlier iteration used SQLite, which suited a
  single-tenant intranet tool but is the wrong shape for a multi-tenant
  identity provider other systems depend on. A deployment is now two
  processes rather than one; the binary still needs no cgo and still ships
  in a `scratch` image.
- Timestamps use `TIMESTAMPTZ` and scan directly into `time.Time`. The
  conversion layer SQLite required is gone.
- `PORTICO_DB_DSN` is required and has no default.
- **SAML service providers and CAS services are addressed by an opaque id**
  rather than by entity ID or URL prefix. Those are natural keys containing
  slashes and colons, and a reverse proxy configured the ordinary way
  normalizes the path before it arrives — measured against two real nginx
  containers, not reasoned about: the documented `proxy_pass` form works and
  a trailing slash turns every one of those routes into a 404.
- **Updating settings takes each field as optional**, leaving anything
  omitted unchanged. The endpoint replaces the whole object, so a client
  written against an older shape would omit the newer fields, Go would
  decode them as zero, and a request that meant to rename the system would
  silently switch account lockout off.
- **The hourly sweep now covers spent password resets, dead refresh-token
  chains, and ended sessions**, each after thirty days rather than at
  expiry. A refresh token is deleted only when its entire rotation chain is
  dead: expiry is checked *after* reuse when one is presented, so deleting
  expired rows individually would quietly disable reuse detection, which is
  the only thing that catches a stolen refresh token.
- **Audit entries are pruned only where a tenant configured a retention
  period.** The default is to keep everything.
- **A client that disconnects mid-request is reported as `499`, not `500`.**
  Every operation still in flight fails with `context.Canceled` when someone
  navigates away, and calling that a server error filled the log with
  entries nobody could act on and inflated the 5xx rate an operator alerts
  on. Both conditions are required — the error is cancelled *and* the
  request context is done — because an internal cancellation while the
  caller is still waiting is a real fault. A missed deadline stays a 500
  even if the client has also gone.
- `ActionDownstreamSync` is gone. It named an event the server cannot
  observe: a downstream system reads a profile once with the user's token,
  and whether it then creates an account, updates one, or discards the
  response is known only to it. Recording a read under the name of a sync
  would put an assertion in the audit trail that the server never witnessed.

### Added

- **SAML 2.0.** Portico is a SAML identity provider: metadata,
  service-provider-initiated browser SSO over both bindings, and signed
  assertions — encrypted too, whenever the service provider publishes an
  encryption key. Registration takes the service provider's metadata
  document whole rather than fields to retype.
- SAML certificates are per tenant and live in their own table, apart from
  the OIDC signing keys, because their rotation contracts are incompatible:
  a relying party refetches a key set, while a service provider has the
  certificate typed into its own configuration and no way to learn of a new
  one. Retired SAML certificates are kept indefinitely and rotation is an
  operator's decision, never a timer's.
- Nothing in Portico constructs or verifies an XML signature. goxmldsig is
  pinned ahead of what crewjam/saml resolves, because that is the code the
  whole thing rests on.
- Deliberately absent: identity-provider-initiated sign-on, which has no
  request to correlate an assertion with; and single logout, which requires
  reaching every service provider in the browser and reports having ended
  sessions it did not when it half works. The metadata says so rather than
  advertising an endpoint that 404s.
- **CAS 2.0 and 3.0.** Login, logout, and both validation endpoints, per
  tenant and at the root. Implemented directly rather than through a
  library, because CAS has no cryptography at all — a ticket is a random
  string and validation is a lookup.
- CAS service matching is a URL prefix with a boundary: a registration for
  `https://app.example.com/` can never cover
  `https://app.example.com.somewhere-else.test`. Wildcards, query strings,
  fragments, and plain http over a network are refused at registration.
- CAS tickets are single use, enforced by a conditional update rather than a
  read followed by a write, and bound to the service they were issued for.
- No CAS ticket-granting ticket: single sign-on rides on Portico's own
  session, so signing out, changing a password, and disabling an account
  already end it rather than leaving a third credential to revoke.
- `portico sp` and `portico cas` register SAML service providers and CAS
  services from the command line, alongside the console screens, for
  scripted and out-of-band setup.
- **OpenID Connect 1.0 and OAuth 2.1.** Portico is an OpenID Provider:
  discovery, authorize, token, userinfo, introspection, revocation,
  end-session, and a published key set. An application points its own OIDC
  library at the issuer and needs nothing Portico-specific.
  [docs/federation.md](docs/federation.md) is the integrator's guide.
- Each tenant is its own issuer at `/t/<code>`, with its own signing key and
  its own accounts. A token minted for one tenant is unusable against
  another because a relying party checks `iss` and fetches the key set that
  issuer names — both things every library already does, unlike a custom
  tenant claim nothing would check. The default tenant is additionally
  served at the root, so a single-tenant deployment never has to explain
  tenants to an integrator.
- Only the authorization code grant, and PKCE (`S256`) is required of every
  client including confidential ones. The implicit and hybrid flows put
  tokens in URLs, which is why OAuth 2.1 removes them.
- Refresh tokens rotate on every use. Presenting a spent one means a copy
  leaked, so the whole chain is revoked rather than the one call failing —
  which link leaked is unknowable. A refresh also re-checks that the account
  is still enabled.
- Tokens carry `tenant_id`, `tenant_code`, `role`, and the organization, in
  the ID token, the access token, and userinfo alike.
- Signing keys are per tenant, generated on first use, and rotated with
  `portico client rotate-key`. A retired key stays published for 24 hours so
  the tokens it signed keep verifying.
- OIDC clients are registered either from the console or from the command
  line — `portico client register|list|enable|disable|rotate-key`. Tenants
  remain CLI-only, because no role within a tenant could be authorized to
  create another one; an application belongs to a tenant, so a tenant's own
  administrator can manage it. Redirect URIs are matched exactly, and
  wildcards, fragments, and non-loopback `http://` are refused at
  registration wherever it happens.
- `OAUTH_AUTHORIZE` audit entries record who authorized which application,
  and when.
- Abandoned authorization requests are swept hourly.

- **Password recovery** by email, with a single-use link that expires in 30
  minutes and invalidates any earlier one. The request endpoint answers
  identically whether or not an account matched — in body and in timing,
  since everything past the account lookup happens after the response — and
  resolves against the channel's own column rather than the sign-in lookup.
  Resolving across columns and then sending a token is how one account's
  reset ends up delivered on another account's identifier.
- Delivery goes through plain SMTP (`PORTICO_SMTP_*`), so a deployment
  points at whatever relay it already has and no vendor is involved. SMS
  recovery is defined as an interface with no provider in this version;
  the endpoint reports the channel unavailable rather than accepting a
  request it cannot fulfil.
- **Self-service profile editing.** A user maintains their own display name,
  email, and phone. Role, status, organization, and username are absent from
  that endpoint on purpose. Changing a recovery destination is recorded in
  the audit trail with the old and new values, because repointing it is how
  a stolen session becomes permanent access.
- Multi-tenant isolation, enforced in the query layer. Every table carries a
  `tenant_id`; the service layer reaches the database only through a
  tenant-bound view that supplies it; and three tests hold the boundary up —
  one that fails the build on a query missing its tenant predicate, one that
  does the same for hand-written SQL, and a suite that drives the API as two
  tenants and asserts neither can reach the other's data.
- `portico tenant create | list | enable | disable` for provisioning. There
  is no API for it: no account can act outside its own tenant, so there is
  nobody the API could authorize. Each tenant gets its own administrator at
  creation.
- Local account lifecycle: create, edit, enable/disable, reset password,
  paged search by username or display name. Accounts are disabled rather
  than deleted so the audit trail stays complete.
- Bulk import from `.xlsx`, with a generated template and per-row error
  reporting. Rows are independent, so a partial import succeeds.
- Self-service registration, gated by a runtime toggle that defaults to off.
- Two fixed roles, administrator and user, with server-side enforcement on
  every administrative endpoint.
- Organizations with codes, sort order, and member counts, arranged as a
  tree. Disabling one blocks new members without detaching existing ones.
- Sign-in issuing self-signed JWTs, revoked immediately on signing out,
  changing a password, or disabling an account — see the sessions entry
  above for which of those ends one session and which ends all of them.
- Audit log covering sign-ins, operations, authorization, registrations, and
  organization changes, filterable by type, actor or target, and time range.
- Runtime settings, per tenant: system name, session lifetime, the
  registration toggle, the lockout threshold and window, the password
  policy, and the audit retention period.
- Two endpoints for downstream systems: the caller's profile with
  organization, and an administrator check.
- Web UI in English and 简体中文, with navigation driven by the caller's
  role.
- Single-binary distribution: the built frontend is embedded, and the
  container image is `scratch` plus one file.
- **Application management in the console.** Registering an OIDC client, a
  SAML service provider, or a CAS service, editing it, and enabling or
  disabling it are all screens now, each with the endpoints an integrator
  has to be given. `portico client`, `portico sp`, and `portico cas` still
  work; they are no longer the only way, which is what made every protocol
  above unusable without shell access to the server.
- **Account lockout.** An account locks after a configurable number of
  consecutive failed sign-ins, per tenant, and can be switched off. The lock
  is checked *after* the password comparison, so a wrong guess never learns
  that an account is locked, and the lock does not extend on further
  attempts — otherwise anyone could keep any account locked indefinitely.
- **Password policy: composition, history, and expiry.** All three are off
  by default, because NIST SP 800-63B recommends against the first and the
  third — they are here for deployments audited against regimes that require
  them. Both the settings screen and
  `internal/service/password_policy.go` say so.
- **Sessions are individually visible and individually revocable.** Each
  sign-in is its own row with its device, address, and last activity, and
  can be ended on its own from the profile page, along with a "sign out
  everywhere" for when one of them looks unfamiliar. An administrator can
  see and end a user's sessions from the user list, which is what the other
  end of a "my account is compromised" phone call needs. Signing out ends the
  session doing it and leaves the others; changing a password or disabling
  an account still ends every one. Federated sessions are not part of that
  distinction and always all go — "sign out" on a single sign-on system is
  read as signing out of the things you signed in to, and the surprising
  failure is the one where it did less than you thought.
- **Organizations form a tree**, with a parent that can be changed. Cycles
  are refused in the service layer — a foreign key cannot catch one, because
  every row in a cycle points at something that exists. The list can be
  searched, and flattens while filtering.
- **A readiness probe** at `/api/v1/ready`, which reaches the database, next
  to `/api/v1/health`, which deliberately does not: a database outage should
  not make an orchestrator restart every instance. The image is `scratch`
  and has no shell or curl, so `portico ready` exists as a subcommand for
  container health checks.
- **The audit trail shows what it records.** Each entry expands to the
  detail the server has been writing all along, along with the target type,
  the target id, and the actor's id.
- **Error messages in the reader's language.** Every error code the server
  can return has an English and a 简体中文 rendering; the Chinese table is
  typed against the English one, so a missing translation fails the build
  rather than showing a code to a user.
- **Prometheus metrics**, on a listener of their own and off unless
  `PORTICO_METRICS_ADDR` is set. HTTP counts and latencies, sign-in outcomes,
  lockouts, tokens issued, and the database pool — which is the one that
  names the failure looking like nothing else, where everything is slow, no
  request errors, and no single slow query to find. Separate listener
  because the endpoint is unauthenticated, as every Prometheus endpoint is;
  a route on the application port would be exactly as reachable as the login
  page, and the scrape config would still look right.
- No metric is labelled with a tenant or a request path. Both are values
  created from outside, and a label whose cardinality other people control
  is how a metrics endpoint becomes the largest thing a process produces.
  Routes are labelled with the chi pattern, so `/users/{id}` is one series.
- Sign-in counters are initialised to zero at startup, so a quiet instance
  reports zero rather than nothing and an alert can tell the two apart.
- **A browser test suite** in `web/e2e/`, running a real browser against the
  built binary in CI. Every test in it fails if the browser reported a
  Content-Security-Policy violation or an uncaught error, whether or not
  that test was looking — a blocked script does not fail an assertion, it
  leaves a page that renders and does nothing. That is the shape of the bug
  which broke every SAML sign-in while eleven Go tests passed.
- **A test runner for the frontend**, with the first tests written against
  the two defects a browser had already found — a `role="tab"` with no
  panel, and a status toggle addressing the wrong identifier, which the type
  checker could not see because both were strings.

### Security

- Sign-in answers an unknown username and a wrong password identically, and
  spends the same time on both, so the endpoint cannot be used to enumerate
  accounts.
- Registration hardcodes the user role and rejects unknown request fields, so
  a sign-up cannot grant itself administrator.
- Token verification rejects any algorithm other than the one used to sign,
  including `none`.
- The last active administrator cannot be disabled or demoted, and no account
  can disable itself.
- `PORTICO_JWT_SECRET` must be at least 32 bytes; the server refuses to start
  with a shorter one, because HS256 with a low-entropy key can be
  brute-forced offline from a single captured token.
- Security headers on every response: Content-Security-Policy,
  X-Frame-Options, X-Content-Type-Options, Referrer-Policy, and `no-store` on
  API responses.
- Forwarding headers (`X-Forwarded-For`, `X-Real-Ip`) are ignored unless
  explicitly trusted, so a caller cannot forge the IP recorded against their
  own actions in the audit log.
- Release artifacts carry an SPDX SBOM, and the checksum file is signed
  keylessly through Sigstore.

### Known limitations

Portico has no TLS and no rate limiting, both deliberately. It must run
behind a reverse proxy that provides them — see
[SECURITY.md](SECURITY.md) and
[docs/access-guide.md](docs/access-guide.md).

An access token already issued cannot be withdrawn: a resource server
verifies it offline and never calls back, which is the whole reason to
federate. They last fifteen minutes, and the introspection endpoint answers
for anyone who needs to know sooner. There is no consent screen, because
every client is vetted and registered by an administrator rather than
registering itself, and there is no third party to consent to.

Neither SAML nor CAS has single logout, so ending a session in Portico does
not end a session an application created for itself after accepting an
assertion or a ticket. No identity provider can do that without a working
single-logout profile. [docs/federation.md](docs/federation.md) has the full
table of what revocation reaches, per protocol.
