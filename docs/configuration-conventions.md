# Configuration Conventions

All configuration is environment variables. There is no configuration file,
no config server, and no runtime reload of process settings.

That is a deliberate limit rather than an unfinished feature: a
single-binary, single-node deployment gets no benefit from a config file
that the environment could have supplied, and every additional source is
another place the effective value might be coming from when something is
wrong.

Runtime-adjustable behaviour — session lifetime, whether registration is
open, the system name — lives in the database and is edited from the
settings screen. Those are product settings, not process configuration, and
the distinction is worth keeping: process configuration decides how the
program starts, product settings decide how it behaves.

## Naming

- **`PORTICO_` prefix on everything**, so the program's variables are
  distinguishable from the rest of the environment at a glance.
- **`SCREAMING_SNAKE_CASE`**, naming the thing rather than its type:
  `PORTICO_DB_DSN`, not `PORTICO_DATABASE_CONNECTION_STRING_VALUE`.
- **Grouped by subject**: `PORTICO_DB_DRIVER` and `PORTICO_DB_DSN` share a
  prefix because they belong together.
- **A boolean is `true` or `false`**, compared exactly. Not `1`, not `yes`,
  not case-insensitive matching — a value that is neither is a mistake, and
  the safe reading of a mistake is the default.

## Every setting is in three places

Adding one means updating all three, in the same commit:

1. **`internal/config/config.go`** — the field, its default, and a comment
   explaining what it does and what happens at the edges.
2. **`.env.example`** — the documented example, with the same explanation
   in prose.
3. **`portico --help`** — the summary an operator sees without a browser.

Missing any of these is how a setting becomes discoverable only by reading
source code.

## Defaults

**Every setting has a working default except the signing secret.** Running
the binary with an empty environment starts a working instance — that is
the first-run experience, and making it depend on getting six variables
right would waste the goodwill of anyone evaluating it.

Defaults are chosen so the *unconfigured* state is the *safe* state:

- **Registration is off.** An instance exposed before anyone finishes
  setting it up must not be accepting sign-ups.
- **Proxy headers are not trusted.** Believing `X-Forwarded-For` by default
  would let any caller forge the address recorded against their own
  actions.
- **The compose file binds to `127.0.0.1`.** A plaintext admin console
  should not become internet-reachable because someone did not change a
  line.

A default that is convenient but unsafe is not a default, it is a trap.

## Failing fast

Invalid configuration stops the process at startup with an error naming the
variable and how to fix it. It never degrades to a fallback, because a
process that started "successfully" with the wrong settings is far harder to
diagnose than one that refused.

The signing secret is the case that matters:

```
PORTICO_JWT_SECRET is 9 bytes; it must be at least 32.
Generate one with: openssl rand -hex 32
```

It would have been easy to accept the short secret, or to quietly generate
a replacement. Both hide a misconfiguration that makes every token in the
system forgeable. See [SECURITY.md](../SECURITY.md).

The one deliberate exception is an **entirely unset** secret, where a random
one is generated and a warning is logged — the difference is that unset
means "has not been configured yet", while a nine-byte value means someone
configured it wrongly and believes they are done.

## Secrets

- **No secret ever has a default.** A default password is a published
  password.
- **No secret is committed.** `.env` is ignored; `.env.example` carries
  names and explanations, never values.
- **No secret is logged, ever** — see
  [logging-conventions.md](logging-conventions.md). This includes the
  generated bootstrap password, which is written to stderr precisely so it
  does not enter the log pipeline.
- **No secret goes in a URL.** They end up in access logs, proxy logs, and
  browser history.
- **Rotating `PORTICO_JWT_SECRET` signs everyone out.** Expected, and worth
  knowing before doing it in the middle of a working day.

## Parsing

Configuration is read once, into a struct, in `internal/config`. Nothing
else in the codebase calls `os.Getenv` — otherwise the real set of settings
is whatever a grep turns up, and a typo in a variable name silently means
"default".

Durations accept either a Go duration (`30m`, `2h`) or a bare number of
seconds, because both are things people reasonably type. Anything else is an
error rather than a silent zero.

## Adding a setting

Ask first whether it should exist. Every option is a combination someone can
deploy, a branch that may be untested, and a question in a bug report. The
answer is often a better default instead.

If it should:

1. Add the field with a comment covering the default and the edges.
2. Give it a safe default, or fail fast when it is required.
3. Document it in `.env.example` and `--help`.
4. If it changes security posture, say so in [SECURITY.md](../SECURITY.md).
5. Add a test for the parsing and the failure mode — `internal/config` has
   tests for both.
