## What and why

<!-- What changes, and what problem it solves. The "why" is the part reviewers
     cannot reconstruct from the diff. -->

## How it was verified

<!-- What you actually ran, and what it showed. "Tests pass" is less useful
     than "added a test that fails without the fix". -->

## Checklist

- [ ] Every commit is signed off (`git commit -s`) — this is enforced by CI,
      see [CONTRIBUTING.md](../CONTRIBUTING.md)
- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` is clean
- [ ] Frontend changes: `npm run build` and `npx prettier --check "src/**/*.{ts,tsx,css}"` pass
- [ ] Behavior changes have tests
- [ ] User-facing strings go through i18n in both bundles — see
      [docs/i18n-conventions.md](../docs/i18n-conventions.md)
- [ ] No secrets, tokens, or personal data in new log statements — see
      [docs/logging-conventions.md](../docs/logging-conventions.md)
- [ ] Schema changes follow [docs/database-conventions.md](../docs/database-conventions.md)
      and add a migration rather than editing a released one
- [ ] Docs updated if this changes setup, configuration, or the API

## Security

<!-- Delete if plainly not applicable. This is an auth project, so it usually
     is applicable. -->

- [ ] This does not weaken authentication, authorization, or session revocation
- [ ] New endpoints are behind the right middleware (`RequireAuth` / `RequireAdmin`)
- [ ] Errors returned to clients carry no internal detail — see
      [docs/error-conventions.md](../docs/error-conventions.md)
