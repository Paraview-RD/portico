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
| **Groups** | Create, read, replace, patch, delete, filter, and full membership management |
| PATCH | Users: `active`, `userName`, `displayName`, `externalId`, `emails`, `phoneNumbers`. Groups: `members`, `displayName`, `externalId` |
| Attributes | The core User schema's `name` parts, `nickName`, `profileUrl`, `title`, `userType`, `preferredLanguage`, `locale`, `timezone`, `photos`, `addresses`; and the enterprise extension's `employeeNumber`, `costCenter`, `department`. Carried on POST, PUT, and GET |
| Filter | Users: `userName eq "..."`, `externalId eq "..."`. Groups: `displayName eq "..."`, `externalId eq "..."` |
| Bulk | Not supported |
| Sort | Not supported |
| ETag | Not supported |
| changePassword | Not supported — passwords are not settable over SCIM |

`GET /scim/v2/ServiceProviderConfig` returns the same list, and a test
asserts the two agree: every capability advertised has a handler that works,
and nothing unadvertised quietly does.

### What the attributes do and do not reach

The descriptive attributes above are stored under the names RFC 7643 gives
them, which is why a directory's fields land where they belong rather than in
something this project invented.

Two things they deliberately do not reach:

**`manager` is not read from SCIM.** It names another account by id, and a
directory's id space is its own — accepting one would either store a foreign
identifier or require resolving it during a sync. Set it in the console;
Portico reports it on GET, so a directory can read back what an operator
chose.

**`organization` and `division` are ignored.** Portico has an organization
tree with codes that downstream systems store. A free-text copy beside it
drifts, and when the two disagree nobody can say which to believe. The
enterprise extension's `department` *is* kept, as free text, precisely
because it often names something that is not in this tenant's tree — losing
it would lose information an operator can act on.

**PUT replaces; PATCH does not.** SCIM's PUT means "the resource is now
this", so a directory that stops sending a title is saying the title is gone
and Portico clears it. If that is not what you want, use PATCH — which is
also why the PATCH row above lists a shorter set: those are the paths with a
handler, and `ServiceProviderConfig` advertises exactly them.

### Groups are not organizations

Portico has both, and they are different things:

| | Organization | Group |
|---|---|---|
| How many per person | Exactly one | Any number |
| Shape | A tree | Flat |
| Identified by | A stable code downstream systems store | Its name |
| Usually maintained by | An administrator | A directory |

SCIM Groups map onto **groups**. They deliberately do not touch
organizations: group membership is many-to-many, and forcing it onto the
single-valued organization field would either break the org chart that
downstream systems depend on or silently reassign somebody the first time a
directory added them to a second group.

So a group push does exactly what it says and changes nothing else. If you
also want the directory to decide where people sit in the org chart, that is
a different feature and this is not it.

**Membership grants nothing.** There are two fixed roles and no RBAC, so
there is nothing for a group to carry — and if that changes it will change
deliberately, not because a directory wrote an attribute.

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

**Provisioning → New credential** in the console, or:

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
| Provisioning | Users and groups — create, update, deactivate, and group membership |

### 3. Check it

```bash
curl https://<host>/scim/v2/ServiceProviderConfig \
  -H "Authorization: Bearer <scim token>"
```

## Group membership

`PATCH /Groups/{id}` is what a directory runs, and all of these are
understood, because different providers send different ones:

```jsonc
// add
{"op":"add","path":"members","value":[{"value":"<userId>"}]}

// remove — the id in the path, which is what Okta sends
{"op":"remove","path":"members[value eq \"<userId>\"]"}

// remove — the id in the value
{"op":"remove","path":"members","value":[{"value":"<userId>"}]}

// replace: this is now the whole membership, which is how Entra reconciles
{"op":"replace","path":"members","value":[{"value":"<userId>"}]}
```

`PUT /Groups/{id}` also replaces the membership, because PUT replaces the
resource and members are part of it.

**A member that does not exist fails the request**, with 400 and
`scimType: invalidValue`. It is not skipped: a silently dropped member
leaves a group that looks synchronized and is not, which is the one failure
your directory cannot detect from its own side.

`GET /Users/{id}` reports the groups an account belongs to, so a client can
read back that its push landed. That field is read-only — membership is
changed through the Group resource, so there is one way to do it and no
question of which side wins.

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
