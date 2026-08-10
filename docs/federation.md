# Federation: OpenID Connect, SAML 2.0, and CAS

How another application signs people in through Portico, and what it can and
cannot expect once it has.

Three protocols, one set of accounts. Which one to use is decided by what the
application already speaks, not by anything here: a modern application uses
OpenID Connect, an enterprise product usually has a SAML integration written
years ago, and a university or a Java application often has CAS. All three
answer with the same facts under the same names — tenant, role, organization
— so an application migrated from one to another sees no change in what it
is told.

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
| Access token | JWT, 15 minutes |
| ID token | 15 minutes |
| Refresh token | 30 days, rotated on every use |
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

## Revocation, honestly

Three things end a session: signing out, changing a password, and an
administrator disabling the account. Each of them:

- invalidates every Portico session token immediately, and
- revokes every refresh token held by every relying party.

None of them can withdraw an **access token that has already been
issued**. That is not a gap in Portico; it is what issuing a self-verifying
token means. A resource server checks the signature and the expiry and never
calls back, which is the entire reason to federate rather than to proxy every
request. Two things bound it:

- access tokens live fifteen minutes, and
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
| Portico's own session | Ends immediately |
| OIDC refresh tokens | Revoked |
| OIDC access tokens | Cannot be withdrawn; 15 minutes, or introspect |
| SAML | Nothing to revoke — no server-side session exists, because there is no single logout. A service provider's own session outlives this entirely and ends on its own terms. |
| CAS | Nothing to revoke — a ticket lives a minute and is single use, and there is no ticket-granting ticket. A service's own session, like SAML's, is its own affair. |

The last two rows are worth reading twice before deploying. Ending a session
in Portico does not end the session an application created after it accepted
an assertion or a ticket; no identity provider can do that without a working
single-logout profile, which this version does not have.

## Known limitations in v0.1

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

Attributes use the OASIS X.500 names where such an agreement exists — `uid`,
`mail`, `displayName`, `telephoneNumber` — and Portico's own, unprefixed,
where it does not: `tenant_id`, `tenant_code`, `role`, `organization_id`,
`organization_name`.

Signature construction and verification are entirely
[crewjam/saml](https://github.com/crewjam/saml) and
[goxmldsig](https://github.com/russellhaering/goxmldsig), which is pinned
ahead of what crewjam resolves because it is the code the whole thing rests
on. Nothing in Portico builds or checks an XML signature; a hand-rolled one
is the most reliable way to ship a SAML implementation that accepts forged
assertions.

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
| Attributes | CAS 3.0 only, under the same names the other protocols use |

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

## Trying it locally

```bash
portico client register --id demo --name Demo --public \
  --redirect-uri http://127.0.0.1:9999/callback

curl -s http://localhost:8410/.well-known/openid-configuration | jq
```

The complete flow, driven by a real relying-party library, is in
[internal/server/federation_test.go](https://github.com/Paraview-RD/portico/blob/main/internal/server/federation_test.go) —
it is the most useful worked example in the repository, because it is the one
that has to keep passing.
