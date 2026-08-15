# Webhooks

Portico posts a signed JSON body to an endpoint you register when something
changes: an account is created, updated, enabled, or disabled.

## Registering one

**Webhooks → New subscription** in the console, or:

```bash
curl -X POST https://<host>/api/v1/webhooks \
  -H "Authorization: Bearer <admin token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Billing","url":"https://hooks.example.com/portico","events":["*"]}'
```

The response contains the signing secret. **It is shown once.** Unlike the
other credentials in this system it is stored in the clear, because it signs
rather than authenticates — there is nothing to compare a digest against — so
it is on the same footing as the signing keys, and
[backup-and-restore.md](backup-and-restore.md) says what that means for a
database dump.

## Custom headers

A receiver behind an API gateway usually needs an `Authorization` of its
own. The signature says who produced the body; the gateway is deciding
whether to let the request through at all, and the signature cannot answer
that. Set headers when registering the subscription.

Values are sealed with `PORTICO_ENCRYPTION_KEY`, on the same footing as a
directory bind password: a credential this server stores and later presents,
so there is nothing to compare a digest against. **Without a key configured,
saving one is refused** rather than written in the clear — the console says
so. A subscription with no custom headers needs no key.

They are never served back. The API and the console report the names; the
values are known to whoever typed them.

Refused at registration, rather than at delivery hours later:

| | |
|---|---|
| `X-Portico-Signature`, `-Timestamp`, `-Event`, `-Delivery` | Setting these would let whoever registers a subscription choose what its receiver verifies |
| `Content-Type`, `Content-Length`, `Host`, `User-Agent` | The body is JSON and the transport decides its length; overriding either produces a request that disagrees with itself |
| A value containing a line break | Everything after it is read as another header, or as the body |
| A name that is not an HTTP token | Same, one step earlier |
| More than 10, or a value over 2048 characters | Every one is sent on every delivery |

Portico sets its own headers **after** these, so the order is a second
defence that does not depend on the list above being complete.

## Which destinations are allowed

Https, publicly resolvable, and not an address inside your network. Refused:

| | |
|---|---|
| `http://` | The body and its signature would be readable in transit, and a signature anyone can read and replay proves nothing |
| Loopback (`127.0.0.1`, `::1`, `localhost`) | The database this process is already authenticated to |
| Private ranges (`10/8`, `172.16/12`, `192.168/16`) | Your internal network |
| Link-local (`169.254/16`) | Cloud metadata — the endpoint that hands out credentials to anything that asks |
| Carrier-grade NAT (`100.64/10`) | Container and carrier infrastructure |
| URLs with credentials | They would be stored and logged |

This is not a hardening option; it is what stops a tenant administrator from
using Portico as a proxy into the network Portico runs in.

**The check runs twice**, and the second one is why it works. Checking only
at registration is defeated by a name that resolves publicly then and to
`127.0.0.1` later — DNS rebinding, which needs nothing but a DNS record the
attacker controls. So the address actually being connected to is verified
inside the dialer, at connection time. Redirects are not followed, for the
same reason.

## Verifying a delivery

Every request carries:

| Header | |
|---|---|
| `X-Portico-Event` | The event type |
| `X-Portico-Delivery` | The delivery id, which is also `id` in the body |
| `X-Portico-Timestamp` | Unix seconds |
| `X-Portico-Signature` | `sha256=` followed by a hex HMAC |

The signature is HMAC-SHA256, keyed with your secret, over **the timestamp,
a literal `.`, and the raw request body**:

```
signature = "sha256=" + hex(hmac_sha256(secret, timestamp + "." + body))
```

The timestamp is inside the signature rather than merely sent beside it. A
signature over the body alone is replayable forever by anyone who ever saw
one — including your own logs — and a replayed `user.disabled` is a denial of
service against one person's account in whatever consumes these.

