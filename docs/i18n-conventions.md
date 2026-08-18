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

## One Chinese word per concept

The console decides. A reader who meets a term on screen and then in the
manual is meeting the same product, and the screen is where they meet it
first — so where the two disagree, the manual is what changes.

| Concept | 简体中文 | Where it is settled |
|---|---|---|
| access token | 访问令牌 | `web/src/i18n/zh-CN.ts` |
| refresh token | 刷新令牌 | `web/src/i18n/zh-CN.ts` |
| ID token | ID 令牌 | `web/src/i18n/zh-CN.ts` |

**A document may give the English once**, at the term's first appearance,
as 访问令牌（access token）. After that it is Chinese. The gloss is not
decoration: these three are wire vocabulary — an integrator reading this
page is looking at `refresh_token` in a JSON body and at somebody else's
English specification, and a Chinese-only manual would make them guess
which of the two documents is talking about the same thing.

This list is short because it is enforced. `TestTheDocumentsUseTheConsolesChineseTerms`
reads this table, checks each Chinese term against the console bundle, and
checks the Chinese manual for bare English outside of code — so a row added
here is a row that has to be true everywhere, and a row that stops being
true fails the build. Add a term when the same concept has been written two
ways, not in anticipation.

Code is exempt, deliberately and by construction: fenced blocks and
backticked spans are removed before the check looks. `refresh_token` is a
grant type and a JSON field, and a manual that rendered it in Chinese would
be describing a request nobody can send.

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

## How a string is written

This governs both catalogues, not the translation. The register slipped in
English first and the Chinese followed it there.

A string in these files is read by somebody who is halfway through doing
something. It has one job: tell them what the constraint is and what happens
next. Three habits stop it doing that, and all three were live in this
repository until they were pointed at.

**Do not explain the reasoning to the reader.** The comments in this codebase
argue for their own decisions at length, and that is deliberate — a
maintainer needs the argument. Somebody filling in a form does not. The
argument in a hint is the author still in the room.

| Was | Is |
|---|---|
| A typo here is the one mistake this page cannot undo. | The confirmation link and the administrator credentials are both sent to this address. Please check it is correct. |
| Nothing is created until you open it. | The tenant is created when the link is opened. |
| They will hear it from a sign-in screen rather than from you. | …and none of them is notified. |

The first column is not wrong. It is an observation about the design, and it
costs the reader a sentence they have to interpret before they learn where
the email goes.

**Do not write as if speaking.** 「登录时填的就是它」, "Wrong address? Go back
and change it", 「先别关掉页面」, 「去查收邮件」. A rhetorical question, a
掉/去 tacked onto a verb, 就是 — each is a register a product interface does
not use, and in Chinese the effect is stronger than in English because the
spoken forms are more marked.

**Do not compress into fragments.** 「填两项，提交」 and "Fill this in" save
three characters and leave the reader deciding what was elided. 「填写并提交」
and "Complete and submit" say it.

What good looks like, then: state the constraint (`仅限小写，创建后不可修改`),
state the consequence (`该租户下所有人将被立即登出`), and stop. Where a warning
must be given, give it as a fact rather than as drama —
`此处为演示环境，非正式部署。请勿录入真实数据。`

Two things this is not. It is not a ban on warmth: `你的租户已就绪` stays.
And it does not reach the manual — the documentation's essayistic voice is
the project's own and is consistent across every page, which is a different
register for a different reader with a different amount of time.

### One word per concept, including where the word is ordinary

`撤销` was rendering both *revoke* and *undo*. The collision surfaced in one
sentence: 「撤销被撤销了」 for "Revocations are undone." Two words in the
source, one in the translation, and a reader left to work out which was
which.

So: **吊销** for the security sense — tokens, sessions, credentials, access —
and **撤销** only for reversing an action, which is the sense next to a delete
button (`不可撤销`).

**This one is not guarded, and the reason is worth knowing before somebody
assumes it is.** The glossary above is enforced by
`TestTheDocumentsUseTheConsolesChineseTerms`, but that test looks for bare
*English* in Chinese prose. It cannot see a Chinese page that says 撤销 where
it means 吊销, because both are Chinese and both are legitimate words in this
product. A check keyed on proximity — 撤销 near 令牌 — would fire on
「删除后不可撤销」 in a screen about tokens, which is correct text. So this
rule is held by review, and a relapse looks like nothing.

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
