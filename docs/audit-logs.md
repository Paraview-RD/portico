# Audit logs

What is recorded has already happened — who signed in when, who changed
whom, what was touched. It is for finding out afterwards rather than
watching in real time.

**Entries are written and never edited, and there is no delete in the
interface.** That is what makes them worth anything as evidence, and it is
why this system disables accounts rather than deleting them: a disabled
account keeps its history, and a deleted one would leave the trail pointing
at nothing.

Retention is set on [Settings](settings.md#irreversible-settings). The
default keeps everything, which is the only safe default — the trail is a
record, not an operational buffer.

## What it will not tell you

An audit entry records what this server did or witnessed. It does not record
what a downstream system did afterwards with a profile it read: whether it
created an account, updated one, or discarded the response is known only to
it. An entry claiming otherwise would be an assertion the server never
witnessed.
