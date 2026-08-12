# Internationalization Conventions

The interface ships in English and Simplified Chinese, switchable at
runtime. English is the source language; every key is defined there first.

Adding a screen without following these is the easiest way to create work
for someone later, because retrofitting strings after the fact means
revisiting every component.

## Where translations live

```
web/src/i18n/
  en-US.ts           source language — every key is defined here
  zh-CN.ts           typed against en-US
  errors-en-US.ts    one entry per error code the API can return
  errors-zh-CN.ts    typed against errors-en-US
  index.tsx          the provider, the t() function, language detection
```

`zh-CN.ts` is declared as `Record<keyof typeof enUS, string>`, so **a
missing or misspelled key is a compile error**, not a raw key rendered in
the interface at runtime. The error tables are typed the same way, for the
same reason.

Prose is translated as a sibling file rather than a key: `docs/ldap.md` and
`docs/ldap.zh.md`. `hack/untranslated.sh` counts those, and CI prints the
count — a page with no translation does not fail the build, because then no
batch of translations could ever be merged.

`README.zh.md` follows the same naming and is **outside** that script, which
only looks in `docs/`. Nothing counts it, so what keeps it honest is that it
is named in the Go tests that pin a document to the thing it describes: the
toolchain versions, the layout diagram, the example connection string, and
the seeded demo password. Anything factual added to one README wants either
the same line in the other or a test that notices.

Verify with `npm run typecheck`. Note that a bare `npx tsc --noEmit` checks
*nothing* here — the root `tsconfig.json` is a project-references stub with
`"files": []`, so it silently type-checks zero files. Use the script.

## Keys

- **Dot-separated, `camelCase` segments, grouped by area**:
  `users.colUsername`, `login.sessionExpired`, `common.cancel`.
- **The first segment is the screen or shared area** — `common`, `nav`,
  `login`, `register`, `users`, `organizations`, `auditLogs`, `settings`,
  `profile`. Do not invent a new top-level prefix for a single string; put
  it in the screen that owns it.
- **A key names its role, not its text.** `users.confirmDisable`, not
  `users.areYouSureYouWantToDisable`. When the wording changes — and it
  will — the key should still make sense.
- **A key must never contain business data.** `role.SUPER_ADMIN` is
  correct because the role is a fixed enumeration defined by the system.
  A key built from a username or an organization name is not.

### Keys derived from enumerations

Where the server returns a stable enum value, the key is built from it:

```tsx
t(`status.${user.status}`)      // status.ACTIVE / status.DISABLED
t(`auditLogs.kind.${log.kind}`) // auditLogs.kind.LOGIN, …
```

This is the reason the API returns `ACTIVE` rather than "Active": **the
server sends codes, the client renders them**. A server that returns
display text has made itself monolingual, and no amount of frontend work
fixes it.

## Placeholders

Positional, `{0}`-style, substituted by argument order:

```ts
'common.pageOf': 'Page {0} of {1}',
```

```tsx
t('common.pageOf', page, lastPage)
```

Named placeholders read better, but positional ones survive translation
into a language that needs a different word order, without the translator
having to preserve identifiers. The tradeoff is that argument order is
load-bearing — so keep the count small. A string needing five
substitutions is usually two strings.

**Never build a sentence by concatenation.** `t('greeting') + name` breaks
in any language where the name does not come last. Put the placeholder in
the string.

## What does not get translated

- **Server-side messages and logs.** The API's `message` is English; logs
  are English. The interface localizes by `code`, through the error tables
  above — see [error-conventions.md](error-conventions.md). A code with no
  entry falls back to the server's English `message` rather than to a blank
  or to the code itself: the reader gets a sentence in the wrong language,
  which is worse than a translation and much better than nothing.
- **The detail inside a message, for a handful of codes.** A rejected
  redirect URI, an entity id that did not match, the row a bulk import
  failed on — the translation cannot carry those, and dropping them makes
  the error tidier and useless. Those codes are listed in
  `codesCarryingDetail`, and the server's own text follows the translation
  in parentheses. Add a code there only when the specifics are what the
  reader has to act on.
- **Enumeration values on the wire.** `ACTIVE`, `SUPER_ADMIN`, `LOGIN`.
  Codes are the contract; text is the presentation.
- **User-entered data.** A display name, an organization name, and a
  remark are data. They are shown as stored.
- **Identifiers in the audit log.** Action names like `USER_CREATE` are
  rendered verbatim, in a monospace column, deliberately: they are what an
  operator would grep the server logs for, and translating them would break
  that correspondence.

## Formatting values

- **Timestamps** arrive as ISO 8601 UTC and are formatted in the browser
  with `toLocaleString()`, so they follow the reader's locale and time
  zone. Never send a preformatted date string from the server — it commits
  to one locale and one zone for every reader.
- **Numbers** likewise: send the number, format at the edge.

## Safety

- Translations are rendered as text. Nothing in this project uses
  `dangerouslySetInnerHTML`, and a translation must never be the reason to
  introduce it — a resource bundle is exactly the kind of file that gets
  edited by someone who is not thinking about script injection.
- **No personal data in a translated string or its arguments.** A
  placeholder receives a display name where the interface already shows it;
  it must not carry an email, a phone number, a token, or an identifier the
  reader is not already entitled to see.

## Adding a string

1. Add the key to `en-US.ts`, in the group for the screen that owns it.
2. Add the same key to `zh-CN.ts`. Skipping this fails the build, which is
   the intent — an untranslated interface should not be shippable by
   accident.
3. Use it through `t()`. Never write a literal string in a component; that
   is the one rule whose violations are hardest to find later.

## Adding an error code

A new code on the server needs the same two entries, in
`errors-en-US.ts` and `errors-zh-CN.ts`. Omitting them does not fail the
Go build — nothing on the server knows the tables exist — so it is the
frontend that has to be looked at, and the type on the Chinese table is
what catches the second half of the mistake once the first half is done.

Write the English entry as a sentence to a person who has just been
stopped, not as a restatement of the code. `USER_LAST_ADMIN` renders as
"The last administrator cannot be disabled", not "user last admin".
