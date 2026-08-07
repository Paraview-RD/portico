# Error Conventions

How errors are produced, propagated, and reported. The wire format — which
HTTP status pairs with what — is in
[api-conventions.md](api-conventions.md); this covers everything behind it.

## Error codes

Every failure carries a stable, machine-readable `code` alongside its
human-readable `message`. The code is what clients branch on; the message is
what a person reads.

- **`SCREAMING_SNAKE_CASE`, descriptive, no numbering scheme**:
  `USER_NOT_FOUND`, `INVALID_CREDENTIALS`, `TOKEN_REVOKED`,
  `ORGANIZATION_DISABLED`.

  A registry of numbered codes (`AUTH_LOGIN_0007`) is the right answer at a
  scale where many teams allocate codes independently and collisions are the
  risk being managed. This is one codebase; a flat descriptive namespace is
  easier for a contributor to extend correctly, and a reader can tell what
  `ACCOUNT_DISABLED` means without a lookup table.

- **A code names the reason, not the location.** `USERNAME_TAKEN`, not
  `USER_SERVICE_ERROR_3`. If two places produce the same reason, they share
  the code.

- **A code must never encode business data.** `USER_ADMIN_LOCKED` is wrong —
  the role belongs in the message or a field, not baked into an identifier
  that clients match on.

- **Never reuse `code` as the user-facing message.** Showing someone
  `REGISTRATION_DISABLED` in the interface is a failure of the interface.
  The code is for machines; the frontend maps it, or falls back to `message`.

### Once published, a code is frozen

Its meaning must not change and it must not be repurposed. Clients branch on
these; silently changing what one means breaks integrations in a way that is
very hard to diagnose from the other side. Add a new code and leave the old
one in place, marked deprecated in the changelog.

## Choosing the status code

The distinction that gets collapsed most often is between "malformed",
"conflicts", and "refused":

| Situation | Status | Example |
|---|---|---|
| The request itself is wrong | **400** | missing field, wrong type, unparseable JSON |
| Caller is unidentified | **401** | missing, invalid, expired, or revoked token |
| Caller is known but not permitted | **403** | a normal user calling an admin endpoint |
| The thing does not exist | **404** | no such user |
| Well-formed, but clashes with current state | **409** | username already taken |
| Well-formed, no clash, but a rule refuses it | **422** | registration is currently closed |

**A business rule refusing something is never a 5xx.** 5xx means a bug or an
infrastructure failure — something on our side to fix. Returning 500 for
"password too short" trains operators to ignore 5xx alerts.

**401 versus 403** is worth getting right: 401 is "we do not know who you
are", 403 is "we know, and no". Answering 403 to an unauthenticated caller
tells them the resource exists; answering 401 to an authenticated one sends
them to re-authenticate pointlessly.

## Errors in Go

- **Typed at the point the rule lives.** A failure that has a client-facing
  meaning is constructed as an `*httpx.Error` where the rule is enforced —
  in the service layer — not translated at the handler. That way the status
  and code are decided once, next to the reason.
- **Wrap with `%w`.** Chains must survive so `errors.Is` and `errors.As`
  work. Two places in this codebase used a type assertion instead and would
  have downgraded a wrapped typed error to a generic 500.
- **Never swallow an error.** No empty `catch`-equivalent, no `_ = err` on
  anything that matters. If an error is genuinely ignorable, the assignment
  gets a comment saying why.
- **Wrap once per layer.** A message reading
  `get user: query user: scan user: sql: no rows` is four layers each adding
  nothing.
- **Expected outcomes are return values, not errors.** "This username is
  taken" is a normal result of trying to register; it is modelled as a typed
  error only because that is how it reaches the HTTP boundary, not because
  it is exceptional.
- **One place renders errors to clients.** `httpx.Fail` is it. Handlers
  return errors; they do not write status codes themselves.

## What clients must never receive

An unrecognised error becomes a generic 500 with a fixed message, and the
real cause is logged instead. This is enforced in `httpx.Fail` rather than
left to each handler, because the failure mode is silent: a handler that
returns a raw database error leaks the schema, file paths, and sometimes
connection details, and nothing about the response looks wrong.

Specifically, an error reaching a client must not contain:

- Database errors, SQL fragments, table or column names
- File paths, host names, internal addresses
- Stack traces
- Anything from the "never logged" list in
  [logging-conventions.md](logging-conventions.md) — the bar for a response
  body is higher than for a log, not lower

One real instance: the settings endpoint used to reflect the raw storage
error into a 400. It both leaked detail and misfiled an infrastructure
failure as a client mistake, so it never triggered 5xx alerting.

## Messages

`message` is shown to end users, so write it for them:

- **Plain sentences, ending in a period.** "That username is already in
  use." — not "username_taken" or "ERR: duplicate key".
- **Say what to do** when there is something to do. "Session lifetime must
  be between 5 and 43200 minutes." beats "invalid value".
- **Do not leak whether an account exists.** Sign-in answers the same way
  for an unknown username and a wrong password, on purpose. Any new
  authentication path has to preserve that.
- **Messages are English in the API.** The interface localizes them by
  `code`; see [i18n-conventions.md](i18n-conventions.md).
