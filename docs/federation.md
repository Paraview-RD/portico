# Federation: OpenID Connect, SAML 2.0, and CAS

How another application signs people in through Portico, and what it can and
cannot expect once it has.

Three protocols, one set of accounts. Which one to use is decided by what the
application already speaks, not by anything here: a modern application uses
OpenID Connect, an enterprise product usually has a SAML integration written
years ago, and a university or a Java application often has CAS. All three
answer with the same facts — tenant, role, organization, and the person's
details — so an application migrated from one to another learns exactly as
much as it did before. The names differ, because each protocol has its own;
[One person, three sets of names](#one-person-three-sets-of-names) is the
table to map from.

New to these protocols? [SSO Protocols](sso-protocols.md) explains what each
one is, what happens during a sign-in, and which to choose — without
assuming prior knowledge.

The sections below are OpenID Connect first, then [SAML](#saml-20), then
[CAS](#cas).

## The short version

Portico is an OpenID Provider. Register your application — in the console
under **Applications**, or from the command line — point your OIDC library
at the issuer, and you are done: there is nothing Portico-specific to
write.

```bash
portico client register --id grafana --name "Grafana" \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

```
issuer:  https://id.example.com
```

Everything else — endpoints, the key set, which grants and scopes exist — is
in the discovery document at
`https://id.example.com/.well-known/openid-configuration`, which is what your
library reads at start-up.

## Issuers

Each tenant is its own issuer, with its own discovery document, its own key
set, and its own accounts:

```
https://id.example.com/t/<tenant-code>
```

The default tenant is additionally served at the root, so a deployment with
one tenant has the issuer people expect and never has to explain
multi-tenancy to an integrator:

```
https://id.example.com
```

The tenant is in the URL rather than in a claim because that is what makes a
token minted for one tenant unusable against another. A relying party checks
`iss` and fetches the key set that issuer names; both are things every OIDC
library already does. A shared issuer with a `tenant` claim would only be
safe if every integrator wrote extra code to check that claim, and none of
them will, because no standard library asks them to.

The two mounts of the default tenant are separate issuers over the same
accounts and the same keys. A token names whichever one it was obtained
from, and verifies against that one.

## What is implemented

| | |
|---|---|
| Grant | Authorization code, with PKCE required |
| PKCE method | `S256` only |
| Signing | RS256, with the public keys at the issuer's `/keys` |
| Access token | JWT. 15 minutes by default, settable 1–60 in **Settings** |
| ID token | Follows the access token |
| Refresh token | 30 days by default, settable 1–90; rotated on every use |
| Maximum session age | Off by default. Set it and a refresh chain ends that many days after the sign-in that began it, however diligently it is refreshed |
| Client authentication | `client_secret_basic`, `client_secret_post`, or none (public clients) |
| Scopes | `openid`, `profile`, `email`, `phone`, `offline_access` |
| Endpoints | discovery, authorize, token, userinfo, introspect, revoke, end_session, keys |

The discovery document says exactly this. The protocol library Portico is
built on fills three of those fields from its own defaults rather than from
the configuration — it would otherwise advertise the implicit flow, the
JWT-bearer grant, and a device-authorization endpoint that is not mounted —
so the document is corrected before it is published. A client configures
itself from that file once, before anybody is watching; anything untrue in
it fails later, somewhere else, with an error nobody can trace back to it.

Deliberately **not** implemented: the implicit and hybrid flows, the device
and client-credentials grants, dynamic client registration, front-channel
logout, DPoP, PAR, and `private_key_jwt` client authentication.

The implicit and hybrid flows put tokens in URLs, which is why OAuth 2.1
removes them. PKCE is required of confidential clients too, for the same
reason 2.1 requires it: a code intercepted between the browser and the
application is redeemable without it, and whether the client holds a secret
does not change that.

### There is no consent screen

Every client is vetted and registered by an administrator — from the console
or the command line, never by the application itself — so there is no
third party for a person to consent to; the applications are the
deployment's own. A person signing in to Portico for an application sees the
ordinary sign-in screen and is returned to the application; nothing asks them
to approve a scope. If Portico ever accepts clients it did not vet, that
changes, and it will be a deliberate change rather than an oversight.

## Registering an application

Two equivalent paths, both restricted to a tenant administrator and both
audited: the console's **Applications** screen, or the commands below. They
go through the same service, so the validation and the audit trail are
identical either way. The console additionally shows the endpoints to
configure at the other end, derived from the running deployment.

Dynamic client registration ([RFC 7591](https://www.rfc-editor.org/rfc/rfc7591))
is deliberately absent: that is registration by an anonymous caller, with no
administrator in the loop.

The command line remains the answer for a first deployment before anybody
has signed in, for scripting, and for when the console cannot be reached.

```bash
# A server-side application. The secret is printed once and stored hashed.
portico client register --id grafana --name Grafana \
  --redirect-uri https://grafana.example.com/login/generic_oauth

# A browser or mobile application, which cannot keep a secret. It gets none
# and is authenticated by PKCE alone.
portico client register --id console --name Console --public \
  --redirect-uri https://console.example.com/callback

portico client list
portico client disable --id grafana    # refuses new sign-ins; deletes nothing
```

Add `--tenant acme` to register in a tenant other than the default.

The default scopes are `openid profile email`. **A client that needs refresh
tokens must be registered with `--scope offline_access`** as well — a scope
the client was not registered for is dropped rather than refused, so the
symptom is a token response with no `refresh_token` in it and no error
anywhere:

```bash
portico client register --id grafana --name Grafana \
  --scope openid --scope profile --scope email --scope offline_access \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

Redirect URIs are matched exactly and are validated on registration:
wildcards, fragments, and non-loopback `http://` are all refused. Loopback
`http://` is allowed because a native application's redirect has nowhere else
to go.

### The picture on the tile

Optional, and the tile falls back to the first character of the name — which
is legible, needs no network, and cannot break. What a logo buys is
recognition: people find an application on a portal by its mark long before
they finish reading six names.

Three ways to supply one, and they end up in the same field:

| | |
|---|---|
| Upload | **PNG or JPEG**, at most 512 KiB and 1024 pixels a side. Stored in the database and served from `/t/<tenant>/logos/<id>` |
| A path on this server | Anything you have put there yourself, such as `/icons/wiki.svg` |
| An absolute `https` address | Fetched by the browser from wherever it points |

**An SVG cannot be uploaded**, and the refusal is not about the format being
unusual. An SVG is a document that can carry script, and an uploaded file is
served back from this server's own address — the address the administrative
console is on. Rendered through `<img>` a browser will not run that script,
which is why an SVG *path* is still accepted: a file an operator put under
`/icons` is one they chose. But a file that arrived through a web form can also
be opened directly, in a tab, where it is a page with this origin's cookies.
The safety of the `<img>` case is a property of one component's rendering, and a
stored blob that is only safe because of how something happens to render it is a
trap for whoever changes that component.

The third form has a consequence worth knowing before you use it: the
`Content-Security-Policy` this server sends allows images from itself,
`data:`, and `https:` — so **an absolute `https` address renders**. A plain
`http` address is still accepted on registration, for an intranet
application no public certificate could ever cover, but **will not render**:
the policy admits `https:`, not `http:`. The first two forms are same-origin
and unaffected either way.

Uploads that no application ends up referencing are removed a day later. The
upload has to happen before the form is saved, so a cancelled form leaves a
file behind, and replacing a logo leaves the one it replaced.

## Claims

Beyond the standard ones, every token carries what a downstream system needs
in order to place the person:

| Claim | Meaning |
|---|---|
| `tenant_id`, `tenant_code` | Which tenant the account belongs to |
| `role` | `SUPER_ADMIN` or `USER` |
| `organization_id`, `organization_name` | Present when the account has an organization |

These appear in the ID token, the access token, and the userinfo response
alike. A relying party reads identity from the ID token and a resource
server from the access token; a claim present in only one of them is a claim
half the integrations cannot see.

`email_verified` and `phone_number_verified` are always `false`. This version
never asks anybody to prove an address, and a provider that claimed otherwise
would be lying to a relying party that might act on it.

## Signing in through somebody else's provider

Everything above is Portico as the issuer. This is the other direction: an
OpenID Provider somebody else runs, trusted to say who a person is.

Configure one under **Identity providers**, beside applications and webhooks
— the same menu, because all four are a connection to another system, and
this is the one where the connection runs inward. You supply the issuer, a
client id and secret registered at that provider, and the redirect URI the
screen shows you — copy it exactly, since the provider matches it character
for character. The issuer is contacted when you save, so a configuration
that cannot be discovered is refused at the form rather than at somebody's
sign-in three days later.

The redirect URI is a console address — `/external/callback`, or
`/t/<code>/external/callback` for a tenant other than the default — and not
the API endpoint that completes the sign-in. Coming back from a provider is
a top-level navigation, so whatever answers it is what a person is looking
at; the endpoint answers JSON. The console takes the landing, reads the
`state` and `code` out of its own address, and spends them on that endpoint
itself, which also keeps the issued session out of a URL.

**It must be a public HTTPS address.** The same rules a webhook destination
follows, and for the same reason: a tenant administrator types it and this
server fetches it.

### An external identity does not create an account

A first-time arrival whose identity is not linked to anything here is
refused, and told to sign in with a password and link it from their profile.
This version never creates an account from an external sign-in — self-service
registration and its optional address confirmation are the two switches that
decide who may get an account, and a provider button that quietly made
accounts would go around both.

So the sequence for a person is: sign in as they already do, link the
provider from **My profile**, and from then on the button works.

### The one switch that can hand an account to a stranger

**Trust this provider's verified email** lets a first-time arrival be matched
to an existing account by address, instead of being refused. It is off, and
it should stay off unless you run the provider or know how it verifies.

An identity provider that does not verify addresses lets anybody register
`ceo@your-company.example` and arrive here holding a token that says so. If
an address alone reaches an existing account, that *is* the account.

With it on, the address is consulted exactly once: the identity is linked on
the way through, and from the next sign-in the account is reached by the
provider's subject. An address that later changes at the provider — or is
reassigned to somebody else — cannot repoint an existing link.

### What is checked on the way back

The `state` is consumed by the same statement that reads it, so a replayed
callback finds nothing. The `nonce` in the ID token is compared against the
one stored when the sign-in left. The signature, `iss`, `aud` and expiry are
checked against the provider's published keys. Every way of failing to name a
live request is one error, because a caller able to tell them apart could
learn which states existed.

Whether the journey is a sign-in or a link is decided when it starts and
remembered server-side — never read from what comes back. A callback that
could say which it was would be one a crafted link could lie in.

### WeChat and DingTalk, which are not OpenID Connect

Both are supported, and neither is configured the way everything above is.
OpenID Connect's discovery document is what makes a provider a form to fill
in: give Portico an issuer and it learns the endpoints, the keys, and how to
check what comes back. These two publish no such document, so each is an
adapter compiled into the server — which is why adding a third vendor is a
release rather than a setting.

Pick the kind first; the form changes with it.

| | |
|---|---|
| **WeChat** | The Open Platform's **website application** — the QR code somebody scans. Not the official-account web authorization, which only works inside WeChat's own browser and so cannot sign anybody in to a console; not WeCom, which is a different product. You supply the **AppID** and **AppSecret** under those names, because that is what WeChat's own console calls them. |
| **DingTalk** | The login API it has had since 2022. `scope=openid` is DingTalk's spelling, not a promise: nothing signed comes back. |

There is no issuer field for either, and that is deliberate rather than a
simplification. The issuer is the namespace every stored subject lives in —
identity here is the pair `(issuer, subject)` — so it is a constant this
server chooses. If two tenants could type their own, the same person would be
two identities.

Nor is a saved configuration verified. An OIDC issuer is contacted before the
row is written, which is what makes a typo fail at the form; there is nothing
equivalent to ask these two, because what could be wrong is the credential
and neither vendor offers a way to check one without a person completing a
sign-in. **So for these, the first sign-in is the test.**

#### What these two do not have

Worth stating plainly, because the same button and the same callback serve
them and the providers above:

- **No ID token.** Nothing in either exchange is signed anywhere. The token
  request therefore goes server-to-server over TLS with the client secret,
  and the browser is trusted with nothing but the authorization code.
- **No nonce**, because there is no token to carry one.
- **No PKCE.** Neither implements it.

What ties a callback to a request this server started is the single-use
`state`, and for these two that is the whole of it — where an OIDC provider
is held to three separate checks. That is what those providers offer rather
than something given up here.

**WeChat returns no email address at all**, so the switch above cannot do
anything for it and the form does not offer it. DingTalk returns one and does
not claim it is proved, which Portico records as unverified.

#### One thing to decide before you have users

WeChat identifies people by a `unionid` where the application is bound to an
Open Platform account and an `openid` where it is not. Portico prefers the
first and records which it used, because the difference matters later: if an
application **gains** a unionid it did not have, every identity bound under
an openid stops matching and those people are told their account is not
linked. That is a migration, and it is writable only because the rows say
which key they used — but it is much cheaper to bind the application to an
Open Platform account before anybody links, than after.

### Removing a provider

**Disable** takes the button off the sign-in screen and leaves every link in
place; it is the control for an outage. **Delete** unlinks everybody who
arrived through it, and the audit entry records how many.

Nobody is locked out either way: every account here still has a password, so
a link is a convenience rather than the only way in.


## Revocation, honestly

Three things end a session: signing out, changing a password, and an
administrator disabling the account. All three revoke **every refresh token
held by every relying party**, immediately. Where they differ is how much of
Portico's own they end:

| | Portico's own sessions | Relying parties' refresh tokens |
|---|---|---|
| Signing out | the one signing out | all |
| **Sign out everywhere** | all | all |
| Changing a password | all | all |
| An administrator disabling the account | all | all |

Signing out ends the session doing it and leaves the one on your phone alone,
because that is what somebody closing a browser expects; ending them all is a
deliberate act with a button of its own. The relying parties are the other
way round in every row — on a single sign-on system "sign out" is read as
signing out of the things you signed in to, and doing less than that is the
surprise that matters.

None of them can withdraw an **access token that has already been
issued**. That is not a gap in Portico; it is what issuing a self-verifying
token means. A resource server checks the signature and the expiry and never
calls back, which is the entire reason to federate rather than to proxy every
request. Two things bound it:

- an access token's lifetime, fifteen minutes by default and never more than
  an hour — the ceiling exists because this expiry is the only thing bounding
  a permission that has been withdrawn; see SECURITY.md — and
- the issuer's `/oauth/introspect` endpoint answers `active: false` for a
  disabled account straight away, for a resource server that needs the answer
  sooner than expiry.

Signing out of Portico revoking the relying parties' refresh tokens is a
choice, not an obligation. It would be defensible to leave them alone — that
is what an application's own `end_session` endpoint is for. Portico does not,
because on a single sign-on system "sign out" is read by the person clicking
it as signing out of the things they signed in to, and doing less than that
is the surprise that matters.

### Refresh token rotation

Every refresh mints a new token and spends the old one. Presenting a spent
token means a copy leaked — the legitimate holder would have the replacement
— so the response is to revoke the whole chain rather than to fail the one
call, because which link leaked is unknowable.

A refresh also re-checks that the account is still enabled, so the thirty-day
lifetime is an upper bound and not a promise.

## Signing keys

Each tenant has its own RSA key, generated on first use rather than at
start-up: most tenants never federate, and a keygen apiece would be a cost
paid for nothing.

```bash
portico client rotate-key --tenant acme
```

Rotation issues a new key and retires the old one. The retired key stays in
the published key set for 24 hours, so tokens signed with it keep verifying
until they have all expired, and is then deleted.

## What revocation reaches, per protocol

| | Sign-out, password change, disable |
|---|---|
| Portico's own session | Ends immediately — the one signing out, or all of them for the other two; see the table above |
| OIDC refresh tokens | Revoked |
| OIDC access tokens | Cannot be withdrawn; expiry (15 minutes by default, at most an hour) or introspect |
| SAML | Nothing to revoke — no server-side session exists, because there is no single logout. A service provider's own session outlives this entirely and ends on its own terms. |
| CAS | Nothing to revoke — a ticket lives a minute and is single use, and there is no ticket-granting ticket. A service's own session, like SAML's, is its own affair. |
| An external identity provider | **Nothing to revoke, and nothing reaches them.** Signing out of Portico does not sign anybody out of Google or Entra; the next click on that button signs them straight back in, because the provider still holds its own session. That is not a gap here — it is what federation means in this direction, and it is the same fact as the two rows above, seen from the other side. |

The last two rows are worth reading twice before deploying. Ending a session
in Portico does not end the session an application created after it accepted
an assertion or a ticket; no identity provider can do that without a working
single-logout profile, which this version does not have.

## Known limitations

- **Expired refresh tokens outlive their expiry by thirty days.** The hourly
  sweep removes a rotation chain only once every token in it is both expired
  and thirty days past it, and only whole chains. Deleting a row the day it
  expires would break reuse detection: a token that is expired *and* already
  spent still triggers revocation of the entire chain when it is presented,
  which is how a stolen refresh token is caught. The row is the evidence, so
  it outlives the credential.
- **Access tokens cannot be revoked.** See above; the `/revoke` endpoint
  accepts them and answers successfully, as RFC 7009 requires, but only
  refresh tokens are actually revoked.
- **No consent screen**, as described above.
- **No `private_key_jwt`**, so a client that can only authenticate that way
  cannot be registered.
- **No single logout for SAML or CAS**, and therefore no way to end a
  session an application created for itself. See the table above.
- **No SAML identity-provider-initiated sign-on**, and no proxy tickets for
  CAS.

## SAML 2.0

Portico is a SAML identity provider. Hand the service provider Portico's
metadata, register the service provider's metadata with Portico, and that is
the whole exchange — the documents carry everything either side needs.

Either side of that exchange can be done in the console under
**Applications → SAML 2.0**, which accepts an uploaded or pasted metadata
document and offers Portico's own metadata and certificate for copying.
Portico never fetches metadata from a URL you supply: that would make the
server issue requests to an address a caller names, which is a server-side
request forgery against whatever else it can reach.

```bash
# Portico's metadata, for the service provider's configuration:
#   {PORTICO_PUBLIC_URL}/saml/metadata           the default tenant
#   {PORTICO_PUBLIC_URL}/t/<code>/saml/metadata  any other

# The service provider's, for Portico's:
portico sp register --metadata ./sp-metadata.xml --name "Confluence"
portico sp list
portico sp disable --entity-id https://confluence.example.com/saml
```

`--metadata` takes a file or an `https://` URL. Plain `http` is refused: the
document names where assertions get delivered, so anybody on the path could
redirect them.

### What is implemented

| | |
|---|---|
| Profile | Web browser SSO, service-provider-initiated |
| Bindings | HTTP-Redirect and HTTP-POST inbound, HTTP-POST outbound |
| Signing | RSA-SHA256 over the response |
| Encryption | The assertion is encrypted whenever the service provider publishes an encryption key in its metadata |
| NameID | Persistent, and it is the account id |
| Assertion lifetime | 5 minutes |
| Sign-in deadline | 15 minutes |

The name identifier is the account **id**, not the username. A username can
be changed by an administrator, and a service provider keying its local
record on one would quietly create a second account for the same person the
day it was. The username is in the `uid` attribute for display.

Attributes use the OASIS X.500 names where such an agreement exists, and
Portico's own where it does not. Each is sent under two names, and the
distinction matters when you configure a service provider: the friendly name
is a label for a person reading the assertion, and the **Name** is the string
the mapping keys on.

| SAML attribute | Attribute Name a service provider maps on |
|---|---|
| `uid` | `urn:oid:0.9.2342.19200300.100.1.1` |
| `displayName` | `urn:oid:2.16.840.1.113730.3.1.241` |
| `cn` | `urn:oid:2.5.4.3` |
| `mail` | `urn:oid:0.9.2342.19200300.100.1.3` |
| `telephoneNumber` | `urn:oid:2.5.4.20` |
| `tenantId` | `tenant_id` |
| `tenantCode` | `tenant_code` |
| `role` | `role` |
| `organizationId` | `organization_id` |
| `organizationName` | `organization_name` |
| `urn:oasis:names:tc:SAML:attribute:subject-id` | `urn:oasis:names:tc:SAML:attribute:subject-id` |

The last has no friendly name because the profile that defines it does not
give one. It carries the account id, the same value as the name identifier,
and is there for a service provider that follows the subject identifier
profile rather than reading the NameID.

That list is the whole of it, and each name appears exactly once. `cn` and
`displayName` carry the same value, which is not an accident: they are the
two names service providers actually map for a person's name, and they
disagree about which.

Signature construction and verification are entirely
[crewjam/saml](https://github.com/crewjam/saml) and
[goxmldsig](https://github.com/russellhaering/goxmldsig), which is pinned
ahead of what crewjam resolves because it is the code the whole thing rests
on. Nothing in Portico builds or checks an XML signature; a hand-rolled one
is the most reliable way to ship a SAML implementation that accepts forged
assertions.

### How a sign-in resumes

An authentication request arrives as a plain browser navigation with no
credential on it, so Portico parks the request, sends the browser to its own
sign-in screen, and finishes the protocol afterwards. Three addresses are
involved and they are not equally sensitive:

1. `/t/<code>/saml/sso` — where the service provider sends the browser.
2. `/login?saml_request=<id>` — Portico's sign-in screen, told which request
   it is completing. **The id is in this URL**, which means it is in browser
   history and in any proxy log along the way.
3. `/t/<code>/saml/sso/callback` — mints the assertion and posts it onward.

The third is where the assertion is created, and it cannot ask for a
credential: a top-level navigation has nowhere to put one. So it must
recognize the browser some other way, and the id is not it — an id is
something several logs have.

Completing a request therefore issues a one-time secret. It is generated
inside `POST /api/v1/saml/authenticate`, which is the authenticated call the
console makes once somebody has signed in, returned only in that response,
and appended to the callback address the console then navigates to. The
callback requires it and compares in constant time; the stored copy is a
SHA-256, on the same terms as an authorization code.

An id recovered from a log is now just an id. A failed attempt does not
consume the request either — deleting it on a mismatch would let anybody
holding a leaked id destroy a sign-in in progress, which trades a hard
attack for an easy one.

This has no counterpart in the OpenID Connect flow, and the difference is
worth being clear about: that callback hands its code to the relying party's
*registered* address, so holding its id gets an attacker nothing. A SAML
assertion is handed to the browser that asked, to be posted onward — which
is what makes the caller's identity matter here and not there.

### Deliberately not implemented

- **Identity-provider-initiated sign-on.** There is no request to correlate
  the assertion with, which makes a stolen assertion replayed into a login
  indistinguishable from the real thing.
- **Single logout.** The profile requires the identity provider to reach
  every service provider a person signed in to, in the browser, and to cope
  with any of them being unreachable. One that half works is worse than
  none, because it reports having ended sessions it did not. The metadata
  says so rather than advertising an endpoint that 404s.
- **Signed authentication requests**, artifact resolution, and attribute
  queries.

### Certificates

Each tenant has its own certificate, generated on first use, valid for ten
years.

```bash
portico sp certificate                  # print it, for a service provider
portico sp rotate-certificate           # generate a new one
```

Rotation retires the old certificate and deletes nothing. This is the one
place where SAML and OpenID Connect differ in kind rather than in detail: a
relying party refetches a key set, so an OIDC key can be retired and dropped
a day later, whereas a service provider has the certificate typed into its
own configuration and no way to learn of a new one. **Every service provider
must be reconfigured by hand before it will accept another assertion**, and
until each one has been, the previous certificate is what an operator needs
to be able to look up. That is why the two live in separate tables and why
`sp rotate-certificate` is a separate command from `client rotate-key`.

## CAS

Portico speaks CAS 2.0 and 3.0. Register the service in the console under
**Applications → CAS**, or with `portico cas register`, and point the
client's CAS server URL at the part before `/login`:

```
{PORTICO_PUBLIC_URL}/cas             the default tenant
{PORTICO_PUBLIC_URL}/t/<code>/cas    any other
```

```bash
portico cas register --url https://wiki.example.com/ --name Wiki
portico cas list
portico cas disable --url https://wiki.example.com/
```

`--url` is a prefix, not a pattern. A `service` parameter matches when it
begins with the registered value **and the prefix ends at a path boundary**,
so `https://app.example.com/` never covers
`https://app.example.com.somewhere-else.test`. There are no wildcards,
registration normalizes a trailing separator on, and query strings,
fragments, and plain `http` over a network are refused — a service URL is
CAS's redirect URI, and it gets the same treatment.

| | |
|---|---|
| Endpoints | `/cas/login`, `/cas/logout`, `/cas/serviceValidate`, `/cas/p3/serviceValidate` |
| Ticket | `ST-` prefix, single use, one minute |
| Attributes | CAS 3.0 only, under CAS's own names — see [One person, three sets of names](#one-person-three-sets-of-names) |

A ticket is bound to the service it was issued for: presenting it at another
service's validation would otherwise let a service that legitimately received
one impersonate that person elsewhere. Validation always answers `200`, even
for a failure, because that is what the specification says and several
clients stop reading on anything else and report a transport error instead
of the reason.

**There is no ticket-granting ticket.** CAS puts one in a long-lived cookie
so a browser can obtain further tickets without signing in again; Portico
already has a session for that, and a second long-lived credential would be
a third thing to revoke on sign-out, password change, and disable. Riding on
the existing session means those three already cover it.

`/cas/logout` redirects to Portico's own sign-in screen, which is where the
session actually is — a plain navigation cannot reach a token the web
application holds, so the application signs out on arrival. The `service`
parameter is deliberately not followed: the specification makes that
optional and warns about it, and an endpoint that redirects wherever a
caller names is an open redirect wearing a protocol's clothes.

Not implemented: proxy tickets, and CAS 1.0 `/validate`, whose bare
`yes\n<user>\n` carries no attributes and no way to say why a ticket failed.

## One person, three sets of names

The three protocols carry the same facts under three sets of names, and this
is the table to map from. They are not the same names and cannot be: OpenID
Connect's are its specification's, and a service provider or a CAS client
maps by the name it is given, so a house style would mean every integration
writing a custom mapping for facts every directory already publishes.

| Fact | OpenID Connect claim | SAML attribute | CAS 3.0 attribute |
|---|---|---|---|
| Account id | `sub` | the NameID, and `urn:oasis:names:tc:SAML:attribute:subject-id` | not sent |
| Username | `preferred_username` | `uid` | the cas:user element, not an attribute |
| Display name | `name` | `displayName`, `cn` | `displayName` |
| Email | `email` | `mail` | `email` |
| Phone | `phone_number` | `telephoneNumber` | `phone` |
| Whether either was proved | `email_verified`, `phone_number_verified` | not sent | not sent |
| Tenant | `tenant_id`, `tenant_code` | `tenantId`, `tenantCode` | `tenant_id`, `tenant_code` |
| Role | `role` | `role` | `role` |
| Organization | `organization_id`, `organization_name` | `organizationId`, `organizationName` | `organization_id`, `organization_name` |
| Last changed | `updated_at` | not sent | not sent |

The SAML column is the friendly name. The Name a service provider maps on is
in the [table above](#what-is-implemented_1).

A name arrives only when the account has the fact behind it: no email
address, no `mail`; no organization, no `organization_id`. Nothing is sent
empty, so a service provider mapping a field it never receives should look at
the account before the mapping.

An OpenID Connect claim also needs its scope to have been asked for —
`phone_number` arrives for `phone` and not otherwise — which is the more
common reason for a claim to be missing than anything on this page.

This table is checked rather than maintained.
`TestEachProtocolSendsTheNamesTheManualLists` signs in over all three
protocols with an account that has every fact, and compares what comes back
against what is written here — in both directions, and against both
translations of this page. It exists because this section previously said CAS
used the same names as the other two, which it never has.

## Local testing

Register an application and ask the server what it advertises:

```bash
portico client register --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

curl -s http://localhost:8410/.well-known/openid-configuration | jq
```

`examples/mock-sp` is the other half: a relying party with a browser
interface, so that a sign-in can be watched rather than only asserted.

```bash
go run ./examples/mock-sp
```

Then open <http://localhost:8413> and choose OpenID Connect. It leaves for
Portico's sign-in screen and comes back to a page showing the ID token's
claims and the userinfo response side by side.

Three details decide whether that works first time, and each fails in a way
that looks like something else:

- **The redirect URI must match to the character.** The one registered above,
  the one `mock-sp` sends, and the address the browser reaches are the same
  string or the sign-in ends in `invalid_request`. `localhost` and
  `127.0.0.1` are different strings even though they are the same host.
- **`PORTICO_PUBLIC_URL` must be the address the browser uses.** It is the
  issuer, discovery is built from it, and a relying party checks that what
  came back matches what it asked for. Pointing at a server whose public URL
  says something else fails during discovery, at start-up.
- **The scopes requested must be scopes the client was registered with.**
  Both default to `openid profile email`. Ask for `offline_access` — which is
  what gets you a refresh token — and register the client with it too.

### The other two protocols

`mock-sp` speaks all three. SAML and CAS each need one registration. The CAS
one could be run at any time — it is a URL prefix, knowable in advance — but
SAML's takes a document the program generates, so start it once first:

```bash
go run ./examples/mock-sp        # writes .mock-sp/, prints the two commands

portico sp register --metadata .mock-sp/saml-metadata.xml --name "Mock SP"
portico cas register --url http://localhost:8413/cas/ --name "Mock SP"
```

The SAML and CAS pages work from that moment — no restart. Registration is
state on Portico's side, and `mock-sp` holds none of it.

Step by step, with the traps, in
[`examples/mock-sp/README.md`](https://github.com/Paraview-RD/portico/blob/main/examples/mock-sp/README.md).

`sp register` takes a **file** here rather than the metadata URL the program
serves, because `--metadata` refuses plain `http`: that document names where
assertions get delivered, so anybody on the path could point them elsewhere.
The key behind it is kept in `.mock-sp/` and reused across runs — Portico
encrypts an assertion to whatever encryption key the registered metadata
published, so a program that generated a fresh key each start would have to
be re-registered each time, and the symptom of forgetting would be a
decryption failure rather than anything naming the cause.

The CAS registration is a **prefix**, and it has to cover the service URL the
program sends: `http://localhost:8413/cas/` covers
`http://localhost:8413/cas/callback`. Watch the ticket on the page at the
end, then reload — it is refused, because a service ticket is good for one
validation.

For another tenant, give the issuer its path and register everything there:

```bash
portico client register --tenant acme --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

go run ./examples/mock-sp --issuer http://localhost:8410/t/acme
```

Each protocol is set up independently, so one that cannot start says so on
the home page and leaves the other two working. It is a development tool and
not part of a deployment — nothing in [integrations.md](integrations.md)
changes by running it, because it makes no connection Portico did not already
make.

The complete flow, driven by a real relying-party library, is in
[internal/server/federation_test.go](https://github.com/Paraview-RD/portico/blob/main/internal/server/federation_test.go) —
it is the most useful worked example in the repository, because it is the one
that has to keep passing.
