# Changelog

Notable changes to this project. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Working toward 0.2.0. See
[docs/requirements/v0.1-requirements.md](docs/requirements/v0.1-requirements.md)
§4.1 for the scope.

### Added

- **Accounts can be read out of an Active Directory or OpenLDAP.** The
  opposite direction from SCIM, which is a server here: a directory pushes
  into `/scim/v2` and Portico never reaches out, while this has Portico
  connect and pull. Reconciled on the directory's own stable identifier, so
  a rename stays a rename rather than becoming a second account.
- The attribute map has no defaults, because Active Directory and OpenLDAP
  disagree on every one of them and a wrong guess imports a directory's worth
  of accounts named after the wrong field. The console ships both as presets
  and leaves every field visible and editable.
- `objectGUID` is handled as the binary value it is and rendered in
  Microsoft's own GUID form. Read as text it becomes mojibake that varies
  with the bytes, and it is the reconciliation key — so a rename would
  silently become a duplicate.
- **A run that gets an empty result changes nothing and fails.** An empty
  search looks exactly like a directory everybody has left; it is far more
  often a wrong base DN, and acting on it would deactivate every account the
  source owns.
- A synchronization never touches an account it does not own. Ownership is
  recorded rather than inferred from the username, so an administrator who
  shares a name with somebody in the directory is skipped rather than
  renamed, demoted, or later deactivated.
- Every run is kept — created, updated, deactivated and skipped counts, and
  the reason when it failed — because the question afterwards is "when did
  this start", which one overwritten result cannot answer.
- **A run says why entries were skipped, not only how many.** "6 skipped" and
  nothing else sends an operator to the documentation, which says a skip is
  most often a username collision — so when it is not one, that sentence is a
  wrong lead rather than no lead. It cost a walkthrough several rounds to
  find that every account was being refused for a phone number written with
  spaces, which the code knew as it refused each one and recorded nowhere.
  Grouped by reason with an example of each, and bounded — a source pointed
  at the wrong attribute skips everything for the same reason, and a line per
  entry would be a line per account in the directory. It is on the run record
  itself, and beside the counts in the console, whatever the outcome — a run
  that skipped entries still succeeded, and a reason that only appeared on a
  failure would be one nobody read.
- **An account has the attributes a directory actually has for it** — 24 of
  them, named after SCIM 2.0's core User schema and its enterprise extension
  rather than invented, so a directory's fields land where they belong. They
  round-trip through SCIM in both directions, with the enterprise extension
  under its own URN where a conforming client looks for it.
- Describing somebody and deciding their access are separate endpoints.
  `/users/{id}/profile` cannot reach a role, a status, or an organization,
  which is what makes the self-service version of it safe — minus the
  manager, because a reporting line is an organizational fact that downstream
  systems read as an approval chain.
- **An organization can name whoever is responsible for it**, and a person
  can be attached to organizations beside the one they belong to. Both grant
  nothing: this version has two fixed roles, and a field that quietly became
  a third would be a permission model nobody designed. The primary membership
  does not move — it stays the one thing SCIM writes and an export names.
- Multiple root organizations always worked; there is now a test saying so,
  so a later change cannot quietly introduce a single-root assumption.
- **The directory can be exported as a spreadsheet**, in the same columns the
  import template uses, so a file taken out can be edited and fed back in.
  No password is ever written into it, and the column for one is present and
  empty rather than absent: the parser reads columns by position so that a
  translated header still works, so an export missing it would shift every
  later field one place to the left on the way back in, silently. An export
  is a report, and a report carrying credentials is a credential-distribution
  mechanism nobody meant to build. Audited, because "who took a copy of the
  directory, and when" is asked after an incident rather than before one.
- **Accounts can be enabled, disabled, or moved between organizations in
  bulk.** Each one goes through the path a single account takes, so the rule
  that protects the last active administrator is not bypassed by selecting
  more people — a bulk path writing straight to the table would be a way
  around every such rule, and an invisible one. Failures are reported per
  account, because an operator needs to know *which* one.
- The import template gained eight columns, appended rather than inserted:
  the parser reads by position so that a translated header still works, which
  also means a column in the middle would silently remap every spreadsheet
  anybody has already prepared.
- **The user list is filtered from the organization chart**, in a column
  beside it, instead of from a dropdown of names. A dropdown could name every
  organization and could not say how any of them relate — which is most of
  what somebody knows about their own company. "Everybody in Engineering" is
  a question about a branch, and a list of names cannot express a branch.
  Picking a node selects it and everything under it, so the shape on screen
  and the shape of the answer are the same shape. It also asks the question a
  dropdown had no way to put: who is in no organization at all.
- The chart sits inside the page's own column rather than as a second sidebar
  next to the navigation. Every screen is laid out in the same column and a
  browser test holds that; widening one screen for one control would have
  meant either breaking the rule or making this screen an exception to it.
- No member counts beside the names, though the endpoint reports them. They
  count the people filed directly against each organization, and picking one
  now answers with its whole branch — so the number beside a name would
  disagree with the number of rows that appear when you click it. A wrong
  number is worse than no number.
