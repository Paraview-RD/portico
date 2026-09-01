# CLI Reference

`portico` is a single binary. Run it with no arguments and it starts the server;
every subcommand runs as a short-lived process, talks to the database, and exits.

All subcommands read `PORTICO_DB_DSN` — the same connection string the server
uses. No other configuration is required.

---

## portico (server)

```
portico
```

Starts the server. Configuration is entirely environment variables; see
[Configuration](configuration.md) for the full reference.

```
portico --version   print the version and exit
portico --help      print usage and exit
```

---

## portico tenant

Tenants are provisioned from the command line because no account can act outside
its own tenant, so there is no one the API could authorise to create one.

```
portico tenant create --code <code> [--name <name>]
                      [--admin-username <name>] [--admin-password <password>]
portico tenant list
portico tenant enable  --code <code>
portico tenant disable --code <code>
```

### tenant create

Creates a tenant and its first administrator account.

| Flag | Default | Description |
|---|---|---|
| `--code` | (required) | Short code used at sign-in and in tenant-scoped URLs |
| `--name` | same as code | Display name |
| `--admin-username` | `admin` | Username of the first administrator |
| `--admin-password` | (generated) | Password. When omitted, the documented default is used and the account is refused until it is replaced at first sign-in |

The tenant's first sign-in URL is `{PORTICO_PUBLIC_URL}/t/<code>` for non-default
tenants, or `{PORTICO_PUBLIC_URL}` for the default tenant.

### tenant list

Lists all tenants with their code, name, status, and creation date.

### tenant enable / disable

Disabling a tenant refuses all sign-in without deleting any data. It can be
undone with `enable`.

| Flag | Required | Description |
|---|---|---|
| `--code` | yes | Tenant code |

---

## portico invitation

Issues and manages the codes that gate self-registration. The console can do
all of this too; this is the command-line equivalent — for a first
deployment before anybody has signed in, for scripting, and for when the
console cannot be reached.

```
portico invitation create  --code <code> --quota <n> [--tenant <code>]
                            [--organization-id <id>] [--group-id <id>]
                            [--expires-in <duration>]
portico invitation list    [--tenant <code>]
portico invitation disable --id <invitation-id> [--tenant <code>]
```

### invitation create

| Flag | Required | Description |
|---|---|---|
| `--code` | yes | The code somebody types at registration |
| `--quota` | yes | Maximum number of successful registrations. A quota greater than one is the normal case — one code, issued once, handed to several people |
| `--tenant` | no | Tenant code (defaults to the default tenant) |
| `--organization-id` | no | Organization assigned to the account on redemption |
| `--group-id` | no, repeatable | Group assigned to the account on redemption |
| `--expires-in` | no | How long the code stays valid, e.g. `720h`. Empty never expires |

### invitation list

Lists every code issued in the tenant: id, code, quota, how many
registrations it has produced, status, and expiry.

### invitation disable

| Flag | Required | Description |
|---|---|---|
| `--id` | yes | Invitation id |
| `--tenant` | no | Tenant code |

Disabling is terminal: there is no command that returns a code to active.
Issue a new one instead.

---

## portico client

Registers and manages the OIDC/OAuth 2.1 applications that sign in through
Portico. The console can do all of this too; the CLI is for first deployments,
scripting, and when the console cannot be reached. Both paths go through the
same service and produce the same audit trail.

```
portico client register --id <client-id> --redirect-uri <uri> [--redirect-uri <uri>]
                        [--tenant <code>] [--name <name>] [--public]
                        [--type WEB|NATIVE|USER_AGENT]
                        [--post-logout-redirect-uri <uri>] [--scope <scope>]
                        [--launch-url <url>] [--logo-uri <url|path>]
portico client list        [--tenant <code>]
portico client enable      --id <client-id> [--tenant <code>]
portico client disable     --id <client-id> [--tenant <code>]
portico client rotate-key  [--tenant <code>]
```

`--tenant` defaults to the default tenant throughout.

### client register

| Flag | Default | Description |
|---|---|---|
| `--id` | (required) | The `client_id` the application will send |
| `--redirect-uri` | (required, repeatable) | Where authorization codes are delivered. Matched exactly — no wildcards |
| `--tenant` | default tenant | Tenant code |
| `--name` | same as id | Display name |
| `--public` | false | Browser or mobile client. Cannot keep a secret; authenticates with PKCE instead |
| `--type` | `WEB` | `WEB` \| `NATIVE` \| `USER_AGENT` |
| `--post-logout-redirect-uri` | (none, repeatable) | Where to return the user after sign-out |
| `--scope` | `openid profile email` (repeatable) | Allowed scopes |
| `--launch-url` | (none) | Where a person opens the application, for the home screen |
| `--logo-uri` | (none) | Application icon: an `https://` URL, or a server-relative path such as `/icons/wiki.svg` |

A confidential client (no `--public`) is given a secret, printed once to stderr.
What is stored is a hash, so the secret cannot be recovered — rotate it by
registering a new client.

All clients must use PKCE, including confidential clients. This is an OAuth 2.1
requirement. A request without `code_challenge` is rejected.

### client list

Lists registered clients with their id, name, kind (public/confidential),
status, and redirect URIs.

### client enable / disable

| Flag | Required | Description |
|---|---|---|
| `--id` | yes | Client id |
| `--tenant` | no | Tenant code |

