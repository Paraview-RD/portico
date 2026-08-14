# Organizations and groups

Two ways of arranging people, and they answer different questions. Getting
which is which right at the start is worth more than anything else on this
page, because moving somebody later is easy and moving a *concept* later is
not.

| | Organization | Group |
|---|---|---|
| Answers | Where somebody sits | What sets they belong to |
| How many each | Exactly one, or none | Any number |
| Shape | A tree | Flat |
| Usually mirrors | The reporting structure | Projects, systems, approvers |
| Grants | Nothing | Nothing |

**Neither grants anything.** This version has two fixed roles —
administrator and user — set on the account itself. Putting somebody in an
organization or a group changes what downstream systems know about them and
changes nothing about what they may do here. That is worth saying plainly
because it is the assumption people arrive with.

## Organizations

Exactly one, or none, arranged as a tree, usually matching the real
departments.

The **code** is what downstream systems store and what an import file names,
so it cannot be changed after creation. The name can.

An organization can be **moved** by changing its parent. A move that would
put one inside its own subtree is refused — a foreign key cannot catch that,
because every row in a cycle satisfies it individually. The tree is bounded
at ten levels deep, which is far past any chart anybody navigates
willingly.

**Multiple roots are normal.** A deployment with three unrelated divisions
does not need an artificial "Company" above them.

### Disabling one

Disabling blocks new members. It does not detach the existing ones, and that
is deliberate: removing them would erase the record of who belonged where,
which is the question asked afterwards.

### The manager, and attachments

An organization can name whoever is **responsible** for it, and a person can
be **attached** to organizations beside the one they belong to.

Both are descriptive. Neither is a permission, and neither moves the primary
membership — that stays the single thing SCIM writes and an export names. A
field that quietly became a third role would be a permission model nobody
designed.

### Administrators, recorded before they can act

An organization can also name **administrators**, each with a scope: this
organization, or this organization and everything under it.

**Nobody named there can do anything today.** No authorization decision in
this version reads those records, and a test in the source requires that to
stay true. What they are for is the delegated administration on the roadmap:
a chart is entered by people over months, and a feature that arrives to an
empty table makes every deployment start by re-entering what it already
knows. So the place to write it exists first.

Two things are asked for at the time of recording because neither can be
recovered afterwards. **The scope** — a record that did not say whether it
meant one organization or a whole branch is one nobody can interpret when
the feature that reads it ships. **Who recorded it** — this becomes a
privilege grant, and provenance can only be written as it happens.

Changing a scope is a removal followed by a new record, so that both appear
in the audit trail as the decisions they were. Removing somebody's account
does not remove their assignments; the list shows the account's status
instead, because an assignment that vanished on a suspension would come back
on its own at a reinstatement and nobody would have decided either.

### When a directory owns the chart

Where accounts synchronize from Active Directory or OpenLDAP, the tree is
usually maintained there too. Editing it here is overwritten on the next
run. See [Reading a directory](ldap.md).

## Groups

An overlapping label: project members, approvers, the people who use a
particular system. Somebody belongs to as many as apply, and groups have no
hierarchy between them.

Membership carries no meaning here — it is carried by whatever reads it. An
application may decide that anyone in `finance` can see the reports; that
rule lives in the application. Portico's part is saying who is in `finance`
and telling the application when that changes, which is what
[webhooks](webhooks.md) are for.

### Usually pushed rather than typed

In most deployments an upstream directory maintains these over SCIM, and
creating them by hand suits the case where Portico is itself the only
source. See [Provisioning](scim.md), which also documents the `PATCH`
shapes real providers send — including the ones a naive implementation
mangles.

## Choosing between them

Ask what happens when the answer changes.

- Somebody transfers between departments: their **organization** changes.
  One value, one event, and every downstream system agrees afterwards.
- Somebody joins a project without leaving their department: a **group**.
  Storing this as an organization would mean either breaking single
  membership or silently reassigning them out of their department, which is
  what happens when a directory adds them to a second one.

If a thing has to be true of somebody *in addition to* where they sit, it is
a group. If it replaces where they sit, it is an organization.