- **Five more event types**, each with somewhere it is actually sent from:
  `user.password_changed`, `user.locked`, `user.unlocked`,
  `organization.enabled`, and `organization.disabled`. The first is published
  from the one place all three password paths meet, so a subscriber hears
  about a completed recovery exactly as it hears about an administrator's
  reset — publishing at the call sites would have covered two of the three
  and missed a password changed by somebody who had forgotten it, which is
  the one most worth hearing about. `user.locked` is sent where the lock is
  applied rather than on each later attempt against an already-locked
  account: a lock happens once and is refused many times.
- Organizations announce a status change as its own pair rather than as
  `organization.updated`, matching how an account does. A mirror has to act
  on a disable, and making it diff a payload to discover one is how a mirror
  goes on offering an organization nobody may be added to.
- **The event picker says what each event means**, in the reader's language,
  with the wire identifier beside it rather than instead of it. It was a
  column of bare dotted identifiers: an administrator deciding what to
  subscribe to had to read the manual to find out what `group.members_changed`
  was, and the console is where that decision is made. Both are shown because
  they answer to different people — the identifier is what the receiver's
  code matches on. Grouped by subject, which was already the first half of
  every name.
- An event this build has no label for falls back to its identifier rather
  than to a translation key. The wildcard subscribes to event types later
  versions add, and a server can be newer than the console talking to it, so
  meeting an unknown event is expected rather than exceptional.
- **Self-registration can require a confirmed address.** Off by default, per
  tenant. Registration used to create a usable account with whatever email
  was typed — and that address is both a sign-in identifier and where a
  password-reset link is sent, so somebody could open an account under a
  colleague's address and receive their reset links. Acceptable on a closed
  intranet, which is the only place open registration was ever usable here;
  not acceptable facing outward.
- Turning it on where nothing can be sent is refused at the point of turning
  it on, rather than accepted and then stranding every registration on a
  message that never arrives. Registration checks again before creating the
  account, because a mail relay can be taken out of the environment
  afterwards.
- **Turning it on does not lock out anybody who already registered.** An
  account registered while the requirement is off is marked accepted then,
  and the migration does the same for accounts that predate the column — the
  same rule, applied once to history and continuously afterwards. Without it
  a policy change would silently revoke access from every existing member.
- Sign-in tells an unconfirmed account why it was refused, and the resend
  endpoint tells nobody anything. The asymmetry is deliberate: the first is
  a registration oracle and is accepted because the alternative is a dead
  end for somebody who never opened the message — and it is confined to a
  caller who already has the password. The second is public, so it answers
  identically for an unknown address, an already-confirmed account, and a
  successful send.
- Only self-registered accounts are gated. An administrator-created,
  imported, or directory-synchronized account is vouched for by whoever
  created it.
- **Somebody can close their own account.** The one sanctioned way to
  disable yourself — everywhere else that is refused, so an administrator
  cannot lock themselves out by accident, and this is the case that rule was
  never about. The password is required, for the same reason changing one is:
  a stolen token must not be enough to destroy the account it was stolen
  from. The tenant's last active administrator cannot close theirs; there
  would be nobody left who could undo it.
- Closing deactivates rather than deletes, so it is reversible and the audit
  trail keeps pointing at an account that exists. Signing in afterwards says
  `ACCOUNT_CLOSED` rather than `ACCOUNT_DISABLED`: the two look identical in
  the status column and call for different conversations. `closedAt` is on
  the user record so an administrator can tell "they left" from "we suspended
  them", and it is cleared when the account is reinstated.
- Reinstating does not revive the sessions the closure ended. A token from a
  laptop somebody no longer has must not start working again because an
  administrator undid a departure.
- **`PORTICO_ENCRYPTION_KEY`**, protecting credentials the server has to
  store and later use rather than merely verify. Today that is a directory
  bind password. Without it, saving one is refused rather than written in the
  clear. It must differ from `PORTICO_JWT_SECRET`.
- **Every administrative screen says what it is for**, in three or four
  sentences above the content: when the screen is the one you want, what has
  to be in hand before starting, and which neighbouring screen does the
  opposite thing. Collapsible and remembered, so it stops being furniture for
  whoever reads it daily. Deliberately short — the manual is compiled into
  the same binary and linked from each panel, and a second copy of an
  explanation is a guarantee that the two will disagree.
- The same explanation inside the registration dialogs, where the jargon
  actually is. A dialog covers the page that would have explained its own
  title, which is why "register a service provider" kept being asked about:
  the sentence defining the term was on the list behind it.

- **Messages are written in the reader's language.** A password-reset link
  and a registration confirmation are the only text Portico sends where
  nobody had a chance to pick a language from a menu — and they were English
  format strings. The language is chosen in one place and one order: the
  account's own `preferredLanguage`, then the tenant's default, then the
  deployment's `PORTICO_DEFAULT_LOCALE`, then English. Each step is somebody's
  stated preference, and a later one applies only because the earlier said
  nothing.
- Choosing it from the account is safe here in a way it would not be a few
  lines earlier: a recovery request for an address nobody holds never reaches
  delivery, so the language of a message is only ever seen by the person
  whose account it is. If one were sent either way, the language itself would
  disclose that the address is registered.
- `PORTICO_DEFAULT_LOCALE`, and a per-tenant default beside it in settings.
  Empty at the tenant means "follow the deployment" rather than "English", so
  a tenant that has said nothing follows a deployment that changes its mind
  later instead of being frozen at whatever it was the day it was created. A
  tag this build has no messages for is refused at both levels rather than
  stored — the server will not start with one, and the settings endpoint
  answers `INVALID_SETTINGS`.
