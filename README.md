# Keylite

A lightweight, self-hostable Identity & Access Management system. Single
Go binary, embedded web UI, no external IdP/SSO protocol stack required —
built for the "just need accounts, login, and basic roles" case that
full-scale IAM/IGA platforms over-serve.

> Status: early scaffold. Structure and licensing are settled; backend and
> frontend implementation have not started yet.

## Scope (MVP)

- Local account lifecycle: manual creation, Excel batch import, self
  service registration (togglable)
- Unified username/password login, session/token expiry, logout
- Two fixed roles: super admin / normal user — no custom RBAC
- Single-tier organizations, one org per user
- Self-issued JWT for auth (no OAuth2/OIDC/SAML/CAS)
- Downstream pull-based user/org sync for integrating business systems
- Login / operation / auth / registration / org-change audit logs

Full detail: [docs/requirements/mvp-requirements.md](docs/requirements/mvp-requirements.md).

Explicitly out of scope for the MVP: standard federation protocols,
multi-app isolation, third-party/LDAP login, fine-grained RBAC, MFA/rate
limiting, SDKs/webhooks. See the requirements doc for the full exclusion
list and the post-MVP roadmap.

## Layout

```
cmd/server/        entry point
internal/          handler / service / repository / middleware / model
migrations/        DB schema migrations
web/               frontend (embedded into the server binary at build time)
docs/              requirements, architecture, conventions
deploy/            Dockerfile, compose
```

## Tech stack

- Backend: Go
- Frontend: TBD (React + Tailwind, reusing this project's own design
  token architecture — not Paraview's internal brand values)
- Distribution: single binary via `go:embed` of the built frontend

## License

[Apache License 2.0](./LICENSE). See [NOTICE](./NOTICE) for attribution.
Contributions require a DCO sign-off — see [CONTRIBUTING.md](./CONTRIBUTING.md).
