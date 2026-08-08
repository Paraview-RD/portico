# SCIM Provisioning

Portico speaks SCIM 2.0 (RFC 7643, RFC 7644) so a directory — Okta, Entra
ID, or anything else that speaks the protocol — can create, update, and
deactivate accounts without anyone typing them in twice.

Base URL: `https://<host>/scim/v2`

## What is implemented, exactly

This is the part worth reading before configuring anything. A SCIM server
that advertises more than it does produces a push that half-works, and the
discovery endpoints here are deliberately honest so your identity provider's
own configuration screen shows the truth.

| | |
|---|---|
| **Users** | Create, read, replace, patch, deactivate, and filter |
| **Groups** | **Not implemented.** See below |
| PATCH | Supported, for `active`, `userName`, `displayName`, `externalId`, `emails`, `phoneNumbers` |
| Filter | `userName eq "..."` and `externalId eq "..."` |
| Bulk | Not supported |
| Sort | Not supported |
| ETag | Not supported |
| changePassword | Not supported — passwords are not settable over SCIM |

`GET /scim/v2/ServiceProviderConfig` returns the same list, and a test
asserts the two agree: every capability advertised has a handler that works,
and nothing unadvertised quietly does.

### Why there are no Groups

An account in Portico belongs to at most one organization. SCIM group
membership is many-to-many, and `PATCH /Groups` with `add members` is the
operation directories lean on hardest. Mapping that onto a single-valued
field would either silently reassign somebody's organization or fail partway
through a push — and silent reassignment in an identity system is the worse
of the two.

So `/ResourceTypes` offers `User` only. Configure users-only provisioning;
both Okta and Entra support it as a first-class option. Assign roles and
organizations in Portico, where they mean something.

### Why DELETE does not delete

`DELETE /Users/{id}` **deactivates** the account rather than removing the
row. Portico disables and never deletes, so the audit trail keeps naming
something that exists — an entry pointing at a vanished id answers no
questions later.

The account stays readable afterwards, with `active: false`. That matters
for your sync: a client that treats 404 as "gone" would recreate the account
on its next run.

`DELETE` and `PATCH active=false` go through the same code path, so
deprovisioning cannot work one way and not the other.

## Setting it up

### 1. Issue a credential

**Settings → SCIM credentials → New credential** in the console, or:

```bash
curl -X POST https://<host>/api/v1/scim-credentials \
  -H "Authorization: Bearer <admin token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Okta production"}'
```

The response contains the token. **It is shown once.** What is stored is a
SHA-256 digest, which is what makes a database dump not a set of working
credentials — and also means nobody, including you, can recover it later.
Lost it? Issue another and delete the old one.

A credential is scoped to `/scim/v2` and to one tenant. It cannot sign in to
the console, cannot read the API, and has no account behind it.

### 2. Point your directory at it

| Setting | Value |
|---|---|
| SCIM base URL | `https://<host>/scim/v2` |
| Authentication | HTTP Header / OAuth Bearer Token |
| Token | The one issued above |
| Provisioning | Users only — create, update, deactivate |

### 3. Check it

```bash
curl https://<host>/scim/v2/ServiceProviderConfig \
  -H "Authorization: Bearer <scim token>"
```

## What a directory may and may not change

It may set: `userName`, `displayName`, `emails`, `phoneNumbers`,
`externalId`, and `active`.

It may **not** set a role or an organization, and this is the boundary
rather than an omission. SCIM has no notion of Portico's two roles, so any
mapping would have to be invented — and an invented one is a way to become
an administrator by writing a directory attribute. A directory says who
somebody is; Portico says what they may do.

It may not set passwords. A directory pushing them would mean it holds a
value it can replay, and this deployment's own policy — length, history,
expiry — would apply to something nobody here chose.

## externalId is what keeps one account one account

Bind `externalId` on every account you provision. It is the only stable
correlation key: usernames and email addresses are exactly the attributes a
sync is likely to be changing.

Portico reconciles on it. `POST /Users` with an `externalId` that already
exists **updates that account** rather than creating a second one — including
when the `userName` has changed, which is what happens when somebody is
renamed in the directory. Without it, a provisioning run that cannot match
an existing account creates one, and that is the most common way a SCIM
integration passes testing and duplicates a directory in production.

## What deactivation does

Immediately, in one operation:

- the account is disabled, so every subsequent request is refused;
- every session it holds is revoked, so an open browser stops working;
- every federated refresh token is revoked, so applications it signed in to
  cannot refresh.

The one thing it does not do is reach inside an application that has its own
session — no identity provider can. See
[federation.md](federation.md#what-revocation-reaches-per-protocol).

A directory cannot deactivate the last active administrator. The request is
refused rather than obeyed: a directory that stops listing somebody is a
routine event, and without the guard a leaver's last day would lock everyone
out of the tenant.

## Errors

SCIM's own shape, not Portico's envelope, so your directory can report them:

```json
{
  "schemas": ["urn:ietf:params:scim:api:messages:2.0:Error"],
  "scimType": "invalidPath",
  "detail": "This server does not support patching title. Supported paths: ...",
  "status": "400"
}
```

A `PATCH` for an attribute this server does not store answers **400 with
`scimType: invalidPath`**, naming the attribute — not 501. The difference
matters where you will read it: a 501 in a sync log says the server is
broken, while this says which attribute nobody can set.

A `PATCH` is all-or-nothing. If the third operation is unsupported, the first
two are not applied either — a client that receives an error retries the
whole body, and a partly applied patch is a state neither side knows about.

## Auditing

Provisioning has its own audit verbs — `SCIM_USER_CREATE`,
`SCIM_USER_UPDATE`, `SCIM_USER_DISABLE`, `SCIM_USER_ENABLE` — separate from
the ones an administrator's actions produce. An automated deprovisioning and
somebody deciding to disable a colleague are different events to whoever
reads the trail later.

Issuing and revoking credentials is audited at the same weight as
registering an application: one of these can change every account in the
tenant without a person being present.

The credential list shows when each was last used, which is the question
asked when a directory has quietly stopped syncing.