- **The manual is compiled into the binary and served at `/docs`**, in English
  and 简体中文. Documentation hosted elsewhere drifts from the releases people
  actually run, and the failure mode is somebody following instructions for a
  version they do not have — which reads as the product being wrong rather
  than the page being old. Public, because most of what it explains is how to
  configure a deployment nobody has signed into yet, and it names where
  credentials come from rather than what any of them are. The console links
  into it from the screen you are on: the directory page to the LDAP chapter,
  provisioning to SCIM, applications to federation, subscriptions to
  webhooks.
- A page with no translation falls back to English **and says so**, in the
  language the reader asked for. Falling back silently would let somebody
  take stale English for current Chinese.
- **`examples/mock-sp`**: a relying party you can sign in to, in a browser,
  over all three protocols — so a deployment can be demonstrated and checked
  before its details are handed to somebody who has to integrate against it.
  It is a real client rather than a stub, which is how the duplicate SAML
  attributes below were found: they are invisible from inside Portico and
  obvious from the far end.
- **A dev stack and a walkthrough** — a directory with people in it, an inbox
  that catches mail, and a script that takes an account through thirteen
  steps: synchronize, a rename that must stay one account, an entry that
  leaves and returns, an empty result that must change nothing, a SCIM push,
  a registration confirmed out of a real inbox, an export, and a sign-in to
  `examples/mock-sp` over all three protocols. It asserts at every hop and
  exits non-zero at the first failure, because a script that printed progress
  and always exited 0 would read as "the flow works" while proving nothing.
  What it covers is the wiring between subsystems, which is where nothing
  else looks: each hop has tests on both sides of it, and none of them can
  see a connector that reaches the right directory and lands the wrong field.
  It is how the skipped-entry blindness above was found, and how three
  documents were caught claiming the export has no password column when it
  has one and must. Contributor-facing, so it is documented in
  [docs/dev-stack.md](docs/dev-stack.md) rather than in the shipped manual.
- It runs in CI on main, on a tag, and on request — deliberately not on pull
  requests, where two container images and a server would be spent protecting
  something other than the change in front of the reviewer.
- **The accounts in no organization at all can be asked for**, through the
  reserved value `none` on the same filter. The question could not be put
  before: an empty value already means "every organization", so the people
  nobody has filed anywhere — which is precisely who somebody goes looking
  for — were the one group unreachable from the control meant to find
  groups. `none` cannot collide with a real organization, whose id is a UUID.
- Webhook delivery is now tested against a server that actually receives it,
  which was the one hop in that chain nothing covered. The rules for
  registering a destination are tested where they live and everything up to
  the queue was covered; what arrived at the far end — signed by the recipe
  the documentation gives a subscriber, rather than by calling the signing
  code and comparing it with itself — was not.

### Changed

- **Filtering the user list by organization now means that organization and
  everything under it.** It was an exact match, which is defensible on its
  own and wrong the moment the filter is reached from a chart: picking a
  division and being shown only the handful of people filed directly against
  the division itself, rather than everybody in it, reads as a defect and not
  as a distinction. The narrower question has not been asked; the broader one
  is asked constantly. The subtree is resolved inside the same statement as
  the count and the page, so somebody reparenting a department mid-request
  cannot produce a total that disagrees with the rows beneath it.
- The export shares that filter, and now has a test that opens the workbook
  and compares it with the listing rather than trusting the two to agree for
  having called the same function. "Export what I am looking at" is a claim
  the documentation makes; this is what makes it true rather than intended.
- **The module path and the container image moved to the address this project
  is actually at.** `github.com/paraview/portico` was a name only the compiler
  ever saw and a repository that does not exist for everybody else: `go
  install github.com/paraview/portico/...` resolves to nothing, and the
  release pipeline stamped that same dead path into every binary it built. It
  is now `github.com/Paraview-RD/portico`. The image is
  `ghcr.io/paraview-rd/portico` — lowercase, because GHCR takes its namespace
  from the GitHub owner and an image reference may not carry capitals, so the
  two differ in case and only here. Done before a release rather than after,
  when it would have cost a v2 module path or a redirect nobody maintains.
- **The settings screen has a second column**, and still one form and one save
  button below both. It was a single 40rem card on a 1440px page, leaving two
  thirds of the content area empty — and widening it was the wrong fix, and
  had been rejected once already: this screen argues with the reader about why
  each default is what it is, and prose set to the width of the page is prose
  nobody finishes. What was wrong was not the width of the form but that there
  was only one of them. Four save buttons would have turned one deliberate act
  into four chances to leave half the screen unsaved.
- **Three buttons that stood alone were given borders.** The ghost variant is
  legible only in company: three of them in a table's action column are
  obviously buttons, and one at the end of a list row is a caption. That is
  what the control ending a session looked like — a label. Every other ghost
  button in the tree is in a cluster and is unchanged, and the rule is written
  down in [docs/design-principles.md](docs/design-principles.md), because
  "borderless, but only in a group" is not something the next person will
  infer from the variant's name.

- **A dialog's title bar and buttons no longer scroll away.** `overflow-y-auto`
  sat on the dialog itself, so a form taller than the viewport took its own
  heading off the top and its own Save button off the bottom — leaving a
  stack of fields with nothing saying what was being registered. Only the
  body scrolls now, and the header carries a close button, which is worth
  having only because that header stays put.
