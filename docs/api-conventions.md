# API Conventions

These are Keylite's own conventions for its HTTP API. They're written from
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
| DELETE | Remove (or soft-disable, where the resource is never physically deleted — see below) |

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

- Success is always HTTP 200 for JSON responses (204 is fine for a
  genuinely empty body, e.g. some DELETEs).
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

## Deletion semantics

Per the MVP requirements ([docs/requirements/mvp-requirements.md](requirements/mvp-requirements.md)),
users are never physically deleted — `DELETE`/disable endpoints flip a
status flag. Document this explicitly per resource in its handler; don't
assume DELETE means "gone" project-wide.
