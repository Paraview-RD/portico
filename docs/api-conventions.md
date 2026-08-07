# API Conventions

These are Portico's own conventions for its HTTP API. They're written from
general REST API design practice — consistency, predictable error
semantics, and being easy for an external client to integrate against —
not derived from or tied to any particular company's internal standard.

## URL design

- Resource-oriented, plural nouns: `/api/v1/users`, `/api/v1/organizations`.
- Multi-word path segments use kebab-case: `/api/v1/audit-logs`.
- Nesting reflects ownership, capped at one level:
  `/api/v1/organizations/{id}/members`.
- Actions that don't map to a CRUD verb are a POST sub-resource, not a
  query param: `POST /api/v1/users/{id}/disable`, not
  `PATCH /api/v1/users/{id}?action=disable`.
- All routes are versioned under `/api/v{n}/`. Breaking changes bump the
  version; the previous version keeps working until it's formally
  deprecated.

## HTTP methods

Full REST verb semantics — this project has no gateway constraint that
would justify limiting to GET/POST, and external integrators expect
standard verb behavior:

| Verb | Use |
|---|---|
| GET | Read, list, filter/sort/paginate via query params |
| POST | Create, or an action with side effects that isn't idempotent |
| PUT | Full replace of a resource |
| PATCH | Partial update |

`DELETE` is intentionally absent. Nothing in this system is destroyed —
users and organizations are disabled instead, so the audit trail stays
complete. Disabling is a state change with its own endpoint
(`POST /{id}/disable`), not a deletion, and naming it accurately keeps
clients from assuming the row is gone.

Query params are for filtering/sorting/pagination on GET only. Everything
else — including all POST/PUT/PATCH bodies — is JSON in the request body,
`Content-Type: application/json; charset=utf-8`.

## Request format

- Body field names: `camelCase`.
- Top-level body is always a JSON object, never a bare array or scalar.
- IDs, large integers, and monetary values are transmitted as strings to
  avoid precision loss in JS clients; everything else uses its natural
  JSON type — no implicit stringification of booleans/numbers.
- Timestamps are ISO 8601 with an explicit timezone offset.
- Auth: `Authorization: Bearer <jwt>`.

## Tenant selection

Every request acts inside exactly one tenant, and where that tenant comes
from depends on whether the caller is authenticated.

**Authenticated requests take the tenant from the token**, and from nothing
else. `X-Portico-Tenant` on an authenticated request is ignored — not
rejected, ignored — because honouring it would let one tenant's
administrator reach every other tenant by adding a header. The token's
tenant claim is checked against the account record on every request, so a
token cannot be replayed against a tenant it was not issued for either.

**Public endpoints** — `POST /auth/login`, `POST /auth/register`,
`GET /auth/registration-status` — have no principal to read, so they resolve
the tenant from the request, in this order:

1. the `tenant` field in the request body (`login` and `register` only),
2. the `X-Portico-Tenant` header,
3. the `tenant` query parameter,
4. the default tenant, whose code is `default`.

The last step is what lets a single-tenant deployment never mention tenants
at all while the same build serves a multi-tenant one.

An unknown or disabled tenant answers `TENANT_NOT_FOUND` (404) or
`TENANT_DISABLED` (403) rather than a generic credential failure. A tenant
code is not a credential — it appears in sign-in URLs and in the
configuration handed to every user of that tenant — so concealing whether
one exists buys nothing and costs an operator a diagnosable error.

There is no API for creating, listing, or disabling tenants. No account can
act outside its own tenant, so there is no one the API could authorize to do
it; provisioning is `portico tenant ...` on the command line.

## Sign-in identifiers

`POST /auth/login` takes one `identifier` field, which may be a username, an
email address, or a phone number. All three are unique within a tenant and
all three produce the same session; the caller does not say which kind they
sent, because it is a way of naming an account rather than a kind of
sign-in.

Resolution has a declared precedence — username, then email, then phone —
because a username may look like an email address and "which column matched"
needs a fixed answer.

**Password recovery does not use that resolution.** It matches the channel's
own column, and the message goes to what the account has bound rather than
to what the request contained. Resolving across columns and then sending a
token is how one account's reset ends up delivered on another account's
identifier.

## Response envelope

```json
{
  "code": "SUCCESS",
  "message": "",
  "data": { }
}
```

- `code`: a string constant (`SUCCESS`, or an error identifier — see
  below). Never a bare HTTP status number.
- `message`: human-readable, safe to show to an end user. Empty string on
  success unless there's something worth surfacing (e.g. a warning).
- `data`: present in every response. `null` when there's nothing to
  return — never an omitted field, and never `{}`/`[]` used to mean "no
  data" (an empty list is a valid, real value; `null` means "no result").

## Status codes and error semantics

- Success is always HTTP 200 for JSON responses. A 204 is acceptable only
  for a genuinely empty non-JSON body; no endpoint currently returns one.
- The HTTP status and the body's `code` must agree on failure vs success —
  never HTTP 200 with a body that signals an error. A client should be
  able to branch on the transport-layer status alone and get the right
  answer.
- Client/business errors are always 4xx. 5xx is reserved for actual bugs
  or infrastructure failures on our side — a duplicate username, an
  expired token, or a disabled account is never a 500.
- Distinguish the three 4xx cases that get conflated most often:
  - **400** — the request itself is malformed (missing required field,
    wrong type, invalid JSON).
  - **409** — the request is well-formed but conflicts with current
    state (e.g. registering a username that already exists).
  - **422** — the request is well-formed and doesn't conflict with
    state, but a business rule rejects it (e.g. registration is
    currently disabled).
- **401** vs **403**: 401 means "we don't know who you are" (missing/
  invalid/expired token); 403 means "we know who you are, and you're not
  allowed to do this."

## Error identifiers

`code` values for errors are `SCREAMING_SNAKE_CASE`, human-readable
without needing a lookup table: `USER_NOT_FOUND`, `INVALID_CREDENTIALS`,
`ACCOUNT_DISABLED`, `TOKEN_EXPIRED`, `REGISTRATION_DISABLED`,
`ORGANIZATION_NOT_FOUND`. No enforced module/sequence-number scheme — at
this project's size, a flat, descriptive namespace is easier for
contributors to extend correctly than a registry of numeric codes.

## Disable, never delete

Per the MVP requirements ([docs/requirements/v0.1-requirements.md](requirements/v0.1-requirements.md)),
records are never physically removed. `POST /{resource}/{id}/disable` flips
a status flag; the row, and every audit entry referencing it, stays.

Disabling a user also revokes their live sessions. Disabling an
organization blocks new members but keeps existing ones — the two are not
symmetric, so each is documented on its own endpoint rather than assumed.