- Registration is named for what it does rather than for the protocol's
  vocabulary: "Connect a SAML application" instead of "Register a service
  provider", and likewise for OIDC and CAS.
- The settings page calls its session lifetime what it is — the console's
  own — and says outright that it is not the lifetime of the OIDC tokens
  issued to applications. Those are fixed, and the field's old wording read
  as though it governed them.
- **A SAML assertion states each attribute once.** `uid` and `mail` were each
  sent twice: the assertion library derives attributes from the session it is
  handed, and Portico both filled those fields and supplied the same facts
  itself. A service provider mapping on the attribute name got whichever of
  the pair its parser kept — and which one that was is a property of the
  parser, not a choice either side made.
- Following from that, an assertion no longer carries
  `eduPersonPrincipalName`. It held a second copy of the email address, was
  never documented, and comes from a federation this product has nothing to
  do with — the library emitted it merely because an email was set. `cn` is
  unaffected and still sent: it was in every 0.1.0 assertion, and a good many
  service providers map it. What
  [docs/federation.md](docs/federation.md) lists is now what an assertion
  carries, which it was not before.

## [0.1.0] - 2026-08-09

The first version. Everything below exists, is tested, and is what a
deployment gets — the four single sign-on protocols, multi-tenancy enforced
in the query layer, directory provisioning, webhooks, and the self-service
flow.

What is deliberately absent is listed under **Known limitations** at the
foot, and it is worth reading before deploying rather than after: there are
two fixed roles and no permission model, no MFA, no TLS, and no rate
limiting. The last two are not oversights — Portico expects a reverse proxy
in front of it.

### Changed

- **Sign-in accepts a username, an email address, or a phone number.** One
  `identifier` field, one credential check, one token — the identifier is a
  way of naming an account, not a kind of sign-in. Resolution has a declared
  precedence (username, then email, then phone) because a username may look
  like an address.
- Phone and email are unique within a tenant, through partial indexes so
  that "not bound" stays the empty string and any number of accounts may
  leave either blank.
- A collision now reports which field collided rather than always saying the
  username was taken. The constraints are named in the migration and the
  service matches on the name.
- **Everything is tenant-scoped.** Users, organizations, audit entries, and
  settings all belong to exactly one tenant, and nothing crosses the
  boundary. Usernames and organization codes are unique per tenant rather
  than globally, so two tenants may each have an `admin`.
- Sign-in takes an optional tenant code. Omitting it resolves to the default
  tenant, which is what a single-tenant deployment always does.
- Settings are per tenant — every one of them, listed under Added below.
- Tokens carry the tenant, and it is checked against the account record on
  every request, so a token cannot act outside the tenant it was issued in.
- **Signing out, changing a password, and disabling an account now revoke
  the federated sessions too**, not only Portico's own. Bumping
  `token_version` never reached a relying party's refresh token, which is a
  separate credential in a separate table and would have stayed valid for
  its full month.
- Audit writes no longer inherit the request's cancellation. A client that
  closed the tab used to take the entry with it, which was worst for exactly
  the events worth recording.
- The `filters` builder used by list endpoints now binds the tenant as `$1`;
  callers write `WHERE tenant_id = $1` into their own SQL so the constraint
  is visible and testable.
- Database errors are classified by SQLSTATE rather than message text. The
  previous check matched SQLite's wording and had been returning false for
  every PostgreSQL error since the migration, turning duplicate-key
  conflicts into 500s instead of 409s.
- **Storage is PostgreSQL.** An earlier iteration used SQLite, which suited a
  single-tenant intranet tool but is the wrong shape for a multi-tenant
  identity provider other systems depend on. A deployment is now two
  processes rather than one; the binary still needs no cgo and still ships
  in a `scratch` image.
- Timestamps use `TIMESTAMPTZ` and scan directly into `time.Time`. The
  conversion layer SQLite required is gone.
- `PORTICO_DB_DSN` is required and has no default.
- **SAML service providers and CAS services are addressed by an opaque id**
  rather than by entity ID or URL prefix. Those are natural keys containing
  slashes and colons, and a reverse proxy configured the ordinary way
  normalizes the path before it arrives — measured against two real nginx
  containers, not reasoned about: the documented `proxy_pass` form works and
  a trailing slash turns every one of those routes into a 404.
- **Updating settings takes each field as optional**, leaving anything
  omitted unchanged. The endpoint replaces the whole object, so a client
  written against an older shape would omit the newer fields, Go would
  decode them as zero, and a request that meant to rename the system would
  silently switch account lockout off.
- **The hourly sweep now covers spent password resets, dead refresh-token
  chains, and ended sessions**, each after thirty days rather than at
  expiry. A refresh token is deleted only when its entire rotation chain is
  dead: expiry is checked *after* reuse when one is presented, so deleting
  expired rows individually would quietly disable reuse detection, which is
  the only thing that catches a stolen refresh token.
- **Audit entries are pruned only where a tenant configured a retention
  period.** The default is to keep everything.
- **A client that disconnects mid-request is reported as `499`, not `500`.**
  Every operation still in flight fails with `context.Canceled` when someone
  navigates away, and calling that a server error filled the log with
  entries nobody could act on and inflated the 5xx rate an operator alerts
  on. Both conditions are required — the error is cancelled *and* the
  request context is done — because an internal cancellation while the
  caller is still waiting is a real fault. A missed deadline stays a 500
  even if the client has also gone.
