# Portico

This is the Portico manual.

Every Portico serves its own copy at `/docs`, built into the binary, so that
copy describes the exact version running it. If the screen in front of you
and the page in front of you disagree, that is worth reporting.

## Start here

**[Getting in](access-guide.md)** — the entry points, where credentials come
from, and what each of the two roles can actually do, written as the journeys
people take rather than as a list of screens. Read this one first.

## Bringing accounts in

Two directions, and the distinction matters more than it sounds:

- **[Reading a directory](ldap.md)** — Portico connects to your Active
  Directory or OpenLDAP and pulls accounts out. Portico starts it and holds a
  credential.
- **[Provisioning](scim.md)** — your directory pushes accounts in over SCIM
  2.0. The directory starts it and holds the credential.

If your directory can push, prefer it: nothing about a service account has to
be stored here at all.

## Letting applications in

**[Federation](federation.md)** — OAuth 2.1, OpenID Connect, SAML 2.0, and
CAS, with a table of exactly what revocation reaches for each. Worth reading
before you deploy rather than after somebody asks why a signed-out person is
still in an application.

**[Webhooks](webhooks.md)** — a signed POST when an account, organization, or
group changes, with the verification recipe a subscriber should implement.

## Running it

**[Integrations](integrations.md)** — every external thing Portico talks to,
what it authenticates with, and what it costs.

**[Backup and restore](backup-and-restore.md)** — what to back up, and the
part people miss: the encryption key lives outside the database, so a
database backup alone does not restore a working system.

The full API is described in
[OpenAPI](api/openapi.yaml){ download="portico-openapi.yaml" }.
