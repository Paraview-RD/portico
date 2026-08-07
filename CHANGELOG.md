# Changelog

Notable changes to this project. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet.

## [0.1.0] - 2026-08-07

The initial release: the MVP scope, complete and verified end to end. Not yet
used in production anywhere — treat it as early software.

### Added

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
- `KEYLITE_JWT_SECRET` must be at least 32 bytes; the server refuses to start
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

Keylite has no TLS and no rate limiting, both deliberately. It must run
behind a reverse proxy that provides them — see
[SECURITY.md](SECURITY.md) and
[docs/access-guide.md](docs/access-guide.md).

[0.1.0]: https://github.com/paraview/keylite/releases/tag/v0.1.0