- `ActionDownstreamSync` is gone. It named an event the server cannot
  observe: a downstream system reads a profile once with the user's token,
  and whether it then creates an account, updates one, or discards the
  response is known only to it. Recording a read under the name of a sync
  would put an assertion in the audit trail that the server never witnessed.

### Added

- **SAML 2.0.** Portico is a SAML identity provider: metadata,
  service-provider-initiated browser SSO over both bindings, and signed
  assertions — encrypted too, whenever the service provider publishes an
  encryption key. Registration takes the service provider's metadata
  document whole rather than fields to retype.
- SAML certificates are per tenant and live in their own table, apart from
  the OIDC signing keys, because their rotation contracts are incompatible:
  a relying party refetches a key set, while a service provider has the
  certificate typed into its own configuration and no way to learn of a new
  one. Retired SAML certificates are kept indefinitely and rotation is an
  operator's decision, never a timer's.
- Nothing in Portico constructs or verifies an XML signature. goxmldsig is
  pinned ahead of what crewjam/saml resolves, because that is the code the
  whole thing rests on.
- Deliberately absent: identity-provider-initiated sign-on, which has no
  request to correlate an assertion with; and single logout, which requires
  reaching every service provider in the browser and reports having ended
  sessions it did not when it half works. The metadata says so rather than
  advertising an endpoint that 404s.
- **CAS 2.0 and 3.0.** Login, logout, and both validation endpoints, per
  tenant and at the root. Implemented directly rather than through a
  library, because CAS has no cryptography at all — a ticket is a random
  string and validation is a lookup.
- CAS service matching is a URL prefix with a boundary: a registration for
  `https://app.example.com/` can never cover
  `https://app.example.com.somewhere-else.test`. Wildcards, query strings,
  fragments, and plain http over a network are refused at registration.
- CAS tickets are single use, enforced by a conditional update rather than a
  read followed by a write, and bound to the service they were issued for.
- No CAS ticket-granting ticket: single sign-on rides on Portico's own
  session, so signing out, changing a password, and disabling an account
  already end it rather than leaving a third credential to revoke.
- `portico sp` and `portico cas` register SAML service providers and CAS
  services from the command line, alongside the console screens, for
  scripted and out-of-band setup.
- **OpenID Connect 1.0 and OAuth 2.1.** Portico is an OpenID Provider:
  discovery, authorize, token, userinfo, introspection, revocation,
  end-session, and a published key set. An application points its own OIDC
  library at the issuer and needs nothing Portico-specific.
  [docs/federation.md](docs/federation.md) is the integrator's guide.
- Each tenant is its own issuer at `/t/<code>`, with its own signing key and
  its own accounts. A token minted for one tenant is unusable against
  another because a relying party checks `iss` and fetches the key set that
  issuer names — both things every library already does, unlike a custom
  tenant claim nothing would check. The default tenant is additionally
  served at the root, so a single-tenant deployment never has to explain
  tenants to an integrator.
- Only the authorization code grant, and PKCE (`S256`) is required of every
  client including confidential ones. The implicit and hybrid flows put
  tokens in URLs, which is why OAuth 2.1 removes them.
- Refresh tokens rotate on every use. Presenting a spent one means a copy
  leaked, so the whole chain is revoked rather than the one call failing —
  which link leaked is unknowable. A refresh also re-checks that the account
  is still enabled.
- Tokens carry `tenant_id`, `tenant_code`, `role`, and the organization, in
  the ID token, the access token, and userinfo alike.
- Signing keys are per tenant, generated on first use, and rotated with
  `portico client rotate-key`. A retired key stays published for 24 hours so
  the tokens it signed keep verifying.
- OIDC clients are registered either from the console or from the command
  line — `portico client register|list|enable|disable|rotate-key`. Tenants
  remain CLI-only, because no role within a tenant could be authorized to
  create another one; an application belongs to a tenant, so a tenant's own
  administrator can manage it. Redirect URIs are matched exactly, and
  wildcards, fragments, and non-loopback `http://` are refused at
  registration wherever it happens.
- `OAUTH_AUTHORIZE` audit entries record who authorized which application,
  and when.
- Abandoned authorization requests are swept hourly.

- **Password recovery** by email, with a single-use link that expires in 30
  minutes and invalidates any earlier one. The request endpoint answers
  identically whether or not an account matched — in body and in timing,
  since everything past the account lookup happens after the response — and
  resolves against the channel's own column rather than the sign-in lookup.
  Resolving across columns and then sending a token is how one account's
  reset ends up delivered on another account's identifier.
- Delivery goes through plain SMTP (`PORTICO_SMTP_*`), so a deployment
  points at whatever relay it already has and no vendor is involved. SMS
  recovery is defined as an interface with no provider in this version;
  the endpoint reports the channel unavailable rather than accepting a
  request it cannot fulfil.
- **Self-service profile editing.** A user maintains their own display name,
  email, and phone. Role, status, organization, and username are absent from
  that endpoint on purpose. Changing a recovery destination is recorded in
  the audit trail with the old and new values, because repointing it is how
  a stolen session becomes permanent access.
- Multi-tenant isolation, enforced in the query layer. Every table carries a
  `tenant_id`; the service layer reaches the database only through a
  tenant-bound view that supplies it; and three tests hold the boundary up —
  one that fails the build on a query missing its tenant predicate, one that
  does the same for hand-written SQL, and a suite that drives the API as two
  tenants and asserts neither can reach the other's data.
