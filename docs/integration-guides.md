# Integration Guides

Step-by-step configurations for common applications. Each guide assumes
Portico is already running and you have administrator access to the tenant
you are integrating.

**Before any integration:** fetch the discovery document to confirm your
issuer URL and copy the endpoint addresses into your application's config.

```
GET https://<host>/.well-known/openid-configuration          ← default tenant
GET https://<host>/t/<tenant-code>/.well-known/openid-configuration
```

---

## Any OIDC application

This is the general recipe. If your application's docs say "OpenID Connect"
or "OAuth 2.0 / OIDC", follow these steps.

### 1. Register the application in Portico

In the Portico console: **Applications → New application**.

| Field | What to enter |
|---|---|
| Client ID | A short identifier, e.g. `grafana` or `myapp` |
| Client type | **Confidential** if the app runs server-side and can keep a secret. **Public** if it is a browser or mobile app. |
| Redirect URI | The callback URL from your application's OIDC docs, e.g. `https://grafana.example.com/login/generic_oauth` |

After saving, copy the **client secret** immediately — it is shown once.

Or from the command line:

```bash
portico client register \
  --id myapp \
  --name "My App" \
  --redirect-uri https://myapp.example.com/auth/callback
```

### 2. Configure the application

Give your application these four values:

| Value | Where to find it |
|---|---|
| Issuer URL | `https://<host>` for the default tenant, or `https://<host>/t/<code>` |
| Client ID | The ID you chose in step 1 |
| Client secret | Shown once after registering (confidential clients only) |
| Scopes | `openid profile email` — add `phone` or `offline_access` as needed |

Most OIDC libraries auto-discover the token, userinfo, and JWKS endpoints
from the issuer URL. If yours asks for them explicitly:

| Endpoint | URL |
|---|---|
| Authorization | `<issuer>/authorize` |
| Token | `<issuer>/oauth/token` |
| Userinfo | `<issuer>/userinfo` |
| JWKS | `<issuer>/keys` |
| End session | `<issuer>/end_session` |

### 3. Enable PKCE

Portico implements OAuth 2.1, which requires PKCE for every client type.
Find the PKCE option in your application's OIDC configuration and set it
to `S256`. If the library does not expose a PKCE option, it likely handles
it automatically — check by initiating a sign-in and confirming the
authorization URL contains `code_challenge`.

