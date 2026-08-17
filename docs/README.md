# Documentation

## Using and operating Portico

- **[capabilities.md](capabilities.md)** — what Portico does, on one page:
  which protocols, which ways in, where accounts come from, and — the part
  worth reading twice — what it deliberately does not do. Start here if you
  are deciding whether this fits.
- **[access-guide.md](access-guide.md)** — entry points, where credentials
  come from, what each role can do, and how to put Portico behind a reverse
  proxy. Start here to deploy it.
- **[federation.md](federation.md)** — signing an application in through
  Portico: issuers, registering a client, what the tokens carry, and exactly
  what revocation can and cannot reach. Read this before integrating.
- **[backup-and-restore.md](backup-and-restore.md)** — what to copy, why a
  database dump on its own is not a backup of this system, and the three
  things a point-in-time restore does that nobody expects.
- **[ldap.md](ldap.md)** — reading accounts *out of* an AD or OpenLDAP, which
  is the opposite direction from SCIM: what is synchronized, why the external
  id attribute is the one to get right, and what a run will refuse to do.
- **[scim.md](scim.md)** — provisioning accounts from a directory: what is
  implemented, how groups differ from organizations, and why DELETE
  deactivates rather than deletes.
- **[webhooks.md](webhooks.md)** — being told when something changes here:
  the signature scheme, which destinations are refused and why, and what
  "delivered" does and does not mean.
- **[organizations.md](organizations.md)** — organizations and groups side by
  side: one says where somebody sits and the other which sets they belong to,
  they have incompatible shapes, and neither grants anything. Read it to
  decide which of the two a given fact belongs in.
- **[field-mappings.md](field-mappings.md)** — renaming, suppressing and
  adding the attributes a recipient receives, for all four kinds of recipient
  at once.
- **[settings.md](settings.md)** — the per-tenant settings and the audit
  trail: what each one changes, and which of them affect people who are
  already signed in.
- **[deployment.md](deployment.md)** — running it in production: which probe
  points at which endpoint, whether you may run more than one instance, what
  an upgrade does, and the Kubernetes manifests in `deploy/k8s/`.
- **[public-demo.md](public-demo.md)** — putting a demonstration where
  strangers can use it: a domain, mail that reaches other people's inboxes,
  self-service trials, and the four steps whose failure mode is a deployment
  that looks healthy.
- **[integrations.md](integrations.md)** — external services Portico depends
  on at runtime (there are none, deliberately) and what that implies.
- **[api/openapi.yaml](api/openapi.yaml)** — the management and self-service
  API, machine-readable. Every operation under `/api/v1`, checked against the
  running router by a test, so a client generated from it calls endpoints
  that exist. Protocol endpoints are not restated here; they have their own
  specifications.

## Contributing

Conventions this project holds itself to. They describe what the code
actually does, so a reviewer can check a change against them:

- **[code-conventions.md](code-conventions.md)** — package layout, which
  direction dependencies run, and how tests are organized. Start here.
- **[configuration-conventions.md](configuration-conventions.md)** —
  environment variables, defaults, failing fast, and handling secrets.
- **[api-conventions.md](api-conventions.md)** — URL shape, the
  `{code, message, data}` envelope, and which status code means what.
- **[database-conventions.md](database-conventions.md)** — schema naming,
  types, migrations, and how to write a query safely.
- **[error-conventions.md](error-conventions.md)** — error codes, choosing a
  status, wrapping errors in Go, and what must never reach a client.
- **[logging-conventions.md](logging-conventions.md)** — structured log
  format, levels, correlation, and what must never be logged.
- **[i18n-conventions.md](i18n-conventions.md)** — translation keys,
  placeholders, and what does and does not get translated.
- **[design-principles.md](design-principles.md)** — design tokens, colour
  roles, and the rules the frontend styles itself by.
- **[dev-stack.md](dev-stack.md)** — the containers a development machine
  runs beside Portico — a directory to synchronize from, a mail relay to
  send to — and how to point a local instance at them.

## Background

- **[requirements/v0.1-requirements.md](requirements/v0.1-requirements.md)** —
  the requirements for the current version, including what is deliberately
  excluded and the V0.2–V0.4 roadmap. Written in Simplified Chinese.
- **[requirements/v0.1-baseline-mvp.md](requirements/v0.1-baseline-mvp.md)** —
  the earlier, narrower scope the first working version was built against.
  Kept because the code still reflects many of its decisions, and because it
  records why things like "disable, never delete" exist.
