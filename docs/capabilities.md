# Capabilities

One page, for the question that comes before any of the others: *does this
do what we need?*

Everything here is checked against the code in this repository rather than
against a plan. The last section is the one worth reading twice — a list
that only says what a thing supports gets read as though everything else is
supported too.

## Single sign-on, as the provider

Four protocols, all against the same accounts, all per tenant.

| | Supported | Not |
|---|---|---|
| **OAuth 2.1 / OpenID Connect 1.0** | Authorization code + PKCE, refresh tokens, discovery, userinfo, introspection, revocation, RP-initiated logout, JWKS | Implicit and hybrid flows — removed by OAuth 2.1 because they put tokens in URLs. No device flow, no client credentials. |
| **SAML 2.0** | Sign-on begun by the service provider, published metadata, signed assertions (crewjam/saml — nothing here hand-rolls XML signing), attribute release through field mappings | **No single logout.** No artifact binding. |
| **CAS** | 2.0 and 3.0 (`/cas/serviceValidate`, `/cas/p3/serviceValidate`), login and logout | Not CAS 1.0, which answers with a bare `yes`/`no` and cannot say why a ticket failed. **No proxy tickets.** No single logout. |
| **Endpoints** | `/.well-known/openid-configuration`, `/authorize`, `/oauth/token`, `/userinfo`, `/oauth/introspect`, `/revoke`, `/end_session`, `/keys`, `/saml/metadata`, `/saml/sso`, `/cas/login`, `/cas/serviceValidate` | |

Each is also available under `/t/<tenant>/…`, which is how one deployment
serves several organizations with separate issuers.

## Ways to sign in

| | |
|---|---|
| **Password** | The identifier may be a **username, an email address, or a phone number** — the server works out which. All three are unique within a tenant, so which one somebody typed never changes who they are. |
| **Through somebody else's OpenID Provider** | Any provider with a discovery document: Google, Microsoft Entra ID, Okta, Auth0, Keycloak, GitLab, Authentik. Configured per tenant, and it **never creates accounts** — see below. |
| **WeChat and DingTalk** | Supported. Neither speaks OpenID Connect, so each is an adapter in the server rather than a configuration entry — WeChat through the Open Platform's website application, the QR code somebody scans. |
| **Multi-factor** | **Not implemented.** Planned for V0.3, TOTP only. |

### No implicit account creation

A first-time arrival whose identity is not linked to anything here is
refused and told to sign in with a password and link it from their profile.
Self-service registration and its optional address confirmation are the two
switches that decide who may have an account, and a provider button that
quietly made accounts would go around both.

The exception is per provider and off by default: **trust verified
addresses** lets a first sign-in link by email address, which delegates that
decision to whoever runs the provider.

## Where accounts come from

| | Direction | Who holds a credential |
|---|---|---|
| Created in the console or the API | — | — |
| **Self-registration** | inbound | optional email confirmation |
| **LDAP / Active Directory** | Portico pulls | Portico stores a bind password |
| **SCIM 2.0** | your directory pushes | your directory holds a token |
| **Spreadsheet import / export** | both | — |
| **External identity linking** | inbound | — |

SCIM serves `/Users`, `/Groups`, `/ServiceProviderConfig`, `/ResourceTypes`
and `/Schemas`. LDAP reconciles on the directory's own stable identifier, so
a rename stays a rename.

Accounts are **disabled, never deleted** — the audit trail points at them.

## What goes out

| | |
|---|---|
| **Webhooks** | HMAC-SHA256 signed, timestamp inside the signature, exponential-backoff retries, delivery history, secret rotation with a 24-hour dual-signature overlap, custom headers, and **full snapshots** so a new subscriber can be sent everything that already exists. |
| **Field mappings** | Rename, suppress or add attributes per recipient — for OIDC clients, SAML service providers, CAS services and webhooks alike. |
| **Audit log** | Sign-ins, operations, authentication and registration, with a per-tenant retention period. |
| **Metrics** | Prometheus, on a separate port, off unless configured. |

## Shape of a deployment

- **One Go binary.** The console and the manual are compiled in; there is no
  separate front end to host.
- **PostgreSQL**, and nothing else. No Redis, no message broker, no object
  store.
- **Multi-tenant**, with isolation enforced in the query layer rather than
  by reviewer discipline. Tenants are created from the command line, so no
  role exists that can see across all of them.
- **Two roles**: `SUPER_ADMIN` and `USER`. Fixed.
- **English and 简体中文**, in the console and in this manual.

[Running in Production](deployment.md) covers replicas, upgrades and
probes.

## Deliberate omissions

The part of this page most worth reading. None of these is an oversight;
each is a decision, and most are recorded with their reasoning elsewhere in
this manual.

| | |
|---|---|
| **Multi-factor authentication** | Planned for V0.3, TOTP only. Not SMS — NIST has downgraded SMS one-time codes, and this project has no SMS provider to send them with anyway. |
| **WeChat Work, QQ, Weibo, Feishu** | Each is a different API again. WeChat and DingTalk are supported; these are not, and adding one is a file rather than a row in a table. |
| **GitHub sign-in** | GitHub is OAuth 2 without OpenID Connect, so it needs an adapter rather than a configuration entry. |
| **Fine-grained permissions** | Two fixed roles. No role builder, no per-application assignment, no menu-level control. Group membership grants nothing. |
| **API keys** | The SCIM credential is a tenant-scoped token for `/scim/v2` and nothing else. There is no general-purpose API key system. |
| **SMS** | The interface exists; no provider is implemented, so SMS password recovery reports itself unavailable rather than silently failing. |
| **Password recovery without a contact address** | An account with neither an email address nor a phone number cannot self-recover. An administrator resets it. |
| **Deleting anything the audit trail points at** | Accounts, organizations, applications and tenants are **disabled, not deleted**. What can be deleted is what nothing in the log refers to: a group, a session, a webhook subscription, a provisioning credential, a tenant-defined attribute, an identity-provider registration. **There is no way to remove a tenant and its data at all.** |
| **Single logout, for SAML or CAS** | Signing out of Portico does not sign anybody out of a service provider that already has its own session, and there is no way to make it. The same is true in the other direction for an external identity provider: the next click on that button signs them straight back in. |
| **Read replicas, sharding, partitioning** | One PostgreSQL is the assumption. |
| **Rate limiting as your only defence** | The built-in sign-in limiter counts per process and is a floor; the real limit belongs at the reverse proxy. |
| **An internal-network external IdP** | An issuer must be a public HTTPS address. The SSRF guard that enforces it is not configurable. |

## Version

This page describes what is in the repository today, which is 0.2 in
progress. The tagged release is v0.1.0. Every Portico serves its own copy of
this manual at `/docs`, so the copy inside a running instance describes that
instance.
