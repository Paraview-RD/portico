# Reading accounts out of a directory

Portico connects to your Active Directory or OpenLDAP on demand and pulls
accounts out of it. This is the **opposite direction** from
[scim.md](scim.md), and the distinction is worth holding onto because the two
land in the same place:

| | SCIM | LDAP |
|---|---|---|
| Who starts it | The directory | Portico |
| Where it is configured | In the directory, pointed at `/scim/v2` | Here, in **Directory integration** |
| When accounts stop arriving | Nothing at this end to look at | A failed run, with the reason |
| Credential held | The directory holds a token issued here | Portico holds a bind password |

If your directory can push SCIM, prefer it: nothing about your service
account has to be stored here at all. LDAP exists because most Active
Directory deployments cannot.

## What is synchronized

Users. Not groups, not organizations, not passwords.

- **Created** when an entry appears that this source has not seen before.
  A source may name an organization, and the accounts it creates are filed
  there. That is the only time it is applied: a later run will not move
  somebody an administrator has moved, because the directory says who
  somebody is and where they belong is decided here.
- **Updated** when the display name, email, phone, or username changes.
- **Deactivated** when an entry stops appearing.
- **Reactivated** when it appears again — a directory that is the source of
  truth has to be able to say somebody is back.

Passwords are never read or written. An account synchronized from a
directory has a random password here that nothing can authenticate with, the
same as a SCIM-provisioned one. **Signing in with an AD password is a
different feature** (federation, not synchronization) and does not exist yet.

## The attribute map, which is the part to get right

There are no defaults, and that is deliberate. Active Directory and OpenLDAP
disagree on every one of these, and a wrong guess imports a directory's worth
of accounts named after the wrong field — which looks exactly like a working
integration until somebody reads the user list.

| | Active Directory | OpenLDAP |
|---|---|---|
| User filter | `(&(objectClass=user)(objectCategory=person)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))` | `(objectClass=inetOrgPerson)` |
| Username | `sAMAccountName` | `uid` |
| Display name | `displayName` | `cn` |
| Email | `mail` | `mail` |
| Phone | `telephoneNumber` | `telephoneNumber` |
| **External id** | **`objectGUID`** | **`entryUUID`** |

The console offers both as presets. They fill the fields in and leave them
editable — check them against your own directory rather than trusting them.

The AD filter above excludes disabled accounts. Include them instead if you
would rather they arrive here and be deactivated by their `userAccountControl`
value; Portico does not read that attribute itself.

### External id is the one that matters

It is the reconciliation key. Everything else can be wrong and fixed later;
this one, wrong, means every rename in the directory creates a second account
here instead of renaming the first — silently, and with no way to merge them
afterwards.

`objectGUID` is **binary**: sixteen raw bytes that Portico renders in
Microsoft's mixed-endian GUID form, so what you see here matches what
`Get-ADUser` and every other tool will show you. `entryUUID` is already text
and is stored as it arrives.

Do not point it at `distinguishedName` or `userPrincipalName`. Both change
when somebody moves department or marries.

## What a synchronization will not do

**It will not act on an empty result.** A search matching nothing looks
exactly like a directory everybody has left. The first is a typo in a base DN
or a filter and happens regularly; the second essentially never happens, and
nobody would want it applied automatically overnight. So a source that owns
accounts and gets nothing back **fails the run and changes nothing**.

**It will not touch an account it does not own.** Ownership is recorded, not
inferred from the username. An administrator who happens to share a name with
somebody in the directory is skipped — not renamed, not demoted, and not
deactivated when that directory entry later disappears. The skipped count on
the run is where those show up.

**It will not delete.** Accounts are deactivated, exactly as SCIM's `DELETE`
does, so the audit trail keeps pointing at something that still exists.

**Disabling the connector does not deactivate its accounts.** The connector
and the people are different things. Use it to stop synchronizing — during a
directory migration, say — without locking anybody out.

## The bind password

It is the one credential in this system that has to be recoverable rather
than merely checkable: Portico sends it to your directory on every run.

It is encrypted with AES-256-GCM under `PORTICO_ENCRYPTION_KEY`, which lives
in the environment rather than the database. The honest limit of that: anyone
who can read the process environment can read the credential. What it defends
against is the leak that actually happens — a backup, a replica, a snapshot
handed to somebody for debugging.

**Without that variable set, Portico refuses to store one** rather than
writing it in the clear. Generate one with `openssl rand -hex 32`, and do not
reuse `PORTICO_JWT_SECRET` — the server refuses that too, because one value
doing both jobs means either leak costs both.

Use a read-only service account. Portico never writes to your directory, and
a bind account with write access is a standing risk for no benefit.

An empty bind DN means an anonymous bind, which some read-only directories
allow.

## Running it

Synchronization is manual in this version: **Directory integration** →
**Synchronize**. The run is synchronous and reports what it did — created,
updated, deactivated, skipped — and the history is kept per run, because the
question asked when something has gone wrong is not "what is the state now"
but "when did this start".

There is no scheduler yet, and the obvious workaround is worse than it
looks. A cron job can call `POST /api/v1/directories/{id}/sync`, but an
access token lasts `PORTICO_TOKEN_TTL` (two hours by default) and is revoked
by a password change or a sign-out-everywhere — so the job has to sign in on
each run, which means an administrator's password sitting in the cron
environment. That is a worse credential to leave lying about than the bind
password this page spends a section protecting.

Run it by hand until the scheduler exists, or accept that trade knowingly.

## Troubleshooting

**"No Such Object"** — the base DN does not exist. Note that this is the
directory's own wording, left untranslated on purpose: it is the string worth
searching for.

**The run failed saying the directory returned no entries** — the base DN
exists and the filter matched nobody. Check the filter first; this refusal is
what stopped everybody being deactivated.

**Accounts arrive with the wrong name** — the display-name attribute is
pointed at the wrong field. Fix it and synchronize again; existing accounts
are updated in place.

**A rename created a second account** — the external id attribute is pointing
at something that changes. Stop synchronizing before it happens again.

**Skipped count is not zero** — the run says why, grouped by reason with an
example of each: `5 × That is not a valid phone number. (mei, arjun, …);
1 × That username is already in use. (admin)`. Read that before assuming.

The common one is a username collision with an account Portico owns rather
than a fault in the directory. The others are entries with no username or no
external id, and values the account rules refuse — a phone number formatted
for humans, with spaces, is the one that catches people.