- `portico tenant create | list | enable | disable` for provisioning. There
  is no API for it: no account can act outside its own tenant, so there is
  nobody the API could authorize. Each tenant gets its own administrator at
  creation.
- Local account lifecycle: create, edit, enable/disable, reset password,
  paged search by username or display name. Accounts are disabled rather
  than deleted so the audit trail stays complete.
- Bulk import from `.xlsx`, with a generated template and per-row error
  reporting. Rows are independent, so a partial import succeeds.
- Self-service registration, gated by a runtime toggle that defaults to off.
- Two fixed roles, administrator and user, with server-side enforcement on
  every administrative endpoint.
- Organizations with codes, sort order, and member counts, arranged as a
  tree. Disabling one blocks new members without detaching existing ones.
- Sign-in issuing self-signed JWTs, revoked immediately on signing out,
  changing a password, or disabling an account — see the sessions entry
  above for which of those ends one session and which ends all of them.
- Audit log covering sign-ins, operations, authorization, registrations, and
  organization changes, filterable by type, actor or target, and time range.
- Runtime settings, per tenant: system name, session lifetime, the
  registration toggle, the lockout threshold and window, the password
  policy, and the audit retention period.
- Two endpoints for downstream systems: the caller's profile with
  organization, and an administrator check.
- Web UI in English and 简体中文, with navigation driven by the caller's
  role.
- Single-binary distribution: the built frontend is embedded, and the
  container image is `scratch` plus one file.
- **Application management in the console.** Registering an OIDC client, a
  SAML service provider, or a CAS service, editing it, and enabling or
  disabling it are all screens now, each with the endpoints an integrator
  has to be given. `portico client`, `portico sp`, and `portico cas` still
  work; they are no longer the only way, which is what made every protocol
  above unusable without shell access to the server.
- **Account lockout.** An account locks after a configurable number of
  consecutive failed sign-ins, per tenant, and can be switched off. The lock
  is checked *after* the password comparison, so a wrong guess never learns
  that an account is locked, and the lock does not extend on further
  attempts — otherwise anyone could keep any account locked indefinitely.
- **Password policy: composition, history, and expiry.** All three are off
  by default, because NIST SP 800-63B recommends against the first and the
  third — they are here for deployments audited against regimes that require
  them. Both the settings screen and
  `internal/service/password_policy.go` say so.
- **Sessions are individually visible and individually revocable.** Each
  sign-in is its own row with its device, address, and last activity, and
  can be ended on its own from the profile page, along with a "sign out
  everywhere" for when one of them looks unfamiliar. An administrator can
  see and end a user's sessions from the user list, which is what the other
  end of a "my account is compromised" phone call needs. Signing out ends the
  session doing it and leaves the others; changing a password or disabling
  an account still ends every one. Federated sessions are not part of that
  distinction and always all go — "sign out" on a single sign-on system is
  read as signing out of the things you signed in to, and the surprising
  failure is the one where it did less than you thought.
- **Organizations form a tree**, with a parent that can be changed. Cycles
  are refused in the service layer — a foreign key cannot catch one, because
  every row in a cycle points at something that exists. The list can be
  searched, and flattens while filtering.
- **A readiness probe** at `/api/v1/ready`, which reaches the database, next
  to `/api/v1/health`, which deliberately does not: a database outage should
  not make an orchestrator restart every instance. The image is `scratch`
  and has no shell or curl, so `portico ready` exists as a subcommand for
  container health checks.
- **The audit trail shows what it records.** Each entry expands to the
  detail the server has been writing all along, along with the target type,
  the target id, and the actor's id.
- **Error messages in the reader's language.** Every error code the server
  can return has an English and a 简体中文 rendering; the Chinese table is
  typed against the English one, so a missing translation fails the build
  rather than showing a code to a user.
- **Webhooks.** A tenant registers an https endpoint and Portico posts a
  signed body when an account is created, updated, enabled, or disabled.
  Queued when the event happens and delivered by a worker, so the operation
  that caused it never waits for a subscriber and never fails because one is
  down. [docs/webhooks.md](docs/webhooks.md) is the subscriber's guide.
- The destination rules are not configurable, because they are what stops a
  tenant administrator from using this server as a proxy into the network it
  runs in: https only, and never loopback, a private range, link-local —
  which is where every cloud puts the metadata service that hands out
  credentials — or carrier-grade NAT. Redirects are not followed.
- **The address is checked again at connection time**, inside the dialer.
  Checking only at registration is defeated by a name that resolves publicly
  then and to 127.0.0.1 later, which needs nothing but a DNS record the
  attacker controls.
- The signature is HMAC-SHA256 over the timestamp, a dot, and the raw body.
  The timestamp is inside the signature rather than beside it: a signature
  over the body alone is replayable forever by anyone who ever saw one,
  including from the receiver's own logs.
- Retries are five attempts over about half an hour, for the failures that
  might become successes. A 4xx is not retried — the receiver understood and
  refused, and sending it again produces the same refusal. Delivery records
  are kept for thirty days and shown in the console, which is what answers
  "we are not receiving anything" without asking the receiver.
