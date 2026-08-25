# Configuration Reference

All environment variables Portico reads at startup, with their defaults.

Every setting has a working default except `PORTICO_DB_DSN`, which has none:
a connection string for someone else's database is not something to guess.
Unset, the server reports it and exits.

## Core

| Variable | Default | Description |
|---|---|---|
| `PORTICO_DB_DSN` | *(required)* | PostgreSQL connection string. URL form: `postgres://user:pass@host:5432/db?sslmode=disable`. Keyword form also accepted. |
| `PORTICO_ADDR` | `:8410` | TCP address the HTTP server listens on. |
| `PORTICO_PUBLIC_URL` | `http://localhost:8410` | The URL people use to reach this deployment. Used to build links in email. Wrong when unset in production: password-recovery links point at localhost. |
| `PORTICO_JWT_SECRET` | *(random)* | Signs and verifies access tokens. Must be at least 32 bytes. Unset: a random secret is generated per process — sessions end on restart and tokens are rejected by other instances. Generate: `openssl rand -hex 32`. |
| `PORTICO_ENCRYPTION_KEY` | *(unset)* | 32-byte hex key protecting LDAP bind passwords at rest. Unset: storing a directory connector's credentials is refused. Must differ from `PORTICO_JWT_SECRET`. Generate: `openssl rand -hex 32`. |
| `PORTICO_LOG_LEVEL` | `info` | Log verbosity. One of `debug`, `info`, `warn`, `error`. |

## SMTP mail

All four of the variables below must be set for mail to work with the SMTP
transport. Without a working mail configuration, password recovery and
registration confirmation are unavailable.

| Variable | Default | Description |
|---|---|---|
| `PORTICO_MAIL_TRANSPORT` | `smtp` | Which transport to use. `smtp` or `resend`. |
| `PORTICO_SMTP_HOST` | *(unset)* | SMTP relay hostname. Unset: mail is not available. |
| `PORTICO_SMTP_PORT` | `587` | SMTP port. Typically 587 (STARTTLS) or 465 (TLS). |
| `PORTICO_SMTP_USERNAME` | *(unset)* | SMTP authentication username. |
| `PORTICO_SMTP_PASSWORD` | *(unset)* | SMTP authentication password. |
| `PORTICO_SMTP_FROM` | *(unset)* | Sender address, e.g. `noreply@example.com`. |
| `PORTICO_SMTP_ENCRYPTION` | `starttls` | Encryption mode. `starttls` (port 587), `tls` (port 465), or `none`. |

## Resend mail

Used when `PORTICO_MAIL_TRANSPORT=resend`.

| Variable | Default | Description |
|---|---|---|
| `PORTICO_RESEND_API_KEY` | *(required with resend)* | API key from [resend.com](https://resend.com). |
| `PORTICO_MAIL_FROM` | *(required with resend)* | Sender address on a Resend-verified domain. |

## Tokens and sessions

| Variable | Default | Description |
|---|---|---|
| `PORTICO_TOKEN_TTL` | `2h` | Access token lifetime. Accepts a Go duration (`15m`, `2h`) or a number of seconds. Overridable at runtime in **Settings**; this is the startup default. |

## Authentication rate limiting

Rate limiting is on by default. Set to `0` to disable entirely.

| Variable | Default | Description |
|---|---|---|
| `PORTICO_AUTH_RATE_LIMIT` | `60` | Writes allowed per client IP per minute under `/api/v1/auth/`. |
| `PORTICO_AUTH_RATE_LIMIT_BURST` | `30` | How many requests may arrive at once within that budget. |

## Bootstrap

| Variable | Default | Description |
|---|---|---|
| `PORTICO_INITIAL_ADMIN_USERNAME` | `admin` | Username of the first administrator, created on an empty database. Ignored once an administrator exists. |
| `PORTICO_INITIAL_ADMIN_PASSWORD` | *(random, logged once)* | Password for the initial administrator. If unset, a random password is generated and logged once at startup. |

## Features

| Variable | Default | Description |
|---|---|---|
| `PORTICO_LANDING_PAGE` | `false` | Show a landing page at the root instead of the sign-in form. For public-facing deployments where visitors may not have an account. |
| `PORTICO_TRUST_PROXY_HEADERS` | `false` | Believe `X-Forwarded-For` and `X-Real-IP`. Enable only when a trusted proxy sits in front and rewrites these headers — otherwise callers can forge their own audit log IP. |
| `PORTICO_TENANT_CONSOLE` | `false` | Register the operator-only screens: a list of every tenant and the ability to disable one. Off by default: on a deployment where the `default` tenant belongs to a customer, turning this on would let that customer's administrator enumerate every other tenant. |
| `PORTICO_DEFAULT_LOCALE` | `en` | Locale for messages when neither the account nor the tenant specifies one. Must be a locale this build ships messages for; the server refuses to start with an unrecognized value. |

## Self-service trials

These settings only matter when `PORTICO_TRIAL_SIGNUP=true`.

| Variable | Default | Description |
|---|---|---|
| `PORTICO_TRIAL_SIGNUP` | `false` | Enable self-service trial tenant creation. Requires SMTP. Off everywhere that is not a public demonstration. |
| `PORTICO_TRIAL_MAX_TENANTS` | `50` | Maximum trial tenants that may exist at once. Reached: the signup form says so rather than queueing. `0` disables the cap. |
| `PORTICO_TRIAL_RATE_PER_HOUR` | `10` | How many trial requests the whole deployment accepts per hour. Protects sending reputation shared across all tenants. `0` disables. |
| `PORTICO_TRIAL_RATE_LIMIT` | `5` | Trial signup requests per client IP per minute. |
| `PORTICO_TRIAL_RATE_LIMIT_BURST` | `3` | Burst allowance for trial signup rate limiting. |
| `PORTICO_TRIAL_BLOCKED_EMAIL_DOMAINS` | *(unset)* | Comma-separated list of additional email domains to reject for trial signups, added to the built-in throwaway-address list. Example: `mailinator.com,guerrillamail.com`. |

## Observability

| Variable | Default | Description |
|---|---|---|
| `PORTICO_METRICS_ADDR` | *(unset)* | TCP address for a Prometheus `/metrics` endpoint. Example: `127.0.0.1:9410`. Unset: no metrics are published and no second listener is opened. **Bind to a private interface** — this endpoint is unauthenticated by design, matching Prometheus conventions. Never expose it on the same port as the application. |

## Example: minimal production `.env`

```bash
PORTICO_DB_DSN=postgres://portico:secret@db:5432/portico?sslmode=disable
PORTICO_PUBLIC_URL=https://id.example.com
PORTICO_JWT_SECRET=<output of: openssl rand -hex 32>
PORTICO_ENCRYPTION_KEY=<output of: openssl rand -hex 32>

# SMTP
PORTICO_SMTP_HOST=smtp.example.com
PORTICO_SMTP_USERNAME=noreply@example.com
PORTICO_SMTP_PASSWORD=<smtp password>
PORTICO_SMTP_FROM=noreply@example.com

# Proxy
PORTICO_TRUST_PROXY_HEADERS=true
```

For a full deployment walkthrough, see [Running in production](deployment.md).
