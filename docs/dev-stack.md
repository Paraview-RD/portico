# The dev stack and the walkthrough

A directory with people in it, an inbox that catches mail, and a script that
walks an account through every hop and asserts at each one.

This is for contributors. It is not in the manual the binary ships — an
operator has no dev stack — which is why this page is excluded from the site.

It runs in CI as its own workflow — `.github/workflows/walkthrough.yml`,
which starts this same compose file. **Not on pull requests**: it costs two
container images and a server, and what it protects is the wiring between
subsystems rather than the change in front of a reviewer. On main after every
merge, on a tag because a release should not be the first time anybody asked,
and on `workflow_dispatch` so a branch touching the directory connector,
SCIM, or federation can be asked for it without waiting for a merge.

All thirteen steps run there. `examples/mock-sp` is in the repository now, so
the three federation steps are no longer skipped — see below for the guard
that still covers a checkout without it.

## Running it

```bash
docker compose -f deploy/dev-stack/compose.yml up -d --wait

go build -o portico ./cmd/server
PORTICO_DB_DSN=… PORTICO_ENCRYPTION_KEY=… ./portico      # in another terminal

./hack/walk-the-flow.sh
```

Everything binds to `127.0.0.1`. A directory with a published bind password
and an inbox with no authentication are both fine on a laptop and neither is
fine on a network.

| | Where | |
|---|---|---|
| Directory | `ldap://127.0.0.1:3890` | `cn=admin,dc=example,dc=org` / `portico-dev` |
| Inbox | <http://127.0.0.1:8426> | SMTP on 1026 |

Portico itself is deliberately **not** in the compose file. You run the
binary you just built against the database you already have; a compose file
that also owned the server would mean demonstrating a released image rather
than the change in front of you.

## What the walkthrough proves

Thirteen steps, fifty-five assertions, non-zero exit on the first failure. A
walkthrough that printed progress and always exited 0 would read as "the flow
works" while proving nothing.

| Step | What would otherwise regress silently |
|---|---|
| Synchronize | Accounts arrive; `uid=admin` is **skipped**, because a directory listing the same username as your administrator is not a claim on that account |
| Rename | One account stays one account. Get the external id wrong and every rename in the directory quietly becomes a second account here |
| Leave and return | Deactivated when the entry stops appearing, active again when it comes back |
| Empty result | The run **fails and changes nothing**. A wrong base DN looks exactly like an organisation everybody has left, and acting on it would deactivate every account the source owns |
| SCIM push | A directory renaming somebody lands on the account that already exists, matched on `externalId` |
| Registration | An unverified account is **refused at sign-in with a reason**, and the link that fixes it comes out of a real inbox |
| Export | A workbook with the password column present and empty, and the export recorded as `USER_EXPORT` |
| Sign-in through a client | `examples/mock-sp` signs in over **all three protocols**. OpenID Connect: discovery, a PKCE request, a code exchange, and an ID token **verified against the key set the discovery document named**. SAML: an authentication request over the redirect binding, and an **encrypted assertion the client decrypts and validates**. CAS: a service ticket the client validates back against `/serviceValidate`. Everything asserted at the end came out of something a client verified, not out of an API call this script made |

It runs in a scratch tenant (`walkthrough` by default, `WALK_TENANT` to
override), so it cannot touch the accounts of the deployment it is pointed
at. The tenant is reused between runs; there is no tenant delete, and
inventing one for a walkthrough would be the wrong reason to have it.

It is repeatable: the third step puts the directory back to its starting
state, because a walkthrough that only passes on a fresh fixture is one
nobody runs twice.

## Things that cost time once

**Rebuilding the LDAP container invalidates every reconciliation key.**
`compose down -v` makes slapd generate new `entryUUID`s, so a tenant that has
already synchronised is holding external ids that no longer exist: the next
run deactivates everybody and then skips the "new" entries as username
collisions. Use a fresh `WALK_TENANT` after recreating the container, or do
not recreate it.

**Phone numbers in the seed carry no spaces.** Portico accepts digits with an
optional leading `+`, so a directory that formats them for humans has every
one of its accounts refused. Finding that took several rounds, because the
run reported six skipped and nothing else — which is why the run now records
the reason as well as the count, grouped with an example of each. The same
mistake today reads `6 × That is not a valid phone number. (mei, arjun, …)`.

**Deleting an entry and adding it back is not the same person.** slapd
generates a new `entryUUID`, so what returns is a different entry sharing a
username, correctly skipped rather than reactivated. To make somebody leave
and return, move the entry out of the base DN and back — which is what
`ou=archive` in the seed is for.

**`/ready` is not "ready enough" to create a tenant against.** It answers as
soon as the database is reachable, which is true while migrations are still
running and before the first tenant exists. A run against a fresh database
failed once with `TENANT_NOT_FOUND` for a tenant the CLI had just created and
the database really held; it did not reproduce. CI therefore waits until the
server can actually sign somebody in, which is the signal that says
migrations ran and the bootstrap finished.

**A failed run still answers 200.** The sync endpoint returns the run record,
and a run that failed is a run that was successfully recorded. The outcome is
in `data.outcome`, the reason in `data.errorCode`. Reading only the envelope
reports a working refusal as missing — and, worse, lets a broken source
return a record full of zeros that every later assertion passes on.

**The export has a password column, and it is empty.** Three documents said
it had no such column, which was wrong and would have been the wrong thing to
"fix": the importer reads columns by position so a translated header still
works, so an export missing that column shifts every field one place to the
left on the way back in — silently. The heading stays; the values do not. The
walkthrough asserts both halves.

## What this does not cover

Nothing in the hops themselves — the thirteen steps reach the directory,
SCIM, registration through a real inbox, the export, and a sign-in through a
real relying party over each of the three protocols.

The three federation steps are skipped, out loud, when `examples/mock-sp` is
not in the checkout.

Four things about those steps worth knowing before changing them.

The client is **built and then run** rather than started with `go run` — `go
run` executes a child, so killing the process the script holds leaves the
server listening, and the next run either collides on the port or talks to a
stale instance still configured for the previous run and passes.

The flows are driven with a cookie jar, because state and the PKCE verifier
live in cookies: a run without them looks to the client exactly like a stolen
code.

The SAML registration is **pushed every run**, updating an existing one
rather than skipping it. Metadata carries the client's encryption
certificate, and Portico encrypts the assertion with whatever it was
registered with — so a registration left over from a client that has since
regenerated its key fails at the far end with "certificate does not match
provided key", which reads as a broken assertion rather than a stale
registration. The client keeps its state in `~/.cache/portico-walkthrough-sp`
for the same reason; delete it to start clean.

The assertion form's `+` characters are HTML-escaped as `&#43;`. A browser
decodes entities before submitting and curl does not, so the value has to be
decoded here — and is checked afterwards for anything outside base64's
alphabet, so that the next entity nobody thought of fails loudly instead of
arriving as "illegal base64 data at input byte 491".