- The container image now carries a CA certificate bundle. Webhook delivery
  is this project's first outbound TLS, and a `scratch` image has no root
  store — every delivery would have failed certificate verification in a
  deployment while working on a developer's machine.
- **SCIM 2.0 provisioning** at `/scim/v2`, for users and groups, so a
  directory creates, updates, and deactivates accounts and maintains group
  membership without anyone typing any of it twice.
  [docs/scim.md](docs/scim.md) is the integrator's guide.
- **Groups are a new concept, and are not organizations.** An organization is
  where somebody sits — one of them, in a tree, with a code downstream
  systems store. A group is a set they belong to — any number of them, flat.
  Group membership is many-to-many, so storing it in the organization field
  would have meant either breaking single membership or silently reassigning
  people the first time a directory added somebody to a second group.
  Membership grants nothing: there are two fixed roles and no RBAC, so there
  is nothing for a group to carry.
- `PATCH /Groups/{id}` understands the shapes providers actually send,
  including `members[value eq "<id>"]` with the member inside the path —
  which is what Okta sends for a removal, and what a naive path lookup
  mangles — and full-set `replace`, which is how Entra reconciles. A member
  that does not exist fails the request with `invalidValue` rather than
  being skipped: a silently dropped member leaves a group that looks
  synchronized and is not.
- Provisioning authenticates with its own kind of credential, issued from
  the console and scoped to `/scim/v2`. Not an account: a directory has no
  session, no password to recover, and no way into the console, and
  modelling one as a user would leave a row every listing and every role
  check had to remember to exclude. The token is shown once and stored as a
  digest.
- `externalId` is stored and reconciled on. Re-posting a user whose
  `userName` has since changed updates that account rather than creating a
  second one, which is the most common way a SCIM integration passes
  testing and duplicates a directory in production.
- `DELETE /Users/{id}` deactivates rather than deletes — accounts are
  disabled and never deleted here — and shares its code path with `PATCH
  active=false`, so deprovisioning cannot work one way and not the other.
  Either one ends every session and every federated refresh token at once.
- A `PATCH` for an unsupported attribute answers 400 with
  `scimType: invalidPath`, naming the attribute, rather than 501: the
  difference is whether an operator's sync log says which attribute cannot
  be set or that the server is broken. A patch is applied whole or not at
  all.
- A directory may not set roles or organizations. SCIM has no notion of
  Portico's roles, so a mapping would have to be invented — and an invented
  one is a way to become an administrator by writing a directory attribute.
- **Prometheus metrics**, on a listener of their own and off unless
  `PORTICO_METRICS_ADDR` is set. HTTP counts and latencies, sign-in outcomes,
  lockouts, tokens issued, and the database pool — which is the one that
  names the failure looking like nothing else, where everything is slow, no
  request errors, and no single slow query to find. Separate listener
  because the endpoint is unauthenticated, as every Prometheus endpoint is;
  a route on the application port would be exactly as reachable as the login
  page, and the scrape config would still look right.
- No metric is labelled with a tenant or a request path. Both are values
  created from outside, and a label whose cardinality other people control
  is how a metrics endpoint becomes the largest thing a process produces.
  Routes are labelled with the chi pattern, so `/users/{id}` is one series.
- Sign-in counters are initialised to zero at startup, so a quiet instance
  reports zero rather than nothing and an alert can tell the two apart.
- **A browser test suite** in `web/e2e/`, running a real browser against the
  built binary in CI. Every test in it fails if the browser reported a
  Content-Security-Policy violation or an uncaught error, whether or not
  that test was looking — a blocked script does not fail an assertion, it
  leaves a page that renders and does nothing. That is the shape of the bug
  which broke every SAML sign-in while eleven Go tests passed.
- **A test runner for the frontend**, with the first tests written against
  the two defects a browser had already found — a `role="tab"` with no
  panel, and a status toggle addressing the wrong identifier, which the type
  checker could not see because both were strings.
- **An OpenAPI 3.1 description of the API**, at
  [docs/api/openapi.yaml](docs/api/openapi.yaml) — every operation under
  `/api/v1`, so an integrator can generate a client instead of reading
  prose. It stops at `/api/v1` on purpose: SCIM, OpenID Connect, SAML, and
  CAS have their own specifications, and a hand-maintained restatement of
  somebody else's protocol can only disagree with it.
- The document is checked both ways round against the running router by
  `TestOpenAPIDescribesEveryRoute`, and validated in CI by a real OpenAPI
  linter. The first catches an endpoint that exists without being described
  and a path described without existing; the second catches the document
  being invalid, which the first cannot see. It found a dangling `$ref` on
  its first run.
- **A home screen, for everybody.** Somebody who is not an administrator used
  to sign in and land on their own profile — true, and not the question they
  arrived with, which is what they can use. `/` now lists the applications
  they can open, their account at a glance, and their last few sign-ins.
- The applications on it are **the tenant's, not the reader's**, and the
  screen says so. This version has two fixed roles and no notion of who may
  use what, so the list is identical for everybody; implying an entitlement
  that does not exist would invite the conclusion that an application missing
  from a colleague's portal is one they were not granted.
- Application registrations gained an optional **launch address**, because
  none of the addresses already stored is one: a redirect URI, an assertion
  consumer service, and a CAS prefix are all places a protocol sends a
  browser mid-flow, and opening any of them directly produces an error.
