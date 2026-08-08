# Backup and Restore

What to copy, what a copy is worth without the rest, and what happens when
you actually use one.

## The state is in two places, and one of them is not the database

| Where | What | Lost how |
|---|---|---|
| PostgreSQL | Everything else: accounts, organizations, tenants, settings, the audit trail, sessions, and **every private key** — the OIDC signing keys and the SAML certificates both live in tables | A dropped database, a bad migration, a deleted tenant |
| `PORTICO_JWT_SECRET` | The secret that signs Portico's own session tokens. It is not in the database and never has been | A rebuilt host, a rotated secret manager entry, a lost `.env` |

**A `pg_dump`-only backup is not a backup of this system.** Restore it into
an instance with a different `PORTICO_JWT_SECRET` and everything comes back
except that every signed-in person is signed out, immediately, including the
administrator you were going to use to check the restore worked. The
database is intact and the deployment looks broken.

This is not a prediction. Restoring a dump and starting it under a fresh
secret leaves the data correct — passwords still work, `/ready` answers 200
— while every previously issued token answers 401. Nothing in the logs says
"wrong secret"; it reads as an instance that signed everyone out for no
reason.

Back up the secret with the same care and the same schedule as the database,
and keep them recoverable together. They are one backup in two files.

## The database dump contains private keys

`oauth_signing_keys.private_key` and `saml_signing_keys.private_key` are
PKCS#8 PEM, stored as plain text — a deliberate decision, on the same
footing as the password hashes beside them: protecting the database is the
deployment's job.

The consequence for backups is not deliberate unless you make it so. A dump
sitting in an object store with default permissions is your identity
provider's signing keys sitting in an object store. Someone holding it can
mint tokens your relying parties will accept and assertions your service
providers will trust, and can keep doing so until every key is rotated and
every service provider has been told.

Encrypt dumps at rest, restrict who can read them, and treat a leaked backup
as a key compromise rather than a data breach — the response is different
and larger.

## Taking a backup

```bash
pg_dump --format=custom --file=portico-$(date +%F).dump "$PORTICO_DB_DSN"
```

`--format=custom` rather than plain SQL: it compresses, and it restores with
`pg_restore`, which can be told to continue past an object that already
exists rather than failing halfway with the database half-written.

**Match the client to the server.** `pg_dump` refuses to dump from a server
newer than itself, and — worse — an older client against a newer server can
produce a dump that restores with pieces missing rather than an error. Check
both:

```bash
pg_dump --version
psql "$PORTICO_DB_DSN" -c 'SHOW server_version'
```

If they differ by a major version, run `pg_dump` from a container of the
server's version rather than from whatever the host has installed.

The secret is one line, and belongs wherever your other secrets do:

```bash
# Whatever this is for you: a secret manager, an encrypted file, a vault.
echo "$PORTICO_JWT_SECRET" | your-secret-store put portico/jwt-secret
```

## Restoring

Restore into an empty database, with the instance stopped:

```bash
createdb portico_restored
pg_restore --dbname=portico_restored --no-owner portico-2026-08-08.dump
```

Then start Portico against it **with the `PORTICO_JWT_SECRET` that was in
use when the dump was taken**. Point `PORTICO_DB_DSN` at the restored
database and let it start: migrations are embedded and run at startup, so a
dump from an older version is brought forward automatically. There is no
separate migration step to remember, and no tool to install.

Check the restore with something that reads the database rather than
something that reads the process:

```bash
curl -sf http://127.0.0.1:8410/api/v1/ready     # 200 means the database answered
```

`/health` is not the check. It deliberately does not touch the database, so
it answers 200 against a restored instance whose database is unreachable —
see [access-guide.md](access-guide.md).

## Three things a point-in-time restore does that nobody expects

These follow from what the data means, and none of them announce themselves.

**Revocations are undone.** Signing out, changing a password, and disabling
an account all work by writing to the database — `token_version` on the
account, a revocation on the session row, a revocation on the refresh-token
chain. Restoring to a point before a revocation restores the state in which
that credential was still good. A token somebody revoked because it leaked
starts working again, for as long as it has left to run.

If you are restoring *because* of a compromise, rotate
`PORTICO_JWT_SECRET` as part of the restore. That invalidates every session
token at once regardless of what the restored rows say, at the cost of
signing everyone out — which during an incident is what you wanted anyway.
Refresh tokens are not covered by that rotation; disable the affected
clients instead.

**SAML service providers may stop trusting you.** A relying party refetches
the OIDC key set on its own, so restoring an older signing key is invisible
to it. A SAML service provider does not: the certificate is pinned in its
configuration, and it learns of a new one only when a human tells it. If a
certificate was rotated after the dump was taken, the restore puts back the
key the service provider has now been configured *away* from, and every
sign-in to that application fails signature validation with no message that
points at the cause. Check `saml_signing_keys` against what your service
providers hold before assuming a restore is complete.

**The audit trail is now shorter than the story.** Entries written after the
dump are gone, and the trail does not record its own truncation. If the
period being restored over matters — and during an incident it usually is
exactly the period that matters — export the audit entries from the damaged
database *before* replacing it, even if it is otherwise unusable.

## Practising

A backup nobody has restored is a file, not a backup. The failure modes
above are all silent, so the only way to know is to do it:

1. Restore last night's dump into a scratch database.
2. Start an instance against it, with the archived secret, on another port.
3. Sign in as an ordinary account — not just as the administrator, whose
   password you may have set by environment variable and which therefore
   proves less than it appears to.
4. Check one federated application still works end to end, if you run any.

That fourth step is the one that catches the SAML certificate problem, and
it is the one that gets skipped.

## Upgrades

Startup runs `goose.Up` and nothing else, so migrations only ever go
forward. A down migration does exist, and it is worth knowing what it is
before reaching for it: it drops every table. It is there to reset a
development database, not to step a deployment back a version — running it
is indistinguishable from deleting the data.

So there is no downgrade path, and that is a property of schema changes
rather than an omission: one that has already dropped a column cannot invent
its contents back. **Take a dump before upgrading.** Rolling back a version
means restoring that dump, not running the old binary against the new
schema.

The container image carries no state — `PORTICO_ADDR` and a database is the
whole of it — so an upgrade is a new image and a restart. There is no volume
to migrate and nothing to preserve on the host.
