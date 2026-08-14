#!/usr/bin/env bash
# Build everything the binary embeds, then fill a database worth looking at.
#
# Runs once, when the Codespace is created. Starting the server is not this
# script's job — see start.sh, which runs on every start and therefore also
# after the machine has been stopped and resumed.
#
# Ordering is the part worth knowing. cmd/seed applies migrations itself,
# exactly as the server would, and refuses a database that already holds
# accounts. So it goes first: the other way round, the server would create
# its bootstrap administrator, and seeding would then stop and say the
# database is already in use — correctly, and unhelpfully.
set -euo pipefail

cd "$(dirname "$0")/.."

say() { printf '\n\033[1m▸ %s\033[0m\n' "$1"; }

# Each step says roughly how long it takes, because two of them produce no
# output at all while they run and this terminal is the only thing a reader
# has to go on. `go build` and the seed are both several silent minutes, which
# is indistinguishable from a hang — and the environment was already being
# abandoned as broken at about that point.
cat <<'EOF'

  Building Portico. This takes 10-20 minutes on a fresh Codespace, most of
  it before this script even starts: GitHub installs Go, Node and Python
  first. Two of the steps below print nothing for minutes at a time. That is
  normal.

  The Portico port does not exist yet and will not open until the last step
  says Ready. A tab opens by itself at that point.

EOF

say "Go modules (~1 min)"
go mod download

# Both of these are compiled into the binary. A build without them produces a
# server that answers the API and serves neither a console nor a manual, which
# is a confusing thing to hand somebody who came to look at the console.
say "Console (~2 min)"
npm ci --prefix web
npm run build --prefix web

say "Manual (~1 min)"
pip install --quiet --disable-pip-version-check -r hack/docs-requirements.txt
./hack/build-docs.sh

say "Server — silent, ~3 min"
CGO_ENABLED=0 go build -o portico ./cmd/server

# Migrations and data, in one step, from a binary that is deliberately not
# part of the release image — see cmd/seed for why.
say "Accounts, organizations, applications and history — silent, ~2 min"
go run ./cmd/seed

say "Ready"
cat <<'EOF'

  Sign in with any seeded account. They all share one password:

      admin      Administrator   super administrator
      zhangwei   张伟            super administrator, with a history
      liyan      李燕            ordinary user

      password:  Portico@1

  The same name exists in tenant "acme" with almost nothing carried across,
  which is the shortest way to see what multi-tenant means here.

  Mail goes to Mailpit and stops there — open the forwarded port labelled
  "Mailpit" to read a password-reset link rather than waiting for one.

  The manual is at /docs, built from this working copy rather than from a
  release, so it describes what is in front of you.

EOF