- The launch address and the icon are settable from the command line too
  (`--launch-url`, `--logo-uri` on `client`/`sp`/`cas register`), so an
  application provisioned without touching the console still appears on the
  home screen. A guard test now checks that every environment variable the
  server reads appears both in `portico --help` and in `.env.example`;
  `PORTICO_METRICS_ADDR` was missing from the first for as long as it had
  existed.
- Registrations also gained an optional **icon**, called `logoUri` after the
  field an OAuth client's own metadata uses (RFC 7591). It may be an absolute
  http(s) address or a path on this server, and the second form is the one
  worth having: a portal that fetches logos from third parties reports every
  visitor to those hosts on every sign-in, and an offline deployment shows a
  wall of broken images. Six icons ship under `/icons`. An application
  without one gets a tile bearing the first character of its name, in a
  colour derived from that name — absence is the common case, not a defect.
- The home screen **says what somebody can act on today**: a password within
  a fortnight of expiring, and contact details missing that recovery would
  need. The second appears only where the deployment has that channel
  configured, because telling somebody to add an email address so they can
  recover their password, on a server with no mail set up, promises a
  capability that does not exist.
- `/users/me` reports **`passwordExpiresAt`**, absent when the tenant does not
  expire passwords. The instant rather than the policy: the policy is
  administrator-only, and somebody does not need to be told the tenant's
  rules to be told their own deadline. The console reads the account from
  this endpoint after signing in rather than from the sign-in response, which
  is a smaller shape — otherwise the warning arrived on the next reload
  instead of at the sign-in it is about.
- **The home screen and the profile use more than one column.** Both were a
  single stack of narrow cards on a 1440px column, which reads as a page that
  failed to load rather than as a deliberate measure — and on the home screen
  each label sat hard left with its value hard right, so ten words of content
  were arranged as a metre of white space. The forms did not get wider;
  something was put beside them.
- **Every screen is laid out in the same column**, bounded rather than
  stretching to the edge of whatever display it is on, and every screen puts
  its content on the same kind of surface. The settings form sat directly on
  the page background while the profile screen's identical fields sat in a
  card, and the profile screen itself was three narrow cards followed by one
  that ran to the far edge. Two widths — the column and the form — are now
  named in the theme instead of being `max-w-md` written out wherever
  somebody needed it.
- **The user list can be filtered by organization**, and an account now says
  which groups it is in. Both existed everywhere except the screen: the list
  endpoint had taken an `organizationId` since it was written, and
  `GET /users/{id}/groups` had a client method and two translations and no
  caller at all. They were found by looking for translation keys nothing
  renders — usually a sign of surplus, sometimes the last trace of something
  never finished.
- **Provisioning and webhooks are screens of their own**, at `/provisioning`
  and `/webhooks`, instead of sections near the bottom of the settings page.
  Issuing a credential that lets a directory create and disable every
  account in a tenant is not a preference, and a delivery history is
  something an operator comes to read rather than something they set once.
- **The navigation is grouped by the question each group answers** —
  directory, integration, audit, system — rather than by "people" and "the
  rest". Application registration used to sit under Operations beside the
  password rules, though it is the list of systems that trust this one to
  say who somebody is. A label meaning "everything else" collects whatever
  the other label will not take, and that is what it had collected.
- **Accounts a directory provisions are marked as such in the console**, and
  editing one warns that the next synchronization may overwrite the change.
  Groups have carried this since they landed; accounts knew where they came
  from and did not say. The wire type was also short a value — `source`
  listed three where the server has four — so a provisioned account was
  something the type checker believed could not arrive.

### Security

- Sign-in answers an unknown username and a wrong password identically, and
  spends the same time on both, so the endpoint cannot be used to enumerate
  accounts.
- Registration hardcodes the user role and rejects unknown request fields, so
  a sign-up cannot grant itself administrator.
- Token verification rejects any algorithm other than the one used to sign,
  including `none`.
- The last active administrator cannot be disabled or demoted, and no account
  can disable itself.
- `PORTICO_JWT_SECRET` must be at least 32 bytes; the server refuses to start
  with a shorter one, because HS256 with a low-entropy key can be
  brute-forced offline from a single captured token.
- Security headers on every response: Content-Security-Policy,
  X-Frame-Options, X-Content-Type-Options, Referrer-Policy, and `no-store` on
  API responses.
- Forwarding headers (`X-Forwarded-For`, `X-Real-Ip`) are ignored unless
  explicitly trusted, so a caller cannot forge the IP recorded against their
  own actions in the audit log.
- Release artifacts carry an SPDX SBOM, and the checksum file is signed
  keylessly through Sigstore.

### Known limitations

Portico has no TLS and no rate limiting, both deliberately. It must run
behind a reverse proxy that provides them — see
[SECURITY.md](SECURITY.md) and
[docs/access-guide.md](docs/access-guide.md).

An access token already issued cannot be withdrawn: a resource server
verifies it offline and never calls back, which is the whole reason to
federate. They last fifteen minutes, and the introspection endpoint answers
for anyone who needs to know sooner. There is no consent screen, because
every client is vetted and registered by an administrator rather than
registering itself, and there is no third party to consent to.

Neither SAML nor CAS has single logout, so ending a session in Portico does
not end a session an application created for itself after accepting an
assertion or a ticket. No identity provider can do that without a working
single-logout profile. [docs/federation.md](docs/federation.md) has the full
table of what revocation reaches, per protocol.
