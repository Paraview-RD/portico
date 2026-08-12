# Field Mappings

Every application Portico signs somebody into receives a set of facts about
them, under names Portico chose. Most integrations are fine with that. The
ones that are not have exactly two problems, and this is where both are
solved: **the name is wrong**, or **the fact is not sent at all**.

A service provider maps on the name it is given. If it reads `dept` and
Portico sends `department`, the field arrives and is discarded. Before this
existed the only way in was a code change, applied to every application at
once.

## What can be mapped

`GET /api/v1/fields` returns the catalogue — the list of everything that may
be named. It has two halves:

| | |
|---|---|
| **Built in** | Identity, the twenty-five SCIM profile attributes, the organization, the tenant |
| **Yours** | Whatever attributes this tenant defined for itself, under **Settings → User attributes** |

Both halves are addressed the same way. A mapping stores a **key** from this
list, never a column name — see [the note below](#why-a-list-and-not-a-column-name).

**Nothing is ever sent empty.** A field with no value for an account is absent
rather than present and blank, so a service provider that never receives a
field it mapped should look at the account rather than at the mapping.

## What a rule does

Three things, and a rule does exactly one of them:

| | |
|---|---|
| **Rename** | A fact that is already sent, under a different name |
| **Suppress** | A fact that is already sent, no longer sent at all |
| **Add** | A fact that was never sent, sent under a name you choose |

Suppression is a flag rather than an empty name, because "send nothing" and
"send under a name I have not decided yet" are different intentions.

Adding is the larger half. The twenty-five profile attributes are stored,
arrive over SCIM, and by default reach no application at all.

## Where they are configured

Per recipient, and there are four kinds:

```
PUT /api/v1/applications/oauth-clients/{clientID}/field-mappings
PUT /api/v1/applications/saml-service-providers/{id}/field-mappings
PUT /api/v1/applications/cas-services/{id}/field-mappings
PUT /api/v1/webhooks/{id}/field-mappings
```

`GET` on the same path reads them back. A save **replaces the whole set** —
what the form sends is the table somebody edited, and merging would leave the
rows they deleted in place. Sending an empty list restores the defaults.

```bash
curl -X PUT https://<host>/api/v1/applications/oauth-clients/wiki/field-mappings \
  -H "Authorization: Bearer <admin token>" \
  -H 'Content-Type: application/json' \
  -d '{"mappings":[
        {"sourceKey":"department","targetName":"dept"},
        {"sourceKey":"organization_path","targetName":"org_path"},
        {"sourceKey":"phone","suppressed":true}
      ]}'
```

### An empty set means the defaults

Not "sends nothing" — the defaults, exactly as documented for each protocol.
This is deliberate and it is what makes the feature safe to deploy: an upgrade
changes nothing until somebody decides something, and a row in the table means
somebody decided something. There are no pre-filled rules to tell apart from
deliberate ones.

## What the name means, per recipient

**OpenID Connect** — the target is a claim name.

!!! warning "Rules reach the ID token and the access token, not userinfo"

    They are applied where the client is known, and it is known in two of the
    four places: when a token is issued, and when the access token's claims
    are assembled. It is **not** known at the userinfo endpoint or at
    introspection — an access token here is a bare identifier with nothing
    stored behind it, so there is no client to look rules up for.

    Those two endpoints therefore answer with the documented defaults whatever
    an application has configured. **A suppression is not applied there**: an
    application told not to receive a phone number still receives one if it
    calls userinfo. If you are suppressing a field for disclosure reasons,
    treat that as an open hole rather than a detail — and note that the ID
    token is what most integrations actually read.

    `sub`, `email_verified` and `phone_number_verified` are never mappable.
    The first is reserved on both sides — nothing can be renamed onto it and
    nothing can be renamed away from it — because an application's whole trust
    model rests on it naming one person. The other two follow the claim they
    describe: sent when it is sent under its own name, and not otherwise.

**SAML** — the target is the attribute `Name`, which is what a service
provider actually maps on. `friendlyName` sits beside it and is advisory;
setting it does not change what the SP matches.

**CAS** — the target is an element name in the ticket validation response;
the `cas:` prefix is added for you. `cas:user` is **not** an attribute and not
mappable: it is what every CAS client keys its local record on, and it is the
counterpart of `sub` one protocol over.

**Webhooks** — the target is a key at the top level of the event's `data`
object. This one has consequences the others do not, below.

## Webhooks

The three protocols above assemble a claim set or an attribute list field by
field. A webhook body is not assembled — it is whatever the account or
organization marshals to, and has been since before this feature existed. So
here the rules are applied **on top of** the default body rather than instead
of it. Everything the payload carried, it still carries.

**A rename lifts.** A nested field like `profile.department` cannot stay
nested under a new name, because a target is one name. So it moves to the top
level of `data` and is removed from `profile`:

```json
{
  "id": "…", "displayName": "…",
  "dept": "Analytical Engines",
  "profile": { "title": "Engineer" }
}
```

If lifting empties `profile` entirely, the object is dropped rather than sent
as `{}`.

**Group events are not mapped.** `group.created`, `group.updated`,
`group.deleted` and `group.members_changed` carry a group, and there is no
group vocabulary in the catalogue. Their payloads are delivered exactly as
they always were, even for a subscription that has configured rules for
accounts. This is a decision, not an omission.

**A queued delivery keeps the body it was rendered with.** Payloads are
rendered when the event happens, not when it is sent. Changing a mapping
affects events from that point on; anything already waiting in the queue —
including anything being retried — goes out in the shape it was made. An event
describes what happened, and re-rendering it later would deliver a
`user.disabled` describing an account somebody has since re-enabled.

## What will be refused

At save time, in front of somebody who can fix it — rather than at sign-in, in
front of somebody who cannot.

| | |
|---|---|
| `RESERVED_CLAIM_NAME` | The name is one OpenID Connect acts on: `sub`, `iss`, `aud`, `exp`, `nonce` and the rest. **OIDC applications only** — a SAML attribute called `sub` is unremarkable |
| `DUPLICATE_MAPPING_SOURCE` | Two rules for one field. Which wins would be decided by whichever was read first |
| `DUPLICATE_MAPPING_TARGET` | Two fields under one name. Only one would arrive, and not the one you picked |
| `CLAIM_NAME_TAKEN` | An OIDC rename onto a claim this system already sends — `department` onto `tenant_id`. Not reserved by the specification, and just as occupied |
| `PAYLOAD_NAME_TAKEN` | A webhook rename onto a name the event already uses for something else — `department` onto `id` |
| `MAPPING_TARGET_REQUIRED` | Neither a name nor a suppression, so the rule says nothing |
| `UNKNOWN_FIELD` | No such key in the catalogue. Usually a typo |

The first is the one with teeth. Renaming a department to `nickname` is
somebody's business; renaming it to `sub` would tell an application that one
person is another, in a token it has every reason to trust.

## Which applications receive a given fact

The four kinds share one table, which is what makes this answerable in one
query rather than four. It is the question asked after a disclosure review and
not before one: *who is receiving `department`?*

## Why a list and not a column name

The cheaper design lets a mapping name a database column. The `users` table
also holds `password_hash`, `token_version`, and `failed_login_attempts`.

A configuration that can name a column is one that will eventually name one of
those — and it would be a tenant administrator doing it, through a supported
field, with no code review anywhere in the way. So the catalogue is an
enumeration, and a key it does not hold is refused.

Some entries are outbound-only, and the list says why for each. Several are
security boundaries rather than omissions: a directory attribute that could
set `role` would put privilege escalation in a system Portico does not own.
See [Reading a directory](ldap.md) for where that line is drawn.

## See also

- [Webhooks](webhooks.md) — registering a subscription, signatures, retries
- [Provisioning](scim.md) — where the profile attributes come from
- [Federation](federation.md) — what each protocol sends by default
