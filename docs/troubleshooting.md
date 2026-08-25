# Troubleshooting

Errors that come up during integration, and what to do about them.

## OpenID Connect / OAuth

### PKCE required {#pkce-required}

```
error=invalid_request
error_description=code_challenge is required; this server implements
OAuth 2.1, which requires PKCE of every client
```

**Cause:** The authorization request did not include `code_challenge` and
`code_challenge_method`. Portico implements OAuth 2.1, which makes PKCE
mandatory for all client types — including confidential clients.

**Fix:** Enable PKCE in your OIDC library or application configuration.
Set `code_challenge_method=S256`. The library generates and manages the
verifier and challenge automatically.

For a Casdoor integration: edit the application, enable the PKCE option.

For a manual request: generate a random `code_verifier` (43–128 URL-safe
characters), compute `code_challenge = BASE64URL(SHA256(code_verifier))`,
and add both to the authorization request. Pass `code_verifier` again when
exchanging the code for a token.

Portico does not accept `code_challenge_method=plain`.

---

### redirect_uri not registered

```
error=invalid_request
error_description=redirect_uri is not registered for this client
```

**Cause:** The `redirect_uri` in the authorization request does not exactly
match any URI registered for this client. Matching is byte-for-byte: a
trailing slash, a different path, or `http` vs `https` are all mismatches.

**Fix:** Open the application in the Portico console, check the registered
redirect URIs, and make the authorization request use exactly one of them.
The parameter is required even if only one URI is registered — see
[Why pass redirect_uri if it is already configured?](federation.md#registering-an-application)

---

### Wrong tenant

```
{"code":"AUTH_REQUEST_WRONG_TENANT","message":"This sign-in request belongs
to a different tenant. Sign out and sign in to the tenant the application
asked for."}
```

**Cause:** The user is signed in to a different tenant than the one the
authorization request was made against.

**Fix:** Sign out first, then start the authorization flow again. This
happens when a browser has an active session for tenant A and a link opens
a sign-in for tenant B.

---

### Authorization request expired or already used

```
{"code":"AUTH_REQUEST_NOT_FOUND","message":"This sign-in request has
expired or was already used. Start again from the application."}
```

**Cause:** The sign-in was abandoned, the tab was left too long, or the
code was already exchanged. Authorization codes are single-use.

**Fix:** Start the sign-in flow again from the application.

---

### Authorization code invalid or expired

```
error=invalid_grant
error_description=invalid or expired authorization code
```

**Cause:** The code passed to `/oauth/token` was already used, has expired,
or does not exist. Codes are short-lived (a few minutes) and single-use.

**Fix:** Start the authorization flow again. If this happens consistently
in an automated flow, check that the token exchange happens immediately
after the redirect, not after a delay.

---

### Client not found or disabled

```
{"code":"OAUTH_CLIENT_NOT_FOUND","message":"The application this sign-in
was for is no longer registered."}

{"code":"OAUTH_CLIENT_DISABLED","message":"The application this sign-in
was for has been disabled."}
```

**Cause:** The `client_id` does not exist in this tenant, or the application
has been disabled in the console.

**Fix:** Check the client ID in the console under **Applications**. Client
IDs are scoped to a tenant: `client_id=wiki` in tenant A and `client_id=wiki`
in tenant B are different registrations.

---

### Wrong authorization endpoint URL

**Symptom:** The browser lands on the Portico home page or login page instead
of proceeding with the sign-in, with no error message. The URL in the address
bar contains the authorization parameters.

**Cause:** The path is wrong. Portico's OIDC authorization endpoint is
`/authorize`, not `/auth` or `/oauth/authorize`.

**Fix:** Use the correct path:

```
# Default tenant
https://<host>/authorize

# Named tenant
https://<host>/t/<tenant-code>/authorize
```

Fetch the discovery document to verify:

```
GET https://<host>/t/<tenant-code>/.well-known/openid-configuration
```

The `authorization_endpoint` field in the response is the correct URL.

---

## SAML 2.0

### Assertion signature verification failed

**Cause:** The service provider is validating the assertion against a
certificate that has been rotated on the Portico side, or the SP's copy of
the metadata is stale.

**Fix:** Re-fetch Portico's metadata and re-import it into the SP:

```
GET https://<host>/saml/metadata
GET https://<host>/t/<tenant-code>/saml/metadata
```

See [Federation → Certificates](federation.md#certificates) for the
rotation process.

---

### No response comes back to the SP

**Cause:** The assertion consumer service (ACS) URL registered in Portico
does not match what the SP sent in the `AuthnRequest`, or the SP's metadata
has not been imported into Portico.

**Fix:** In the Portico console, check the application's SAML configuration.
The ACS URL and the entity ID must match the SP's metadata exactly.

---

## CAS

### Invalid ticket

**Cause:** The service ticket has expired (tickets are short-lived), was
already validated, or the `service` parameter in the validate call does not
exactly match the `service` parameter in the login redirect.

**Fix:** The service URL must be identical in both the login redirect and
the validate call — including the scheme, port, and any query parameters
the registration includes. Start the login flow again if the ticket has
expired.

---

### Service not registered

**Cause:** The `service` URL in the `/cas/login` request is not registered
for this tenant.

**Fix:** Register the service URL in the Portico console under
**Applications → CAS**.

---

## Startup and configuration

### Server exits at startup: PORTICO_DB_DSN is not set

The database connection string has no default. Set `PORTICO_DB_DSN` to a
valid PostgreSQL connection string before starting.

### PORTICO_JWT_SECRET warning on startup

```
warn: jwt_secret_generated=true
```

A random JWT secret was generated because `PORTICO_JWT_SECRET` is unset.
Sessions will end when the process restarts. Set a persistent secret:

```bash
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
```

### PORTICO_JWT_SECRET too short

```
PORTICO_JWT_SECRET is N bytes; it must be at least 32.
Generate one with: openssl rand -hex 32
```

The secret must be at least 32 bytes. Generate a new one with the command
shown and replace the old value.

### PORTICO_ENCRYPTION_KEY and PORTICO_JWT_SECRET are the same value

These must be different. They protect different things and leak through
different routes; one value doing both jobs means either compromise costs
both. Generate a second key:

```bash
export PORTICO_ENCRYPTION_KEY=$(openssl rand -hex 32)
```

### Mail is not arriving

1. Verify `PORTICO_SMTP_HOST` is set (or `PORTICO_MAIL_TRANSPORT=resend`
   with `PORTICO_RESEND_API_KEY`).
2. Check that `PORTICO_PUBLIC_URL` is the address people actually reach —
   password recovery links are built from it.
3. For Resend: the `From` address must be on a domain verified with Resend.
4. Check SMTP port and encryption: default is port 587 with STARTTLS.
   Change `PORTICO_SMTP_ENCRYPTION` to `tls` for port 465, or `none` if
   the server requires unencrypted.

### Tokens are rejected by other instances in a cluster

All instances must share the same `PORTICO_JWT_SECRET`. A token minted by
one instance is verified by all; a different secret per instance means
tokens are rejected wherever they were not minted.
