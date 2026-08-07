# Federation: OAuth 2.1 and OpenID Connect

How another application signs people in through Portico, and what it can and
cannot expect once it has.

## The short version

Portico is an OpenID Provider. Register your application from the command
line, point your OIDC library at the issuer, and you are done — there is
nothing Portico-specific to write.

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

Every client is registered out of band by an administrator, so there is no
third party for a person to consent to — the applications are the
deployment's own. A person signing in to Portico for an application sees the
ordinary sign-in screen and is returned to the application; nothing asks them
to approve a scope. If Portico ever accepts clients it did not vet, that
changes, and it will be a deliberate change rather than an oversight.

## Registering an application

There is no API for this, on purpose: a client registration decides who may
ask Portico for tokens about its users, which is an administrative act of the
same weight as creating a tenant, and this version has no role that could be
authorized to perform it over HTTP.

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

Neither of them can withdraw an **access token that has already been
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

## Known limitations in v0.1

- **Expired refresh tokens are never deleted.** They stop working — expiry
  and revocation are both checked when one is presented — but the rows
  accumulate. Abandoned authorization requests *are* swept hourly; refresh
  tokens are not, and a busy deployment will want to prune them out of band
  until this is fixed.
- **Access tokens cannot be revoked.** See above; the `/revoke` endpoint
  accepts them and answers successfully, as RFC 7009 requires, but only
  refresh tokens are actually revoked.
- **No consent screen**, as described above.
- **No `private_key_jwt`**, so a client that can only authenticate that way
  cannot be registered.

## Trying it locally

```bash
portico client register --id demo --name Demo --public \
  --redirect-uri http://127.0.0.1:9999/callback

curl -s http://localhost:8410/.well-known/openid-configuration | jq
```

The complete flow, driven by a real relying-party library, is in
[internal/server/federation_test.go](../internal/server/federation_test.go) —
it is the most useful worked example in the repository, because it is the one
that has to keep passing.