The header may carry **more than one signature**, comma separated, and any
of them verifying is enough. That happens during a secret rotation, when
each delivery is signed with both the new key and the one it replaced —
see [Rotating the secret](#rotating-the-secret). Split on `,` even if you
have never rotated: a receiver that compares the whole header as one string
works perfectly until the day somebody rotates, and then rejects everything
while looking healthy.

```python
import hashlib, hmac, time

def verify(secret: str, headers, body: bytes) -> bool:
    timestamp = headers["X-Portico-Timestamp"]
    expected = "sha256=" + hmac.new(
        secret.encode(), f"{timestamp}.".encode() + body, hashlib.sha256
    ).hexdigest()
    # Any one of them. During a rotation there are two, and which one is
    # yours depends on whether you have deployed the new secret yet.
    #
    # Constant time: a fast string comparison leaks how much of a forged
    # signature was right, one byte at a time.
    signatures = headers["X-Portico-Signature"].split(",")
    if not any(hmac.compare_digest(expected, s.strip()) for s in signatures):
        return False
    # And reject anything too old to be current, or the signature only proves
    # the body was ours at some point in history.
    return abs(time.time() - int(timestamp)) < 300
```

Use the **raw** body, before any JSON parsing. Re-serializing changes
whitespace and key order, and the signature is over bytes.

## Rotating the secret

**Webhooks → Rotate secret**, or `POST /api/v1/webhooks/{id}/rotate-secret`.
The new secret is shown once, along with `previousSecretExpiresAt`.

For **24 hours** after that, every delivery carries two signatures: one with
the new key and one with the old. Install the new secret at your end before
the deadline; until then either verifies, so there is no moment where
deliveries fail.

That window is the whole point. Portico produces the signature and you check
it, so you are the side with something to deploy — a rotation taking effect
instantly would reject every delivery until you had.

The subscription keeps its id, so the delivery history and any deduplication
you do on delivery ids survive. Deleting and re-registering was the only
previous remedy for a leaked key, and it threw both away.

Rotating again before the window closes replaces the pending old key rather
than keeping three. If a key has actually leaked and you cannot wait 24
hours, rotate and then pause the subscription until your receiver is
updated: pausing stops deliveries rather than signing them with a key
somebody else holds.

## The body

```json
{
  "id": "3f1c…",
  "type": "user.disabled",
  "tenant": "6b2e…",
  "occurredAt": "2026-08-08T09:15:00Z",
  "data": { "id": "…", "username": "jsmith", "status": "DISABLED", "…": "…" }
}
```

`data` is the account or organization as the API returns it. The body is
rendered when the event happens and stored as sent — an event describes what
happened, and re-rendering at delivery time would send a "disabled" event
describing an account somebody has since re-enabled.

## Events

| Event | Sent when |
|---|---|
| `user.created` | An account is created, however it arrived — console, import, registration, SCIM, or a directory synchronization |
| `user.updated` | An account's details change |
| `user.enabled` | An account is enabled |
| `user.disabled` | An account is disabled, including when somebody closes their own — the payload's `closedAt` tells the two apart |
| `user.password_changed` | A password is set, whether by its owner, by an administrator's reset, or by completing recovery. Every session ends with it. No credential is in the payload |
| `user.locked` | An account is locked by consecutive failed sign-ins. Sent once, where the lock is applied — not on each later attempt against an account that is already locked |
| `user.unlocked` | An administrator unlocks an account. A password change also clears a lock, and reports itself as `user.password_changed` |
| `organization.created` | An organization is created |
| `organization.updated` | An organization's name, code, parent, or order changes |
| `organization.enabled` | An organization is enabled |
| `organization.disabled` | An organization is disabled. Existing members stay where they are; no new ones can be added |
| `group.created` | A group is created |
| `group.updated` | A group's details change |
| `group.deleted` | A group is deleted |
| `group.members_changed` | A group's membership changes |

`group.members_changed` carries the group as it now stands rather than the
delta — a subscriber wanting to know who is in a group reads the group, and
an event per member would turn a bulk replacement into a burst nobody asked
for.

Subscribe to `*` for everything including types added in future versions, or
name the ones your endpoint can handle. `GET /api/v1/webhooks/events` returns
the current list.

## Reading the delivery log

`GET /api/v1/webhooks/{id}/deliveries` answers one page, newest first, and
the console shows the same thing.

**Paged by cursor, not by offset.** Pass the previous page's `nextCursor`;
omit it for the first page. The table is written to while it is read — every
event, every retry — so an offset walked backwards through it returns some
rows twice and skips others, with nothing to tell the reader. There is
deliberately no total for the same reason: it would be stale before it
arrived.

**Filtered to events by default.** `filter=live` hides the pages a full sync
produces, `sync` shows only those, `all` shows both. A full sync of a large
tenant queues a hundred deliveries in a few seconds, which is not many rows
but is enough to push every ordinary event off the first page of what
somebody is reading.

`GET /api/v1/webhooks/{id}/deliveries/{deliveryID}` adds the bodies: the
request exactly as sent — the same bytes the signature was computed over, so
a receiver debugging a signature mismatch has something to compare against —
and the beginning of what the receiver answered, capped at 2 KiB when it was
stored. A 400 is usually a sentence saying which field was not liked, and
that sentence used to live only in the receiver's logs.

> **Request headers are not stored, and are not in that response.** A
> subscription's custom headers are credentials, sealed with
> `PORTICO_ENCRYPTION_KEY` precisely so that a database dump does not yield
> them. Copying them into a delivery row to make a debugging screen more
> complete would undo that for every delivery ever made.

## Filling in what happened before you subscribed

Events describe changes. A subscription created today has missed every
change that came before it, and the delivery history cannot fill the gap:
finished deliveries are removed after thirty days, and the ones that survive
say what happened rather than what is.

So a receiver building a mirror asks for a **full sync**. In the console
there are two doors to it, and they arrive at the same question:

- **Full sync** on the subscription in the list, at any time.
- A box on the create form — *also send everything that already exists* —
  which is where most people realise they need one. It is unticked, and
  ticking it queues nothing by itself: it asks, once the signing secret has
  been read, through the same confirmation the button opens.

Either way the confirmation names what will go — how many accounts,
organizations and groups, and how many pages — before anything is queued.
That count is the point of asking: "send a copy of everything?" is a
different decision at fifty accounts and at fifty thousand.

From the API:

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" \
  https://portico.example.com/api/v1/webhooks/$ID/snapshot
```

The endpoint is called `snapshot` and the console calls the button **Full
sync**. The console is the one that changed: "snapshot" elsewhere in
operations means a stored copy of a moment, and nothing is stored here — the
current state is sent, in pages, to one receiver. The events have always
been `sync.*`. The path keeps its name because it is published contract, and
renaming it to match a label would break every integration for the sake of
tidiness.

What arrives is a run, through the same endpoint, the same signature, and
the same field mappings as everything else:

| Event | Body |
|---|---|
| `sync.started` | `syncId`, the `scope` that will follow, `pageSize`, `asOf` |
| `sync.users` | `syncId`, `kind`, `page`, `total`, `items[]` |
| `sync.organizations` | the same shape |
| `sync.groups` | the same shape |
| `sync.completed` | `syncId`, and `counts` per kind |

The objects inside `items` are the objects the ordinary events carry, so a
receiver parses one shape rather than two. Pages rather than one event per
account, because the unit that matters is a batch you can write in one
transaction: fifty thousand accounts is a hundred deliveries here and fifty
thousand under any scheme that sends one each.

**Which kinds arrive follows what the subscription selected.** One that asked
only for group events is sent only groups.

`sync.completed` is the signal that you now hold everything and may switch
from building your mirror to trusting it. Compare its `counts` against your
own rows: a mismatch means a page you answered 200 to never landed.

> **This asks something of the receiver, and it is not optional.**
>
> A full sync is not taken atomically. Pages are read one after another while
> the tenant carries on changing, and live events keep arriving throughout —
> so an account edited during the run may reach you as a page or as an event,
> in either order.
>
> **Reconcile by `id`, and let the newer `occurredAt` win.** A receiver that
> applies whatever arrives last will end up holding the older copy, and
> nothing will tell it so.
>
> This is the same demand at-least-once delivery already makes. Retries mean
> you can see any delivery twice; the envelope's `id` is what you deduplicate
> on.

One full sync at a time per subscription: a second while the first is still
queued answers `409 SNAPSHOT_IN_PROGRESS`. A disabled subscription is refused
outright rather than having the largest delivery this product makes queued
against the moment somebody re-enables it.

## Delivery, retries, and what "delivered" means

Deliveries are queued when the event happens and sent by a worker every
fifteen seconds. The operation that caused the event never waits for your
endpoint — creating a user is not slower because a subscriber is slow, and
does not fail because one is down.

**At least once, never exactly once.** A response lost on the way back is
indistinguishable from one that never arrived, so a delivery you already
processed can arrive again. Use the `id` to recognize it.

Any 2xx is success. A 5xx, a 429, or a network failure is retried — five
attempts over roughly half an hour, then the delivery is marked failed and
left in the history. Any other status, including a redirect, is not retried:
a 400 means you understood and refused, and sending it four more times
produces four more refusals.

Respond quickly and do the work afterwards. The request times out after
twenty seconds.

Pausing a subscription stops events being queued for it. Resuming does not
deliver what happened while it was paused — that is what pausing means.

## When a subscriber says they are receiving nothing

**Webhooks → Deliveries** shows what was attempted,
what came back, and how many times. That is the difference between "we never
sent it" and "your endpoint answered 500 five times", and it does not require
asking the receiver to check their own logs.

Delivery records are kept for thirty days.

`GET /api/v1/webhooks/{id}/deliveries` returns the fifty most recent by
default. `?limit=` takes anything from 1 to 200; a value outside that, or one
that is not a number, is ignored rather than refused — the screen asking is
showing a list, and failing it over a query string would replace the list
with an error.

## Running in a container

The release image includes a CA certificate bundle, which it did not before
webhooks existed. Nothing to configure; it is noted because an image built
from an older Dockerfile would fail every delivery with a certificate error
while working perfectly on a developer's machine, which has a system store.

Delivery is not the only thing that needs it. Sending mail over SMTP with
STARTTLS or implicit TLS, and reading a directory over LDAPS or LDAP with
StartTLS, verify against the same bundle — so an image without one breaks
email and directory synchronization too, and each reports it as its own kind
of certificate error.
