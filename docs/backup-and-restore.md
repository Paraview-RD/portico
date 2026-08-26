# Backup and Restore

What to copy, what a copy is worth without the rest, and what happens when
you actually use one.

## State outside the database

| Where | What | Lost how |
|---|---|---|
| PostgreSQL | Everything else: accounts, organizations, tenants, settings, the audit trail, sessions, and **every private key** — the OIDC signing keys and the SAML certificates both live in tables | A dropped database, a bad migration, a deleted tenant |
| `PORTICO_JWT_SECRET` | The secret that signs Portico's own session tokens. It is not in the database and never has been | A rebuilt host, a rotated secret manager entry, a lost `.env` |
| `PORTICO_ENCRYPTION_KEY` | The key the directory bind passwords are encrypted under. Also not in the database, and deliberately: the dump holds the ciphertext and nothing that opens it | The same three ways |

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

**Nor is a dump plus the JWT secret, if you synchronize from a directory.**
`PORTICO_ENCRYPTION_KEY` is what the LDAP bind passwords are encrypted under,
and it is the one credential this system has to be able to read back rather
than merely check — Portico sends it to your directory on every run. Restore
under a different key and every directory connector fails at bind, with an
error from the directory about credentials rather than anything naming the
key. The bind password has to be typed in again for each source, and there is
no way to recover the old one from the dump.

Back up both secrets with the same care and the same schedule as the
database, and keep all three recoverable together. It is one backup in three
files.

## Private keys in the database dump

`oauth_signing_keys.private_key` and `saml_signing_keys.private_key` are
PKCS#8 PEM, stored as plain text — a deliberate decision, on the same
footing as the password hashes beside them: protecting the database is the
deployment's job.

`webhook_subscriptions.secret` is in the clear beside them, for a reason
worth stating rather than assuming: unlike a client secret or a SCIM token it
is never compared against something a caller presents. It *produces* the
signature on every delivery, so there is nothing to hash and a digest would
be useless. Somebody holding a dump can sign a delivery that every subscriber
verifies as genuine.

The consequence for backups is not deliberate unless you make it so. A dump
sitting in an object store with default permissions is your identity
provider's signing keys sitting in an object store. Someone holding it can
mint tokens your relying parties will accept and assertions your service
providers will trust, and can keep doing so until every key is rotated and
every service provider has been told.

What is *not* in the clear is the directory bind password: that one is
AES-256-GCM under `PORTICO_ENCRYPTION_KEY`, which lives in the environment
rather than the database, so a dump on its own does not yield it. That is the
one place where the split above buys something.

Each such value is also sealed against what it is and whose it is, so a
ciphertext lifted out of one row and dropped into another — a webhook's
headers into a directory's bind password, one tenant's credential into
another tenant's row — no longer decrypts. **Restoring a dump into a
different tenant therefore does not carry these credentials with it**: the
rows restore, the sealed values stop opening, and they have to be entered
again. Values written before this existed keep opening, and are sealed
against their row the next time they are saved.

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

Then start Portico against it **with the `PORTICO_JWT_SECRET` and the
`PORTICO_ENCRYPTION_KEY` that were in use when the dump was taken**. Point `PORTICO_DB_DSN` at the restored
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

## Point-in-time restore behavior

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
