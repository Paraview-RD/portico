# Documentation

## Using and operating Keylite

- **[access-guide.md](access-guide.md)** — entry points, where credentials
  come from, what each role can do, and how to put Keylite behind a reverse
  proxy. Start here to deploy it.
- **[integrations.md](integrations.md)** — external services Keylite depends
  on at runtime (there are none, deliberately) and what that implies.

## Contributing

Conventions this project holds itself to. They describe what the code
actually does, so a reviewer can check a change against them:

- **[api-conventions.md](api-conventions.md)** — URL shape, the
  `{code, message, data}` envelope, and which status code means what.
- **[database-conventions.md](database-conventions.md)** — schema naming,
  types, migrations, and how to write a query safely.
- **[design-principles.md](design-principles.md)** — design tokens, colour
  roles, and the rules the frontend styles itself by.

## Background

- **[requirements/mvp-requirements.md](requirements/mvp-requirements.md)** —
  the original requirements the MVP was built against, including what was
  deliberately excluded and the intended direction afterwards. Written in
  Simplified Chinese.
