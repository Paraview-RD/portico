# Logging Conventions

How this project writes logs. Like the other convention documents here,
these describe what the code does, so a reviewer can check a change against
them.

Logging matters more than usual in an authentication system: the logs are
how an operator reconstructs an incident, and they are also the most common
place a credential accidentally ends up.

## Format

- **Structured JSON, one record per line, to stdout.** Nothing writes log
  files. Collection is the job of whatever runs the process — a container
  runtime, systemd, a supervisor — and an application that writes its own
  files fights that.
- **Field names are `snake_case`**: `request_id`, `duration_ms`,
  `client_ip`.

  This deliberately differs from the API, whose JSON bodies are `camelCase`.
  They have different consumers: API fields are read by the browser client,
  log fields are read by aggregators, `jq`, and grep, where `snake_case` is
  the convention. Keeping them distinct also means a field name tells you
  which world you are in.

- **`service_name` is on every record**, attached once when the logger is
  built rather than at each call site, so it cannot be forgotten. Obvious
  with a single service; essential as soon as these logs are aggregated
  alongside anything else.
- **Messages are short, lowercase, and constant** — `"request"`,
  `"panic recovered"`. Variable data goes in fields, never interpolated into
  the message. A message that varies cannot be grouped or counted.
- **Messages are in English**, regardless of the UI language. Logs are read
  by operators and by tooling, not by end users; the i18n system exists for
  the interface, not for diagnostics.

## Levels

| Level | Use |
|---|---|
| `ERROR` | Something failed that needs a human, or a bug was hit. A 5xx response, a panic, a write that could not be completed. |
| `WARN` | Something is wrong but the system handled it — a rejected request, a misconfiguration the process recovered from. |
| `INFO` | Lifecycle events and normal request completion. Startup, shutdown, bootstrap. |
| `DEBUG` | Detail for diagnosing a specific problem. Off by default. |

The rules that actually get broken:

- **`ERROR` is not for client mistakes.** A wrong password, a 404, a
  validation failure — none of those need anyone woken up. They are `WARN`
  at most. If `ERROR` fires during normal use, it stops meaning anything.
- **`INFO` is not for per-item logging inside a loop.** Bulk import logs one
  summary record, not one per row. A 5000-row import that emits 5000 records
  buries everything else.
- **A failure that is handled is not `ERROR`.** Audit-log writes are the
  example in this codebase: if one fails, the operation it was recording
  still succeeds, so it is logged and moved past — but it is a genuine
  problem worth investigating, so it stays `ERROR` rather than being
  swallowed. Judgement, not a mechanical rule.

## Access logs

Every request produces exactly one record, after it completes, carrying
`method`, `path`, `status`, `duration_ms`, `bytes`, `client_ip`, and
`request_id`.

**The level follows the status code**: 5xx is `ERROR`, 4xx is `WARN`,
everything else is `INFO`. This is not cosmetic — logging a 500 at `INFO`
makes the one line an operator needs to find indistinguishable from routine
traffic, which is exactly the situation where they are under time pressure.

## Correlating records

Every request is assigned a `request_id` — honouring a client-supplied
`X-Request-Id` if present — which is returned in the response header and
attached to every record for that request. When a user reports a failure,
that id is what turns "something broke" into a specific set of log lines.

## What must never be logged

This list is a red line, not a guideline. None of it may appear in a
message, a field, or an error string:

- **Passwords**, in any form, including ones typed into the wrong field.
- **Tokens** — access tokens, session identifiers, anything bearer.
- **The JWT signing secret**, or any key material.
- **Password hashes.** They are not plaintext, but they are offline-crackable
  and there is no reason for them to leave the database.
- **Full request or response bodies** on authentication endpoints.

Two consequences that are easy to miss:

- **The bootstrap administrator password is written to stderr as plain
  text, deliberately outside the structured logger.** Under any normal
  deployment log records are shipped to an aggregator, where a credential
  would persist indefinitely, be searchable, and be readable by a far wider
  group than the people entitled to administer the system. Keeping it out of
  that pipeline is the whole point; do not "tidy it up" into a log call.
- **Errors returned to clients are separate from errors logged.** The
  logged form may carry the underlying cause; the client form must not.
  `httpx.Fail` enforces this — the cause is logged and the client receives a
  generic message.

## Errors

When an error is logged, log it once, at the boundary where it is handled —
not at every layer it passes through. A single failure that produces four
records at four levels of the stack makes the trace harder to read, not
easier.

Wrap errors with `%w` so the chain survives, and let the HTTP boundary
decide what the client sees. See
[api-conventions.md](api-conventions.md) for the client-facing half.
