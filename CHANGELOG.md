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
- Settings are per tenant: system name, session lifetime, and the
  registration toggle are each a tenant's own.
- Tokens carry the tenant, and it is checked against the account record on
  every request, so a token cannot act outside the tenant it was issued in.
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

### Added

- **Password recovery** by email, with a single-use link that expires in 30
  minutes and invalidates any earlier one. The request endpoint answers
  identically whether or not an account matched, and resolves against the
  channel's own column rather than the sign-in lookup — resolving across
  columns and then sending a token is how one account's reset ends up
  delivered on another account's identifier.
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
- Flat organizations with codes, sort order, and member counts. Disabling
  one blocks new members without detaching existing ones.
- Sign-in issuing self-signed JWTs, with immediate revocation on logout,
  password change, and account disable.
- Audit log covering sign-ins, operations, authorization, registrations, and
  organization changes, filterable by type, actor or target, and time range.
- Runtime settings: system name, session lifetime, and the registration
  toggle.
- Two endpoints for downstream systems: the caller's profile with
  organization, and an administrator check.
- Web UI in English and 简体中文, with navigation driven by the caller's
  role.
- Single-binary distribution: the built frontend is embedded, and the
  container image is `scratch` plus one file.

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
