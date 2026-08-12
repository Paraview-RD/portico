# Access Guide

How to reach a running Portico instance, where credentials come from, and
what each role can actually do.

## Entry points

| What | URL | Notes |
|---|---|---|
| Web UI | `http://<host>:8410/` | Served by the same process as the API |
| API | `http://<host>:8410/api/v1/` | See [api-conventions.md](https://github.com/Paraview-RD/portico/blob/main/docs/api-conventions.md) |
| Liveness | `http://<host>:8410/api/v1/health` | No authentication. Answers as long as the process is up, deliberately without touching the database — see below |
| Readiness | `http://<host>:8410/api/v1/ready` | No authentication. 200 when this instance can reach its database, 503 when it cannot |
| OpenID discovery | `http://<host>:8410/.well-known/openid-configuration` | The default tenant. Others at `/t/<code>/…` |
| SAML metadata | `http://<host>:8410/saml/metadata` | Hand this to a service provider. Others at `/t/<code>/saml/metadata` |
| CAS | `http://<host>:8410/cas` | The client's "CAS server URL". Others at `/t/<code>/cas` |
| SCIM 2.0 | `http://<host>:8410/scim/v2` | The "base URL" a directory asks for. Authenticated by a bearer token issued under **Integration → Directory integration**, not by an administrator session — see [scim.md](scim.md) |
| Metrics | `http://<host>:9410/metrics` | **A separate port, off by default, and not authenticated.** See below |

The port is `PORTICO_ADDR` (default `:8410`). In development the frontend
also runs a Vite server on `:5410` that proxies `/api` to `:8410`; in
production there is only the one port — unless you turn on metrics, which
get one of their own.

## Metrics

Set `PORTICO_METRICS_ADDR` (for example `127.0.0.1:9410`) to publish
Prometheus metrics. Leave it unset and nothing is published and no second
listener is opened.

**It is a separate listener because it is unauthenticated.** No Prometheus
deployment authenticates its scrapes, so every exporter in this ecosystem
assumes its address is reachable only from inside. Serving it as a route on
the application port would make it exactly as reachable as the login page,
and the mistake would be invisible — a scrape config that works. Bind it to
an interface only your monitoring can reach, and never publish it through
the proxy that serves the application.

What is worth an alert:

| Metric | Why |
|---|---|
| `portico_sign_in_attempts_total{outcome="bad_credentials"}` | A rate climbing across many accounts is credential stuffing. Portico does not rate-limit; this is how you find out you need to. |
| `portico_account_lockouts_total` | Counted where a lock is *applied*. A spike is either an attack or a policy set too tight for real people. |
| `portico_sign_in_attempts_total{outcome="password_expired"}` | Only interesting just after enabling expiry, when it tells you how many people are about to contact you at once. |
| `portico_sign_in_attempts_total{outcome="password_change_required"}` | Somebody signed in with the default bootstrap password. On day one that is you. On day two it means the default is still in place and being found. |
| `portico_db_connections_wait_total` | Pool exhaustion, which presents as everything being slow with no errors and no slow query to find. This is the metric that names it. |
| `portico_http_requests_total{status=~"5.."}` | The label holds the exact code, so this has to be a regular expression — `status="5xx"` matches no series at all and the alert built on it never fires. Client disconnects are reported as `499` and fall outside it, so a rise here is real. |

Every series is initialised to zero at startup, so `rate(...)` returns zero
rather than nothing on a quiet instance — an alert can tell "no failed
sign-ins" from "not reporting".

Nothing is labelled with a tenant or a request path, both deliberately:
those are values created from outside, and a label whose cardinality other
people control is how a metrics endpoint becomes the largest thing a process
produces.

## Tenants

Every account belongs to exactly one tenant, and no account can act outside
its own. A fresh deployment gets one tenant, code `default`, created on
first start — sign-in that names no tenant lands there, so a single-tenant
deployment never has to know tenants exist.

More tenants are provisioned from the command line, because there is no
cross-tenant role for the API to authorize:

```sh
export PORTICO_DB_DSN=postgres://portico:secret@localhost:5443/portico?sslmode=disable

portico tenant create --code acme --name "Acme Corp"   # admin on the default
                                                       # password, which must
                                                       # be replaced to sign in
portico tenant create --code acme --name "Acme Corp" \
  --admin-password "$(openssl rand -base64 18)"        # or choose one, and
                                                       # skip the forced change
portico tenant list
portico tenant disable --code acme                     # refuses sign-in,
portico tenant enable  --code acme                     # deletes nothing
```

Each tenant gets its own administrator at creation. Users of a non-default
tenant sign in with its code — either typed into the **Tenant** field, or
via a link that carries it:

```
http://<host>:8410/login?tenant=acme
```

## Credentials

Portico stores its own accounts — there is no external identity provider in
the MVP.

| Credential | Where it comes from |
|---|---|
| Bootstrap administrator | Created on first start in the `default` tenant. Username from `PORTICO_INITIAL_ADMIN_USERNAME` (default `admin`). |
| A further tenant's administrator | Created by `portico tenant create`; without `--admin-password` it takes the same default, on the same terms. |
| Bootstrap password | `PORTICO_INITIAL_ADMIN_PASSWORD` if set. Otherwise the documented default `Portico@1`, which **sign-in refuses until it is replaced** — the first attempt answers `PASSWORD_CHANGE_REQUIRED` and the screen asks for a new password on the spot. |
| JWT signing secret | `PORTICO_JWT_SECRET`. If unset, a random secret is generated per start, which silently invalidates every session on restart. Set it. |
| Everyone else's password | Set by an administrator at creation, chosen by the user at self-registration, or set through a password-recovery link. |

No credential values belong in this file, in the repository, or in a
committed `.env`. `.env.example` lists the variable names only.

## A demo, without a first run

If the point is to look rather than to deploy,
[open a Codespace](https://codespaces.new/Paraview-RD/portico). GitHub builds
the console, the manual and the server, seeds a database, and opens the
console. None of the section below applies: the database already holds
accounts, so there is no bootstrap administrator and no password to change on
the way in.

| | |
|---|---|
| Sign in as | `zhangwei` or `chenjing` (super administrator), `liyan` (ordinary user) |
| Password | `Portico@1`, shared by every seeded account |
| Second tenant | `acme`, holding the same names with almost nothing carried across |
| Mail | a Mailpit inbox on a second forwarded port; nothing leaves the machine |
| Manual | `/docs`, built from the working copy rather than from a release |

Two differences from a deployment, both of them GitHub's doing rather than
Portico's:

- **The address is private to whoever opened it.** Forwarded ports default to
  private, so the `https://<name>-8410.app.github.dev` URL asks for that
  person's GitHub session and nobody else's. There is no link to send around.
  Somebody who wants to look opens their own from the same button, on their
  own free quota. Making a port public is possible and is a different
  decision — it puts an unauthenticated-by-default identity server on the
  open internet, which is the thing
  [SECURITY.md](https://github.com/Paraview-RD/portico/blob/main/SECURITY.md)
  is about.
- **TLS is the proxy's, not Portico's.** `PORTICO_PUBLIC_URL` is set to the
  forwarded `https://` address, which is what OpenID Connect redirects and
  SAML metadata are built from — so federation works, over a certificate
  Portico never sees. On your own hardware that is the reverse proxy's job;
  see [Before you expose this](#before-you-expose-this).

## First run

1. Start the server. `./portico` needs `PORTICO_DB_DSN`; compose needs
   `POSTGRES_PASSWORD` and `PORTICO_JWT_SECRET` exported first, and stops
   naming whichever is missing rather than defaulting one:

    ```bash
    export POSTGRES_PASSWORD=$(openssl rand -hex 16)
    export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
    docker compose -f deploy/docker-compose.yml up -d
    ```
2. Sign in as `admin` with `Portico@1`, unless you set a password of your
   own. **Do this now rather than later.** That password is in this file, so
   until somebody replaces it the account belongs to whoever reaches the
   instance first — and whoever does gets to set a password you will not
   know, on an account with no address to recover through.
3. The sign-in is refused and the screen asks for a new password: enter the
   default again as the current password, choose a replacement, and you are
   signed in. There is no separate visit to **My profile** for this one, and
   no sign-out afterwards — the replacement issues the session itself.

If you set `PORTICO_INITIAL_ADMIN_PASSWORD`, none of step 3 happens: you
chose a secret that is not published anywhere, so the account signs in
normally.

## Password recovery

A user who cannot sign in asks for a reset link from the sign-in screen. The
link is single-use and expires in 30 minutes.

**It needs a mail relay.** With `PORTICO_SMTP_HOST` unset, the recovery
screen says so plainly rather than accepting a request it cannot fulfil, and
the only way back in is an administrator resetting the password. Set
`PORTICO_SMTP_HOST`, `PORTICO_SMTP_FROM`, and `PORTICO_PUBLIC_URL` — the
last one because the link has to point somewhere, and a server behind a
proxy cannot work that out for itself.

While developing, point it at a local catcher rather than a real relay:

```sh
docker run -d -p 1025:1025 -p 8025:8025 axllent/mailpit

export PORTICO_SMTP_HOST=127.0.0.1
export PORTICO_SMTP_PORT=1025
export PORTICO_SMTP_ENCRYPTION=none
export PORTICO_SMTP_FROM=portico@example.com
export PORTICO_PUBLIC_URL=http://localhost:8410
```

Messages then appear at <http://localhost:8025> and nothing leaves the
machine.

Recovery by SMS is defined but has no provider in this version, so the
sign-in screen offers email only.

## Self-registration

Off by default. **Settings → registration** opens it, and a second box under
it requires a new account to confirm its email address before it can sign in.

Turn that second one on before opening registration to anything but a trusted
network. Without it the address on a new account is whatever was typed — and
that address is both a sign-in identifier and where a password-reset link
goes, so somebody can open an account under a colleague's address and receive
their reset links.

It needs the same mail relay password recovery does, and saving is refused
without one rather than accepting a setting that would strand every
registration on a message that never arrives.

**Turning it on does not affect anybody who has already registered.** Their
addresses were accepted under the rules in force at the time, and a policy
change is not grounds for revoking access; an administrator who wants that
can disable the accounts deliberately.

Somebody refused for an unconfirmed address is told so at sign-in and can ask
for another message from that screen — they have to type the address rather
than the username, because that is where it goes.

## Roles

The MVP has exactly two roles and no way to define more (requirements §3.3).

### Administrator (`SUPER_ADMIN`)

Can do everything: create/edit users and their details, enable and disable
accounts one at a time or in bulk, reset other people's passwords,
bulk-import from a spreadsheet and export back to one, manage
organizations and groups, register the applications that sign in through
Portico, issue the credentials a directory provisions with, subscribe other
systems to events, read the audit log, and change system settings.

Everything an administrator does is scoped to their own tenant. There is no
cross-tenant role.

Typical journey — onboard a batch of existing users:

1. **Organizations** → create the organizations first, noting their codes.
2. **Users** → **Import** → download the template, fill it in (the
   `organizationCode` column matches the codes from step 1), upload.
3. Read the per-row result. Failed rows are listed with the row number and
   reason; fix those rows and re-upload just them.
4. **Audit logs** → filter to *Registration* to confirm what landed.

Typical journey — connect an application:

1. **Applications** → pick the protocol tab the application speaks.
2. **Register** — for OAuth/OIDC give it a client id and its redirect URIs;
   for SAML upload or paste the service provider's metadata document; for
   CAS give its URL prefix.
3. A confidential OAuth client's secret is shown **once**. Copy it then —
   only a hash is stored, so the only way to get another is **Rotate
   secret**, which invalidates the current one immediately.
4. **Integration endpoints** → copy the issuer, discovery document, SAML
   metadata address, or CAS server URL into the application's own
   configuration. These come from the running deployment, so they always
   match what is actually served.
5. Fill in the **launch address** and the **icon** while you are there. Both
   are optional and neither affects signing in — but an application without
   a launch address never appears on anybody's Home screen, and the person
   who notices is a user who was told the application is available and
   cannot find it. None of the addresses already on the form is a launch
   address: a redirect URI, an assertion consumer service, and a CAS prefix
   are all places a protocol sends a browser mid-flow, and opening one
   directly produces an error rather than the application.
6. **Audit logs** → the registration is recorded with the redirect URIs or
   assertion consumer services it permits.

Typical journey — record something about people that Portico has no field for:

1. **System → User attributes** → **New attribute**. Give it a key, a label,
   and a type — text, number, yes/no, date, or a choice from a list you write
   out. Mark it required if an account form should not be finished without it.
2. It appears on every account form from then on: **Users** → any account →
   **Attributes**. Existing accounts keep whatever they had, which is nothing,
   and the server does not refuse them for it — required binds the form, not
   the accounts that predate the attribute.
3. The key cannot be changed later, because a field mapping stores that key. A
   rename would leave the rule pointing at nothing while its editor still
   looked fine.
4. **Retire** takes it off the form and keeps every value recorded so far.
   **Delete** discards the definition and all of those values, and cannot be
   undone. They are two controls rather than one toggle for that reason.
5. It is now in the field catalogue like any built-in — but nothing sends it
   until an application or a subscription names it. See the next journey.

Typical journey — the downstream system reads the wrong field name:

1. **Applications** (or **Webhooks**) → find it → **Fields**.
2. The whole catalogue is listed, grouped, with the stored key under each
   label. The label is what you read; the key is what the API stores, and
   whoever writes the receiving end needs that one.
3. Leave a row alone and it behaves exactly as it always has. Type a name and
   that fact goes out under that name instead. Tick **Do not send** to stop
   sending it. Most of the catalogue is not sent at all by default — those
   rows only go out if you name one.
4. **Save** replaces the whole set. An empty set is not "send nothing": it is
   the documented defaults, exactly.
5. **Audit logs** → the change is recorded as the rules, not as anybody's
   values.

One thing to know before relying on it: a webhook delivery already queued
keeps the body it was rendered with, so changing a mapping affects events from
that point on rather than anything already waiting. For OpenID Connect the
rules apply to the ID token, the access token, userinfo, and introspection
alike — a suppression cannot be got around by asking a different endpoint. See
[field-mappings.md](field-mappings.md).

Disabling an application stops it immediately — it can no longer sign anyone
in, and its credentials stop authenticating at the introspection and
revocation endpoints too. Nothing is deleted, and there is no delete: the
audit trail keeps pointing at something that still exists.

Typical journey — hand over a copy of the directory:

1. **Users** → filter to whoever the request is about. The export takes the
   filters that are on screen, so what you are looking at is what you get.
2. **Export** → an .xlsx in the same columns the import template uses, so a
   file taken out can be corrected and fed back in.
3. **It carries no passwords.** The password column is there and every cell
   in it is empty, which is not an oversight: the importer reads columns by
   position, so that a translated header still works — and an export missing
   that column would put every field one place to the left on the way back
   in, silently. So the heading stays and the values do not. What must never
   appear in a report is a credential; a heading is not one.
4. **It is recorded.** Every export appears in **Audit logs** as
   `USER_EXPORT`, with who ran it and how many accounts left. This is every
   attribute of every matching account leaving through one request — nothing
   else here hands over that much at once, and "who took a copy, and when" is
   asked after an incident rather than before one.

Typical journey — act on many accounts at once:

1. **Users** → tick the accounts, or the box in the header to take the whole
   page. Selection is per page on purpose: a control that silently included
   accounts you never scrolled past would be a bad thing to attach *disable*
   to.
2. **Enable**, **Disable**, or **Move to organization** from the bar that
   appears.
3. Every account goes through the same path a single one does, so the rules
   still hold: the last active administrator cannot be disabled, you cannot
   disable yourself, and each account's sessions and downstream refresh
   tokens end immediately. Selecting more people is not a way around any of
   that.
4. Results come back per account. If some failed, the reason names *which* —
   act on those and leave the rest alone rather than repeating the whole
   selection.

Typical journey — read accounts out of an AD:

1. **Directory integration** → the **Portico reads (LDAP / AD)** tab → add a
   directory. Host, base DN, and a read-only service account; Portico never
   writes to your directory.
2. Pick the preset that matches, then **check every attribute against your
   own directory**. They are presets rather than defaults because Active
   Directory and OpenLDAP disagree on all of them, and a wrong one imports
   everybody named after the wrong field.
3. The external id attribute is the one to get right — `objectGUID` on AD,
   `entryUUID` on OpenLDAP. It is what makes a rename in the directory a
   rename here instead of a second account.
4. **Synchronize**, then read the counts. Anything skipped is an entry that
   could not become an account, most often a username an account Portico
   owns already holds.
5. Once the counts look right, set **Synchronize automatically** — every
   fifteen minutes to once a week. It is off until you set it, and turning it
   on runs one immediately rather than at the end of the first interval. Do
   not build a cron job against the sync endpoint instead: that needs an
   administrator's password in the cron environment, which is a worse
   credential to leave lying about than the bind password.
6. Storing a bind password needs `PORTICO_ENCRYPTION_KEY` set on the
   deployment. Without it the save is refused rather than the credential
   being written in the clear.

Full detail, including what a run refuses to do, is in [ldap.md](ldap.md).

Typical journey — let a directory keep the accounts up to date:

1. **Provisioning** → issue a credential. Like a client secret it is shown
   once, and it is a tenant-wide token that can create, update, and disable
   every account — treat issuing one as the privileged act it is.
2. In the directory, set the base URL to `/scim/v2` from the entry table
   above and the token as the bearer credential.
3. Push a small test group first. Failures come back in SCIM's own error
   shape, so the directory shows them rather than reporting a generic
   failure.
4. **Groups** → a directory-owned group is marked *Directory*, because an
   edit made here may be overwritten by the next sync. Accounts carry the
   same origin in their `externalId` but the user list does not yet show it.

[scim.md](scim.md) is the one to hand an integrator; it leads with what is
*not* provisioned, which is what they need before they start rather than
after.

**Webhooks** is the third direction: not accounts arriving or being pulled
in, but Portico telling another system that something changed.

Typical journey — tell a downstream system when accounts change:

1. **Webhooks** → **New subscription**. The destination must be public
   HTTPS. Loopback, private ranges, link-local — which is where cloud
   metadata lives — and carrier-grade NAT are refused, and the address is
   checked again at connection time, so a name that resolves publicly now
   and privately later does not work either. This is not a hardening option
   to weigh: it is what stops a subscription being used to reach the network
   Portico runs in.
2. **The signing secret is shown once.** It is not recoverable — but unlike
   a client secret it is not hashed either, because it signs rather than
   authenticates. What that means for backups is in
   [backup-and-restore.md](backup-and-restore.md).
3. Give the receiving team [webhooks.md](webhooks.md) before they write the
   handler. The verification example there splits `X-Portico-Signature` on
   `,`, and that detail is not decoration — see rotation below.
4. **Deliveries** shows what was attempted, what came back, and how many
   times. Five attempts spread over roughly half an hour, then the delivery
   is marked failed; records are kept thirty days. This is the screen that
   distinguishes "we never sent it" from "your endpoint answered 500 five
   times", without asking the receiver to go and read their own logs.
5. **Disable** stops delivery and keeps the subscription and its history;
   **delete** is for an integration that is gone.

**Rotating a secret asks something of the receiver.** A rotation issues a new
key and keeps the old one alive for twenty-four hours, during which **every
delivery carries both signatures, comma separated**, and either verifies. A
receiver that compares the whole header as one string works perfectly until
the day somebody rotates, and then rejects everything while looking healthy.
The console says so before it starts a rotation; this is the same warning,
for whoever reads the guide instead.

**Custom headers need `PORTICO_ENCRYPTION_KEY`.** A subscription can send
headers of its own, for a receiver behind a gateway that wants an
`Authorization` of its own — the signature says who produced the body, which
is not the question a gateway is asking. Their values are credentials, so
they are sealed with the same key that seals a directory bind password, and
**without that variable set the save is refused** rather than the value being
written in the clear. They are never served back: the API and this screen
report the header *names*, which answers "what is this subscription sending"
without making a listing a way to read every credential the tenant holds.
Headers that would change what the delivery *is* — the signature headers,
`Content-Type`, `Host` — are refused at registration.

### User (`USER`)

Signing in lands on **Home**: the applications registered in their tenant
that have a launch address, their account at a glance, their last few
sign-ins, and a warning if their password is about to expire or they have no
address to recover it to. From there, **My profile** — details, password,
and the devices signed in as them, any of which they can end.

**Details** is the descriptive half of an account — name parts, job title,
department, locale, address, the fields a directory would hold — and people
maintain their own. It reaches none of the deciding half: not their role, not
their status, not which organization they sit in. That is not a rule the
screen enforces by hiding controls; describing somebody and deciding their
access are separate endpoints, and the one they can call has no way to carry
a role. Their **manager** is on the same side of that line: a reporting line
is an organizational fact that downstream systems read as an approval chain,
so an administrator sets it.

Those two entries are the whole navigation. Typing an administrative address
by hand shows Home instead, and the address bar is corrected to say so —
but that is a convenience, not the boundary: the server answers
`ADMIN_REQUIRED` to an administrative call whatever the UI is showing.

The applications on Home are **the tenant's, not theirs**, and the screen
says so. This version has two fixed roles and no notion of who may use what,
so everybody's list is identical; without that sentence a reader would
reasonably conclude that an application missing from a colleague's Home is
one that colleague was not granted.

Self-registered accounts always get this role; it cannot be requested at
sign-up.

**Closing an account.** Anyone can close their own from **My profile**, after
confirming with their password. It is the one place self-disabling is allowed
— everywhere else it is refused so that an administrator cannot lock
themselves out by accident.

It deactivates rather than deletes: the account stops signing in, every
session and every federated refresh token ends immediately, and the audit
trail keeps pointing at an account that still exists. **An administrator can
reinstate it** by enabling the account, which also clears the closure mark;
the sessions that were ended stay ended. A tenant's last active administrator
cannot close theirs, since nobody would be left to undo it.

Somebody who closes their account and then tries to sign in is told the
account was closed, not that it was disabled — the two call for different
responses, and the user list shows which happened.

## Before you expose this

Portico serves plain HTTP and does not rate-limit requests. Both are
deliberate — it delegates them to the reverse proxy rather than
reimplementing them — but that makes the proxy mandatory, not optional, for
anything reachable beyond localhost. See [SECURITY.md](https://github.com/Paraview-RD/portico/blob/main/SECURITY.md) for
why.

The bundled compose file binds to `127.0.0.1` so the default configuration
is not directly reachable.

### nginx

```nginx
limit_req_zone $binary_remote_addr zone=portico_auth:10m rate=10r/m;

server {
    listen 443 ssl http2;
    server_name id.example.com;

    ssl_certificate     /etc/letsencrypt/live/id.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/id.example.com/privkey.pem;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    # Throttle the credential endpoints; everything else is already
    # authenticated.
    location ~ ^/api/v1/auth/(login|register)$ {
        limit_req zone=portico_auth burst=5 nodelay;
        proxy_pass http://127.0.0.1:8410;
        include /etc/nginx/proxy_params;
    }

    location / {
        proxy_pass http://127.0.0.1:8410;
        include /etc/nginx/proxy_params;
    }
}
```

### Caddy

```caddyfile
id.example.com {
    @auth path /api/v1/auth/login /api/v1/auth/register
    rate_limit @auth {
        zone portico_auth {
            key    {remote_host}
            events 10
            window 1m
        }
    }
    reverse_proxy 127.0.0.1:8410
}
```

With either in place, set `PORTICO_TRUST_PROXY_HEADERS=true` so the audit log
records real client addresses rather than the proxy's. Do **not** set it
without a proxy: callers could then forge the IP attributed to their own
actions.

## Guard rails worth knowing

- **Accounts are never deleted.** Disabling is the substitute, so the audit
  trail stays intact. A disabled user is signed out immediately.
- **The last administrator cannot be disabled or demoted**, and nobody can
  disable their own account. Both are otherwise unrecoverable without
  database surgery.
- **Registration is off by default.** Turn it on in **Settings** when you
  want it; an instance exposed before anyone finishes setup will not accept
  sign-ups.
- **Organizations are a tree.** Set a parent when creating one, or change
  it later to rearrange — the code stays fixed, because downstream systems
  may have stored it. A move that would put an organization inside its own
  branch is refused; a foreign key cannot catch that, since every row in a
  cycle is individually valid.
- **Disabling an organization** blocks new members but keeps existing ones.
  It does not cascade to children, which stay as they were.
- **Signing out ends that session, not all of them.** A laptop and a phone
  are separate sign-ins; **Sign out everywhere**, on your profile, is the
  one that ends both. Your profile also lists what is currently signed in as
  you, with the address and browser as they arrived, and lets you end any of
  it. An administrator can see and end a user's sessions from the user list,
  which is what "I think my account is compromised" needs on the other end
  of the phone. Federated sessions are the exception to all of this: signing
  out anywhere ends every one of them, because signing out of a single
  sign-on system is read as signing out of the applications too. See
  [federation.md](federation.md) for what that does and does not reach in
  each protocol.
- **A reset link ends every session.** Completing recovery changes the
  password, and a password change revokes every live token — which is the
  point, since the reason for recovering is often that somebody else had it.
- **Asking for a second link invalidates the first.** Only the newest one
  works.
- **Password rules are per tenant, in Settings**, and mostly off. The
  minimum length of 8 is a floor no policy can lower. Composition rules
  (uppercase, digit, symbol), reuse checks, and expiry are available and
  default to off — they make passwords more guessable rather than less,
  and are there for deployments audited against regimes that require them.
  If you have the choice, raise the minimum length instead.
- **An expired password cannot sign in at all.** It does not produce a
  session with a "must change" flag, because a flag is something an API
  client can ignore. The sign-in screen asks for a replacement in place.
- **Nothing crosses a tenant.** Users, organizations, audit entries, and
  settings all belong to one, and an administrator of one tenant cannot see
  or change another's — including by id, and including by sending a tenant
  header. There is no account anywhere that can.
- **Point liveness at `/api/v1/health` and readiness at `/api/v1/ready`,
  not both at the same one.** `/health` answers as long as the process is
  running and deliberately never touches the database: a database outage is
  not fixed by restarting instances, and a liveness probe that failed with
  it would turn one failing dependency into a fleet-wide restart loop at the
  worst possible moment. `/ready` is where the database check belongs —
  during an outage the right answer is "stop sending me traffic", not
  "restart me".

- **The audit log is kept forever unless you say otherwise.** Settings has a
  retention in days; 0, the default, keeps everything. Any other value must
  be at least 7, and entries past it are deleted permanently by the hourly
  cleanup with no copy kept. Nothing else in the product deletes an audit
  entry.

## Integrating a downstream system

A downstream application receives the user's token and calls:

- `GET /api/v1/users/me` — the user's profile and organization, which is
  everything needed to create or update a local record (requirements §3.8.2).
- `GET /api/v1/auth/permission-check` — whether this caller is an
  administrator, for gating the downstream system's own admin screens.

Both take the user's own bearer token. There are no service accounts or
machine-to-machine credentials in the MVP.

That path suits a system that already has the user's Portico token. A
system that needs to sign people in itself should use OpenID Connect
instead — point its library at the issuer and register it with
`portico client register`. See [federation.md](federation.md).

### When the names do not line up

A downstream system maps on the name it is given. If it reads `dept` and
Portico sends `department`, the field arrives and is discarded — which looks
from the other end like an account with nothing in it.

Which name goes out is configured per recipient rather than fixed in code, and
it covers all four: OIDC clients, SAML service providers, CAS services, and
webhook subscriptions. **Applications** or **Webhooks** → **Fields**. The same
screen is also how the twenty-five provisioned profile attributes get out at
all — they are stored, they arrive over SCIM, and by default they reach no
application. See [field-mappings.md](field-mappings.md).