### client rotate-key

Replaces the tenant's signing key. The previous key stays in the published key
set (`/api/v1/jwks`) until its tokens have expired, so rotating does not
invalidate any active session.

| Flag | Required | Description |
|---|---|---|
| `--tenant` | no | Tenant code |

---

## portico sp

Registers and manages SAML 2.0 service providers.

```
portico sp register           --metadata <file|url> [--tenant <code>] [--name <name>]
                              [--launch-url <url>] [--logo-uri <url|path>]
portico sp list               [--tenant <code>]
portico sp enable             --entity-id <id> [--tenant <code>]
portico sp disable            --entity-id <id> [--tenant <code>]
portico sp certificate        [--tenant <code>]
portico sp rotate-certificate [--tenant <code>]
```

`--tenant` defaults to the default tenant throughout.

Portico's own metadata is at `{PORTICO_PUBLIC_URL}/saml/metadata` for the
default tenant and `/t/<code>/saml/metadata` for any other. Hand that URL to
the service provider; it is the other half of the exchange.

### sp register

Registration takes the service provider's metadata document — not individual
fields. The document is published by the service provider and carries everything
the protocol needs: entity id, assertion consumer service endpoints, NameID
formats.

| Flag | Default | Description |
|---|---|---|
| `--metadata` | (required) | Path to a local file, or an `https://` URL. Plain HTTP is refused |
| `--tenant` | default tenant | Tenant code |
| `--name` | entity id | Display name |
| `--launch-url` | (none) | Where a person opens the application, for the home screen |
| `--logo-uri` | (none) | Application icon: an `https://` URL, or a server-relative path |

### sp list

Lists registered service providers with their entity id, name, status, and
assertion consumer service URLs.

### sp enable / disable

| Flag | Required | Description |
|---|---|---|
| `--entity-id` | yes | The service provider's entity id |
| `--tenant` | no | Tenant code |

### sp certificate

Prints the current SAML signing certificate as PEM.

### sp rotate-certificate

Generates a new signing certificate and retires the current one. The retired
certificate is kept — it is not deleted. Every service provider must be
reconfigured with the new certificate before it will accept another assertion;
until then, the previous certificate is what they need to look up.

---

## portico cas

Registers and manages CAS services.

```
portico cas register --url <prefix> [--tenant <code>] [--name <name>]
                     [--launch-url <url>] [--logo-uri <url|path>]
portico cas list     [--tenant <code>]
portico cas enable   --url <prefix> [--tenant <code>]
portico cas disable  --url <prefix> [--tenant <code>]
```

`--tenant` defaults to the default tenant throughout.

The CAS endpoints are at `{PORTICO_PUBLIC_URL}/cas/...` for the default tenant
and `/t/<code>/cas/...` for any other. Point the client's CAS server URL at the
part before `/login`.

### cas register

`--url` is a URL prefix, not a pattern. A `service=` parameter matches when it
begins with the registered value. Registration always covers a path boundary:
`https://app.example.com/` cannot match `https://app.example.com.elsewhere.test`.

| Flag | Default | Description |
|---|---|---|
| `--url` | (required) | Service URL prefix |
| `--tenant` | default tenant | Tenant code |
| `--name` | same as prefix | Display name |
| `--launch-url` | (none) | Where a person opens the application, for the home screen |
| `--logo-uri` | (none) | Application icon: an `https://` URL, or a server-relative path |

### cas list

Lists registered services with their URL prefix, name, and status.

### cas enable / disable

| Flag | Required | Description |
|---|---|---|
| `--url` | yes | The registered URL prefix |
| `--tenant` | no | Tenant code |

---

## portico trial

Manages the tenants that a self-service trial form created. These commands need
the same `PORTICO_DB_DSN`. Tenant provisioning is on the command line for the
same reason as `portico tenant`: no account can act outside its own tenant.

```
portico trial list
portico trial delete --code <code> --yes
portico trial prune
```

### trial list

Lists every tenant a confirmed trial produced: code, name, status, industry,
the address that requested it, and when it was confirmed. Tenants provisioned
by hand with `portico tenant create` are not shown.

### trial delete

Deletes a trial tenant and everything in it — accounts, organisations,
applications, the audit trail. This cannot be undone.

| Flag | Required | Description |
|---|---|---|
| `--code` | yes | Tenant code |
| `--yes` | yes | Confirms the deletion. Required so scripts cannot delete by accident |

Refuses any tenant that a trial did not create: the default tenant and anything
provisioned with `portico tenant create` are out of reach.

### trial prune

Deletes trial requests whose confirmation links expired before anyone opened
them, releasing the tenant codes they were holding. A running server already
does this every hour; this command is for when there is no running server, or
when the codes are needed immediately.

---

## portico ready

```
portico ready [--url <base-url>]
```

Asks a running instance whether it can serve. Exits 0 if ready, 1 if not.

| Flag | Default | Description |
|---|---|---|
| `--url` | `http://127.0.0.1:<port>` | Base URL of the instance to check. The port comes from `PORTICO_ADDR` |

The release image is `FROM scratch` — no shell, no `curl`. This command is what
a container health check runs because it is the only executable the image
contains.

It talks to the instance over HTTP rather than opening the database directly:
what matters is whether the serving process can reach its dependencies, not
whether a fresh connection from a second process could.
