# The Portico console

The web UI, built with Vite and compiled into the server binary. There is no
separate front-end deployment: `npm run build` writes to
`../internal/web/dist`, and `go:embed` puts it inside `portico`.

## Working on it

Run the server on 8410 in one terminal and Vite on 5410 in another. Vite
proxies `/api` to the server, so a component change is a hot reload rather
than a Go build.

```bash
../hack/dev.sh          # server on 8410, rebuilt when Go, web or docs change
npm run dev             # Vite on 5410, with hot module replacement
```

`hack/dev.sh` alone is enough if you are not touching this directory — it
builds the console too, and reconciles it at startup so the address never
serves a bundle from a checkout that no longer exists.

## Before you push

Four commands, and CI runs all four:

```bash
npm run typecheck       # tsc -b --force
npm run lint            # oxlint
npm run format:check    # prettier --check
npm test                # vitest run
```

`format:check` is the one that gets skipped. It is also the one that fails in
CI, so run it.

## The two test suites, and why there are two

`vitest` tests components against jsdom. Fast, and blind to anything only a
real browser enforces — a Content-Security-Policy set by Go middleware once
blocked every SAML sign-in while these and eleven Go tests all passed.

`npm run e2e` drives Chromium against **the built binary**, which is the
shape users deploy: the console, the manual and the API served by one
process. That is why it builds Go rather than pointing at the dev server.

## Conventions

- One router, no library — see `src/router.tsx` for why.
- Every string goes through `src/i18n`; the English bundle is the key set and
  the Chinese one is typed against it, so a missing translation fails the
  build rather than rendering a key.
- The API layer is `src/api/`, one function per endpoint, and the envelope is
  unwrapped in exactly one place.

[docs/code-conventions.md](../docs/code-conventions.md) covers the rest, and
applies to both languages in this repository.