If no PKCE setting exists and the library does not add it automatically,
the authorization request will be rejected. See
[Troubleshooting → PKCE required](troubleshooting.md#pkce-required).

### 4. Test

Initiate a sign-in from your application. After authenticating in Portico,
you should be redirected back and signed in. If something goes wrong, the
redirect URI receives `error` and `error_description` parameters that name
the problem — see [Troubleshooting](troubleshooting.md).

---

## Grafana

Grafana supports OIDC through its generic OAuth provider.

### Register in Portico

```bash
portico client register \
  --id grafana \
  --name "Grafana" \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

Copy the client secret.

### Configure Grafana

In `grafana.ini` or the equivalent environment variables:

```ini
[auth.generic_oauth]
enabled = true
name = Portico
allow_sign_up = true
client_id = grafana
client_secret = <secret from above>
scopes = openid profile email
auth_url = https://<host>/authorize
token_url = https://<host>/oauth/token
api_url = https://<host>/userinfo
use_pkce = true

# Map Portico's claims to Grafana's fields
login_attribute_path = preferred_username
name_attribute_path = name
email_attribute_path = email

# Optional: restrict to specific roles
# role_attribute_path = contains(roles[*], 'Admin') && 'Admin' || 'Viewer'
```

For a named tenant, replace `https://<host>` with
`https://<host>/t/<tenant-code>` in all three URLs.

**With environment variables** (Docker / Kubernetes):

```bash
GF_AUTH_GENERIC_OAUTH_ENABLED=true
GF_AUTH_GENERIC_OAUTH_NAME=Portico
GF_AUTH_GENERIC_OAUTH_CLIENT_ID=grafana
GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET=<secret>
GF_AUTH_GENERIC_OAUTH_SCOPES=openid profile email
GF_AUTH_GENERIC_OAUTH_AUTH_URL=https://<host>/authorize
GF_AUTH_GENERIC_OAUTH_TOKEN_URL=https://<host>/oauth/token
GF_AUTH_GENERIC_OAUTH_API_URL=https://<host>/userinfo
GF_AUTH_GENERIC_OAUTH_USE_PKCE=true
GF_AUTH_GENERIC_OAUTH_LOGIN_ATTRIBUTE_PATH=preferred_username
GF_AUTH_GENERIC_OAUTH_NAME_ATTRIBUTE_PATH=name
GF_AUTH_GENERIC_OAUTH_EMAIL_ATTRIBUTE_PATH=email
```

Restart Grafana. The sign-in page will show a **Sign in with Portico** button.

### Notes

- Grafana creates a local user on first sign-in. If `allow_sign_up = false`,
  the user must already exist in Grafana.
- To map Portico's `role` claim to a Grafana role, see Grafana's
  `role_attribute_path` documentation. The claim name in Portico is `role`.

---

## Nextcloud

Nextcloud uses the **Social Login** or **OpenID Connect user backend** app
for OIDC. This guide uses the OpenID Connect user backend.

### Install the app

In Nextcloud: **Apps → Search for "OpenID Connect user backend"** → Enable.

### Register in Portico

```bash
portico client register \
  --id nextcloud \
  --name "Nextcloud" \
  --redirect-uri https://nextcloud.example.com/apps/user_oidc/code
```

Copy the client secret.

### Configure in Nextcloud

**Settings → OpenID Connect** → Add provider:

| Field | Value |
|---|---|
| Identifier | `Portico` (shown on the sign-in page) |
| Client ID | `nextcloud` |
| Client secret | Secret from above |
| Discovery URL | `https://<host>/.well-known/openid-configuration` |
| Scope | `openid profile email` |

Tick **Use PKCE** if the option is available.

For a named tenant, use `https://<host>/t/<tenant-code>/.well-known/openid-configuration`.

### Notes

- Nextcloud auto-discovers all endpoints from the discovery URL.
- The user's `sub` claim (stable user ID) is used as the account identifier.
  Existing Nextcloud accounts are not automatically linked — provision
  accounts through Portico from the start, or use SCIM to push them in.

---

## Any SAML 2.0 application

SAML integrations require exchanging metadata between Portico and the
service provider. The metadata carries endpoints and the certificate each
side uses to verify signatures.

### 1. Get Portico's metadata

```
GET https://<host>/saml/metadata                     ← default tenant
GET https://<host>/t/<tenant-code>/saml/metadata     ← named tenant
```

Import this URL (or download and import the XML) into the service provider.
The SP reads the signing certificate and the SSO endpoint from it.

### 2. Get the service provider's metadata

The SP will have a metadata URL or an XML file. In the Portico console:
**Applications → New application → SAML**.

| Field | Value |
|---|---|
| Entity ID | From the SP's metadata `entityID` attribute |
| ACS URL | The SP's assertion consumer service URL |
| SP metadata URL | Paste the SP's metadata URL (Portico fetches it) |

Or paste the metadata XML directly if the SP does not publish a URL.

### 3. Configure attribute mapping (if needed)

By default, Portico releases the same attributes it sends for OIDC — the
user's ID, name, email, and role. If the SP expects different attribute
names or additional fields, configure them under
[Field mappings](field-mappings.md).

### 4. Test

Initiate a sign-in from the SP. After authenticating in Portico, the SP's
ACS URL receives the signed assertion. If the SP reports a signature
error, re-fetch the metadata from Portico — the certificate may have been
rotated.

---

## Gitea

Gitea supports authentication sources including OIDC.

### Register in Portico

```bash
portico client register \
  --id gitea \
  --name "Gitea" \
  --redirect-uri https://gitea.example.com/user/oauth2/portico/callback
```

Copy the client secret.

### Configure in Gitea

**Site Administration → Authentication Sources → Add Authentication Source**:

| Field | Value |
|---|---|
| Authentication type | OAuth2 |
| Name | `portico` (this appears in the callback URL) |
| OAuth2 provider | OpenID Connect |
| Client ID | `gitea` |
| Client secret | Secret from above |
| OpenID Connect Auto Discovery URL | `https://<host>/.well-known/openid-configuration` |

Save. The redirect URI Gitea shows (`/user/oauth2/portico/callback`) must
match the one registered in Portico above.

On the sign-in page, users will see a **Sign in with portico** link.

---

## Not listed here?

If your application supports OIDC and is not listed, the
[Any OIDC application](#any-oidc-application) guide covers the general case.
Most applications that support "generic OAuth2" or "OIDC" follow the same
four steps: register a client, point the library at the issuer, enable PKCE,
and test.

For SAML applications, see [Any SAML 2.0 application](#any-saml-20-application).

If something does not work, [Troubleshooting](troubleshooting.md) lists the
error messages and their causes.
