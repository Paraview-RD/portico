# Settings and the audit trail

Everything on the settings screen is a **tenant-wide default that takes
effect for everybody the moment it is saved**. There is no staged rollout and
no dry run, so the two irreversible ones below are worth reading before
touching.

## The two that cannot be undone

**Lowering audit retention deletes.** Entries past the new limit are removed
permanently by the next sweep, with no copy kept. Export anything needed
long-term first.

**Raising the maximum session age above zero ends sessions that are
working.** It is measured from the sign-in rather than from the last
refresh, so integrations that have been running for months reach it
immediately. Zero — the default — switches it off.

Everything else is reversible: tightening a password rule does not
invalidate existing passwords, it only applies to the next change.

## Two different things are called a session

This confuses people, and the two are unrelated.

| | What it governs |
|---|---|
| **Console session lifetime** | How often *this interface* asks you to sign in again |
| **Single sign-on tokens** | What Portico issues to the applications connected to it |

An administrator shortening the first because "sessions should be shorter"
has changed nothing about the applications, and vice versa. See
[Federation](federation.md) for what the second group means to an
integrator.

## Sign-in security

**Lockout** locks an account after a number of consecutive failed sign-ins,
counted over a window. Zero switches it off.

It is not rate limiting, and the two cover different attacks: lockout stops
one account's password being guessed, and does nothing about the load of a
large number of attempts. Keep the throttling in your reverse proxy — see
[Getting in](access-guide.md).

The lock is checked *after* the password is compared, so a wrong guess never
learns that an account is locked. It does not extend on further attempts,
because otherwise anybody could keep any account locked indefinitely.

**Password policy** — composition, history, and expiry — is off by default.
Composition rules and forced expiry make passwords more guessable rather
than less, and NIST SP 800-63B recommends against both. They are here for
deployments audited against regimes that require them. If you have the
choice, leave them off and raise the minimum length.

## The language of what this tenant sends

`Default language` is the language of a password-reset link or a
registration confirmation sent to somebody who has stated no preference of
their own. Empty means "follow the deployment's `PORTICO_DEFAULT_LOCALE`",
and that is a real value rather than an absence: a tenant that has said
nothing follows a deployment that changes its mind later, instead of being
frozen at whatever it was the day it was created.

It does not affect the console. That is each reader's own choice and is
remembered in their browser.

## The explanation panels

`Show the explanation on each screen` offers the panel at the top of each
administrative screen. On by default; turn it off once your operators know
the product.

Each panel can also be collapsed individually, which is remembered per
browser rather than for everybody — so an operator who reads them daily can
put one away without deciding for the rest of the tenant.

## The audit trail

What is recorded has already happened — who signed in when, who changed
whom, what was touched. It is for finding out afterwards rather than
watching in real time.

**Entries are written and never edited, and there is no delete in the
interface.** That is what makes them worth anything as evidence, and it is
why this system disables accounts rather than deleting them: a disabled
account keeps its history, and a deleted one would leave the trail pointing
at nothing.

Retention is the setting above. The default keeps everything, which is the
only safe default — the trail is a record, not an operational buffer.

### What it will not tell you

An audit entry records what this server did or witnessed. It does not record
what a downstream system did afterwards with a profile it read: whether it
created an account, updated one, or discarded the response is known only to
it. An entry claiming otherwise would be an assertion the server never
witnessed.
