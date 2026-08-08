# Webhooks

Portico posts a signed JSON body to an endpoint you register when something
changes: an account is created, updated, enabled, or disabled.

## Registering one

**Settings → Event subscriptions → New subscription**, or:

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

```python
import hashlib, hmac, time

def verify(secret: str, headers, body: bytes) -> bool:
    timestamp = headers["X-Portico-Timestamp"]
    expected = "sha256=" + hmac.new(
        secret.encode(), f"{timestamp}.".encode() + body, hashlib.sha256
    ).hexdigest()
    # Constant time: a fast string comparison leaks how much of a forged
    # signature was right, one byte at a time.
    if not hmac.compare_digest(expected, headers["X-Portico-Signature"]):
        return False
    # And reject anything too old to be current, or the signature only proves
    # the body was ours at some point in history.
    return abs(time.time() - int(timestamp)) < 300
```

Use the **raw** body, before any JSON parsing. Re-serializing changes
whitespace and key order, and the signature is over bytes.

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

`user.created`, `user.updated`, `user.enabled`, `user.disabled`,
`organization.created`, `organization.updated`.

Subscribe to `*` for everything including types added in future versions, or
name the ones your endpoint can handle. `GET /api/v1/webhooks/events` returns
the current list.

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

**Settings → Event subscriptions → Deliveries** shows what was attempted,
what came back, and how many times. That is the difference between "we never
sent it" and "your endpoint answered 500 five times", and it does not require
asking the receiver to check their own logs.

Delivery records are kept for thirty days.

## Running in a container

The release image includes a CA certificate bundle, which it did not before
webhooks existed — delivery is the only outbound TLS this server makes.
Nothing to configure; it is noted because an image built from an older
Dockerfile would fail every delivery with a certificate error while working
on a developer's machine.
