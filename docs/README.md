# Documentation

## Using and operating Portico

- **[access-guide.md](access-guide.md)** — entry points, where credentials
  come from, what each role can do, and how to put Portico behind a reverse
  proxy. Start here to deploy it.
- **[federation.md](federation.md)** — signing an application in through
  Portico: issuers, registering a client, what the tokens carry, and exactly
  what revocation can and cannot reach. Read this before integrating.
- **[backup-and-restore.md](backup-and-restore.md)** — what to copy, why a
  database dump on its own is not a backup of this system, and the three
  things a point-in-time restore does that nobody expects.
- **[scim.md](scim.md)** — provisioning accounts from a directory: what is
  implemented, why there are no Groups, and why DELETE deactivates rather
  than deletes.
- **[integrations.md](integrations.md)** — external services Portico depends
  on at runtime (there are none, deliberately) and what that implies.

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

## Background

- **[requirements/v0.1-requirements.md](requirements/v0.1-requirements.md)** —
  the requirements for the current version, including what is deliberately
  excluded and the V0.2–V0.4 roadmap. Written in Simplified Chinese.
- **[requirements/v0.1-baseline-mvp.md](requirements/v0.1-baseline-mvp.md)** —
  the earlier, narrower scope the first working version was built against.
  Kept because the code still reflects many of its decisions, and because it
  records why things like "disable, never delete" exist.
