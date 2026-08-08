# What this does

Twelve commits, in four groups: finishing work that had shipped without its
documentation, adding the two things an open-source IAM is expected to have
and this one did not, fixing a navigation structure that had stopped
describing the product, and then a round of defects found by looking at the
running console rather than at the code.

40 files, +5003 / −309. The bulk of that is one file: `docs/api/openapi.yaml`.

## Documentation that had stopped being true

`README.md` — the file people read first — still listed SCIM and webhooks
under "Deliberately **not** in this version". Both shipped last cycle. Every
other document was updated when they landed; the README was missed, which is
the second time this class of defect has needed its own commit.

It survives a search because it is negation-shaped: looking for "SCIM" finds
the sentence denying it exists just as readily as the ones describing it, and
reading the feature list top to bottom shows nothing wrong, because what is
wrong is a sentence further down that the list does not contradict.

`docs/access-guide.md` had a different shape of gap — plain omission.
`/scim/v2` is reachable from outside and authenticated by a credential that
is not an administrator session, and it was missing from the entry-point
table, the one table whose job is to be complete.

`docs/api-conventions.md` asserted "`DELETE` is intentionally absent. Nothing
in this system is destroyed" while six DELETE routes existed. The wording was
wrong, not the implementation: sessions are revoked, and groups, webhook
subscriptions, and provisioning credentials are genuinely deleted, because
directories delete and recreate groups and disabling instead would accumulate
them forever. The real rule is narrower and is now stated as such —
**nothing the audit trail refers to may stop existing** — with a table of
which records that covers and which it does not.

## OpenAPI

`docs/api/openapi.yaml` describes all 82 operations under `/api/v1`.

It stops there deliberately. SCIM is RFC 7643 and RFC 7644; OpenID Connect,
OAuth 2.1, SAML, and CAS have their own specifications. Restating those in
house style produces a second account of somebody else's protocol, able to
disagree with the real one, that no client library reads. A test fails if a
path outside `/api/v1` appears in the document.

Written by hand rather than generated from annotations, because annotations
drift from the handlers exactly as readily as a separate file does — they
just drift somewhere less visible, at the cost of a code generator in the
build. The drift is made into a test failure instead:
`TestOpenAPIDescribesEveryRoute` walks the chi route tree and compares both
directions. An endpoint added without being described leaves an integrator
with a spec that silently omits it; a path described without existing sends
them at a 404.

That test is necessary and not sufficient — it reads paths and operation ids,
so a dangling `$ref` passes it and then breaks every generator. CI also runs
a pinned OpenAPI validator, which earned its place immediately by finding a
response referencing an envelope schema that was never defined.

Four details in the first draft were invented rather than checked, and were
caught by asking a running instance: the tenant header is `X-Portico-Tenant`;
a lockout answers 401 with `ACCOUNT_LOCKED`, not 423; `recovery-channels`
returns an object, not an array; and `permission-check` takes no parameters
and returns the principal.

## Directory-managed accounts say so

A provisioned group has been marked in the console since groups landed; a
provisioned account had not, though both know where they came from. The
consequence is not cosmetic — an administrator editing one is editing
something the next synchronization will overwrite.

Only the SCIM source is marked. The other three say how an account was born,
which is history; this one says who owns it now.

The wire type was also wrong in the direction that hides the bug:
`UserSource` listed three values where the server has four, so a
directory-provisioned account was something TypeScript believed could not
arrive.

## Navigation

The sidebar had two groups and the second was "Operations" — not a question,
but where things go when the first group will not have them. Application
registration ended up there beside the audit log and the password rules,
though it is the list of systems that trust this one to say who somebody is.

Now four administrative groups, each answering one thing: who is in the
system, who connects to it, what has happened, how it is configured.

The integration group is where this stops being a rearrangement. Two of the
three things that connect another system to Portico had no navigation entry
at all — provisioning credentials and webhook subscriptions were sections
near the bottom of the settings page. Both are now screens, which is the more
honest shape: issuing a credential that lets a directory create and disable
every account in a tenant is not a setting, and a delivery history is
something an operator comes to read. The components moved rather than being
rewritten, so the diff is a rename plus a page header.

## Two features that had everything except a screen

Ten translation keys had no reader. Eight were surplus and are deleted. The
other two were the last trace of work that stopped one step early:

- **Filtering users by organization.** The list endpoint has taken an
  `organizationId` since it was written and the label was in both bundles;
  only the control was missing. The filter an administrator reaches for most
  was the one they could not apply.
- **Showing which groups an account is in.** A whole `GET /users/{id}/groups`
  endpoint, a client method, and two labels, with no caller. Membership is
  still edited from the groups screen; this answers the opposite question,
  which is asked while looking at the person.

## What looking at the running console found

The four above came from reading the code. These came from opening it, and
none of them would have been found any other way.

**Nothing was telling browsers what they could cache.** An embedded file has
a zero modification time, so net/http sent no Last-Modified and no ETag, and
there was no Cache-Control either — nothing to revalidate against and nothing
telling a browser not to guess. What it guesses can be a cached index.html
naming an asset hash the new binary does not serve: a deploy that reaches
nobody until they clear their cache, and which looks like nothing at all from
either side. It cost an hour, diagnosing a console two versions old. Hashed
assets are now immutable and the shell is `no-cache`.

**Fifteen error codes had no message in the console** — every code the
groups, provisioning, and webhooks features introduced. A missing code falls
back to the server's own English, which is the right degradation but is
invisible in English and visible only to a Chinese reader. The existing
checks compare the two bundles to each other, so they stayed green while both
were missing the same code, which is the normal case. A new test reads the Go
source instead, with an exception list whose entries each carry a reason, and
a second test that fails when an exception outlives its code.

**The signed-out screens said "IDENTITY PLATFORM" while the sidebar said
身份平台**, because the auth shell had its own hand-assembled copy of the
brand lockup with the descriptor written in as English — in the component
whose stated purpose is stopping those screens from drifting apart.

**Three screens had no loading state.** Rows started as `null`, so
`rows?.length === 0` was false and `rows?.map` was undefined, and the body
rendered as nothing — identical to "there is nothing here". A reader cannot
tell a slow query from an empty tenant. The three that did have one were
writing the row by hand and none passed a `colSpan`.

**Every page decided its own width against the whole viewport**, so a table
ran to the far edge of a wide display while a form stopped at 448px; the
settings form sat on the page background while the profile screen's identical
fields sat in a card; and the profile screen was three cards at one width
followed by a fourth with no constraint. One column, two named widths, and
everything on the same kind of surface.

## Testing

Every guard added here was verified by breaking it and confirming the
intended test dies — twelve mutations, twelve kills, across the badge, the
OpenAPI guard, the navigation, and the two finished features. The later
round added ten more.

**Seven of those mutations survived on the first attempt, and every one of
them was a test that was checking nothing.** Two matched "Loading…" anywhere
on the page and caught the application shell's own loading state, passing on
every screen whether or not the screen had one. One asserted the content
column was "narrower than the viewport", which an unbounded column also
satisfies. One took the second block after the page header, which is a filter
bar on half these screens. One counted any bordered element and so reported a
288px search box as a layout defect. Two did not compile — and a mutation
that does not compile is not a mutation: the build failed, the previous
binary was still on disk, and the suite passed against it, which reads
exactly like the mutation surviving.

That is the argument for the practice. None of those tests would have failed
when the thing they were named after broke.

New browser coverage: `navigation.spec.ts`, because the rest of the suite
navigates by URL and could not see the failure that matters — a menu entry
pointing at a route the router does not know falls through to the user list
and reads as an item that does nothing. It also checks that each entry sits
under its own heading _by position_, since a screen filed under the wrong
group is still present and every per-item assertion passes while the menu
says something untrue.

Three of my own tests were found worthless and rewritten: one depended on a
dirty database, one passed on data an earlier run had left behind, and one
signed in as an administrator while claiming to test what an ordinary user
sees. The suite is now verified order-independent — clean and dirty database,
`--repeat-each`, and with the file order reversed.

## Verification

- `gofmt`, `go vet`, `golangci-lint` (0 issues), full `go test ./...`
- `tsc` on both projects, `prettier`, `oxlint` (0 errors)
- 18 component tests, 21 browser tests
- OpenAPI document validated
- Both features exercised in a real browser, in both languages, with zero
  unexpected console errors

## Not in this PR

The `CODE_OF_CONDUCT.md` reporting address is still a placeholder, and the
file says so in its first paragraph. Enabling Private Vulnerability Reporting
and the question of the demo credentials in git history are being handled
separately.
