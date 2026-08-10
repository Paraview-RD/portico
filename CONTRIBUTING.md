# Contributing to Portico

Thanks for your interest in contributing. This project is licensed under
[Apache License 2.0](./LICENSE); by submitting a contribution you agree it
will be licensed under the same terms (see License §5, "Submission of
Contributions").

## Developer Certificate of Origin (DCO)

Every commit must be signed off to certify you wrote it or otherwise have
the right to submit it under the project's license. Add `-s` when
committing:

```
git commit -s -m "your message"
```

This appends a `Signed-off-by: Your Name <you@example.com>` trailer to the
commit message. Commits without this trailer will not be merged.

The full certification you're agreeing to (Developer Certificate of Origin,
Version 1.1):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

## Getting started

Prerequisites: **Go 1.26+** (what `go.mod` declares, and what the release
image builds with), **Node 22+**, and — only if you change a SQL query —
[`sqlc`](https://sqlc.dev).

```bash
git clone https://github.com/Paraview-RD/portico.git
cd portico

# The frontend compiles into the binary, so build it first. A Go-only build
# produces a working API and no UI.
cd web && npm ci && npm run build && cd ..

go build -o portico ./cmd/server
go test ./...
```

Run it with a real signing secret — the server refuses to start with a weak
one:

```bash
PORTICO_JWT_SECRET=$(openssl rand -hex 32) ./portico
```

It prints a generated administrator password on first start. The UI is on
<http://localhost:8410>.

### Working on the frontend

```bash
cd web && npm run dev     # http://localhost:5410, proxies /api to :8410
```

Run the Go server separately in another terminal; the dev server hot-reloads
and does not need a rebuilt binary.

### Working on queries

Queries live in `internal/store/queries/` and are compiled to Go by sqlc.
After editing one:

```bash
sqlc generate
```

The generated code in `internal/store/sqlcgen/` is committed, so contributors
who do not touch queries never need sqlc installed.

Every query on a tenant-scoped table must constrain `tenant_id`, and a test
fails the build if one does not — see
[database-conventions.md](docs/database-conventions.md#tenant-isolation)
before adding one.

### After editing a migration

While the schema is unreleased, changes go into `00001_init.sql` rather than
a new migration. goose records `00001` as applied, so **your local database
will not pick the change up** and you will debug a missing column against a
file that already has it. Drop and recreate the database:

```bash
dropdb portico && createdb portico
```

The tests are unaffected — each one gets a fresh database — so a green
`go test` will not warn you about this.

### Before you open a pull request

CI runs all of these, so it is quicker to run them yourself:

```bash
gofmt -l .                                       # must print nothing
go vet ./...
go test ./...
golangci-lint run ./...

cd web
npm run typecheck        # tsc -b; plain `tsc --noEmit` checks nothing here
npx prettier --check "src/**/*.{ts,tsx,css}"
npm run lint
npm test                 # vitest, jsdom
npm run build
```

`npm test` runs component tests under jsdom, which is not a browser. A
Content-Security-Policy that blocks the page passes a jsdom test exactly as
happily as it passed eleven Go tests once — a real bug this project shipped
into a browser and nowhere else. So these tests hold the things nobody
re-checks by hand after the first look, and they do not stand in for opening
the page.

### Browser tests

The suite in `web/e2e/` is what does stand in for opening the page. It runs a
real browser against the real binary, and every test in it fails if the
browser reported a Content-Security-Policy violation or an uncaught error,
whether or not that test was looking for one — because a blocked script does
not fail an assertion, it just quietly does not run.

It needs a built binary and a database of its own:

```bash
createdb portico_e2e                             # or any empty database

cd web
npm run build                                    # embeds the frontend
(cd .. && CGO_ENABLED=0 go build -o portico ./cmd/server)
npx playwright install chromium                  # first time only

PORTICO_E2E_DB_DSN='postgres://…/portico_e2e?sslmode=disable' npm run e2e
```

Playwright starts and stops the server itself, on port 8411, with a fixed
throwaway administrator password. It will not reuse a server already
listening there — these tests write, and a server you did not start may be
pointed at a database you care about.

Rebuild the binary after changing frontend code. The tests run against what
is embedded in it, so a stale binary silently tests the previous version;
that is the one confusing failure mode of this setup.

## Pull requests

- Keep PRs scoped to one concern.
- Add/update tests for any behavior change.
- Describe the "why", not just the "what", in the PR description.
