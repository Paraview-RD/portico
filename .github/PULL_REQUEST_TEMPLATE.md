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
- [ ] User-facing strings go through i18n (both `web/src/i18n/en-US.ts` and `zh-CN.ts`)
- [ ] Docs updated if this changes setup, configuration, or the API

## Security

<!-- Delete if plainly not applicable. This is an auth project, so it usually
     is applicable. -->

- [ ] This does not weaken authentication, authorization, or session revocation
- [ ] New endpoints are behind the right middleware (`RequireAuth` / `RequireAdmin`)
- [ ] No secrets, tokens, or passwords are logged or returned to the client
