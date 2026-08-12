#!/usr/bin/env bash
#
# One instance, one port, rebuilt when the Go source changes.
#
# The point is that the address never moves. Verifying a change meant starting
# a server, finding a free port because the last one was still held, telling
# somebody the new number, and doing it again an hour later — so this pins
# 8140 and restarts in place instead.
#
#   hack/dev.sh              build, run, and rebuild on change
#   hack/dev.sh --reseed     drop the dev database and fill it first
#   hack/dev.sh --once       build and run, without watching
#   hack/dev.sh --no-web     never build the console
#   hack/dev.sh --no-docs    never build the manual
#
# For frontend work run `npm run dev` in web/ alongside this: Vite serves on
# 5410 with hot module replacement and proxies /api here, so a component
# change needs no rebuild at all — seconds against tens of them.
#
# Three things are compiled into the binary and only two of them are in git.
# internal/web/dist and internal/docs/site are build products, so pulling
# somebody else's console or manual work changes no file a Go watcher would
# look at: the address goes on serving a console and a manual built hours
# earlier, from a checkout that no longer exists, with nothing to say so.
# Both were caught that way rather than noticed — one by comparing a running
# binary's vcs.revision against HEAD, the other by reading a page that was
# missing a footer somebody had just added.
#
# So this reconciles all three at startup and watches all three afterwards.
# An interface that is silently out of date is harder to catch than one that
# is visibly broken.
set -euo pipefail

cd "$(dirname "$0")/.."

# Pinned, all of it. A development instance whose port, database and keys
# change between runs is one that cannot be bookmarked, and re-keying is not
# free: the encryption key seals directory bind passwords and webhook
# headers, so a fresh one on every start leaves the seeded ones unopenable.
PORT="${PORTICO_DEV_PORT:-8140}"
DB="${PORTICO_DEV_DB:-portico_seed}"
DB_HOST="${PORTICO_DEV_DB_HOST:-localhost:5443}"
DB_USER="${PORTICO_DEV_DB_USER:-portico}"
DB_PASSWORD="${PORTICO_DEV_DB_PASSWORD:-portico}"

export PORTICO_ADDR=":${PORT}"
export PORTICO_PUBLIC_URL="http://localhost:${PORT}"
export PORTICO_DB_DSN="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}/${DB}?sslmode=disable"
# Development values, fixed on purpose and useless anywhere else: they are in
# a file in the repository, which is the whole reason they must never be a
# deployment's.
export PORTICO_JWT_SECRET="${PORTICO_JWT_SECRET:-portico-development-only-jwt-secret-never-deploy-this}"
# Hexadecimal, because the server checks: 32 bytes of it. Spelled from a word
# so that nobody mistakes it for one that was generated.
export PORTICO_ENCRYPTION_KEY="${PORTICO_ENCRYPTION_KEY:-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef}"

BINARY="$(mktemp -t portico-dev)"
PID=""

reseed=false
watch=true
web=true
docs=true
for arg in "$@"; do
  case "$arg" in
    --reseed) reseed=true ;;
    --once) watch=false ;;
    --no-web) web=false ;;
    --no-docs) docs=false ;;
    # Read to the first line that is not a comment, rather than to a line
    # number. The number was already wrong once: the header grew and --help
    # went on printing the first seventeen lines of a twenty-six line
    # explanation, cut mid-sentence, which is the kind of thing nobody
    # reports because it looks deliberate.
    -h|--help) awk 'NR>1 && /^#/ {sub(/^# ?/, ""); print; next} NR>1 {exit}' "$0"; exit 0 ;;
    *) echo "dev.sh: unknown argument $arg" >&2; exit 2 ;;
  esac
done

if [ "$web" = true ] && ! command -v npm >/dev/null 2>&1; then
  # Not fatal. The point of this script is that a checkout with nothing but
  # Go still runs, and a server with a stale console is more useful than no
  # server — as long as it says which one it is.
  echo "==> npm not found; leaving the embedded console as it is" >&2
  web=false
fi

if [ "$docs" = true ] && ! command -v mkdocs >/dev/null 2>&1; then
  # Also not fatal, but it degrades worse than the console does, so it says
  # which of the two situations this is. A stale manual is still a manual; a
  # manual that was never built is a /docs/ that answers 404, and somebody
  # who hits that should not have to work out that the cause is a missing
  # Python package rather than a broken route.
  if [ -s internal/docs/site/index.html ]; then
    echo "==> mkdocs not found; leaving the embedded manual as it is" >&2
  else
    echo "==> mkdocs not found and no manual has been built; /docs/ will 404." >&2
    echo "    pip install -r hack/docs-requirements.txt, then hack/build-docs.sh" >&2
  fi
  docs=false
fi

stop() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
}
cleanup() { stop; rm -f "$BINARY"; }
trap cleanup EXIT INT TERM

psql_admin() {
  # Through the container rather than a local psql, which is not something
  # this repository asks anybody to install.
  docker exec -i portico-test-db psql -U "$DB_USER" -d postgres "$@"
}

if [ "$reseed" = true ]; then
  echo "==> recreating ${DB}"
  psql_admin -c "DROP DATABASE IF EXISTS ${DB} WITH (FORCE)" >/dev/null
  psql_admin -c "CREATE DATABASE ${DB}" >/dev/null
  go run ./cmd/seed --quiet
fi

DIST="internal/web/dist"
SITE="internal/docs/site"

# Rebuild a directory the binary embeds, and put the old one back if the
# build fails.
#
# The saved copy is not caution for its own sake. Both builders clear their
# output before they know whether the build will succeed — npm through a
# prebuild step that deletes dist/assets before tsc has looked at anything,
# mkdocs by cleaning site_dir on the way in. So a typo does not leave the
# previous one in place: it leaves an index.html pointing at files that are
# gone, which serves a blank page rather than an error. Restoring is what
# makes the promise this script already made about Go — a failed build keeps
# the last good one serving — true of the other two as well.
#
# One helper rather than two nearly identical blocks, because the point is
# that the console and the manual behave the same way. Two copies would
# eventually stop.
rebuild_into() {
  local dir="$1" label="$2" hint="$3"
  shift 3

  echo "==> building the ${label}"
  local saved
  saved="$(mktemp -d -t portico-build)"
  cp -R "$dir/." "$saved/" 2>/dev/null || true

  if "$@" >/dev/null 2>&1; then
    rm -rf "$saved"
    return 0
  fi

  rm -rf "${dir:?}/"* 2>/dev/null || true
  cp -R "$saved/." "$dir/" 2>/dev/null || true
  rm -rf "$saved"
  echo "==> the ${label} did not build; the previous one is still embedded." >&2
  echo "    ${hint}" >&2
}

npm_build() { (cd web && npm run build); }

build_web() {
  [ "$web" = true ] || return 0
  rebuild_into "$DIST" console "Run 'npm run build' in web/ to see why." npm_build
}

build_docs() {
  [ "$docs" = true ] || return 0
  rebuild_into "$SITE" manual "Run hack/build-docs.sh to see why." ./hack/build-docs.sh
}

# Whether a source tree is newer than what was last built from it.
#
# This is the case the whole arrangement exists for. dist and site are build
# products and are not in git, so switching branches or pulling somebody
# else's work leaves them untouched and older than the source they came
# from — and nothing about a running server would tell you. A missing marker
# counts as stale, which is what makes a fresh clone build both.
newer_than() {
  local marker="$1"
  shift
  [ -s "$marker" ] || return 0
  [ -n "$(find "$@" -newer "$marker" 2>/dev/null | head -1)" ]
}

web_is_stale() {
  [ "$web" = true ] || return 1
  newer_than "$DIST/index.html" \
    web/src web/index.html web/vite.config.ts web/package.json
}

docs_is_stale() {
  [ "$docs" = true ] || return 1
  newer_than "$SITE/index.html" docs mkdocs.yml
}

start() {
  # Both before the Go build, not beside it: go:embed makes the console and
  # the manual inputs to it, so a rebuilt one reaches the port only through
  # a rebuilt binary.
  if web_is_stale; then
    build_web
  fi
  if docs_is_stale; then
    build_docs
  fi

  echo "==> building"
  if ! go build -o "$BINARY" ./cmd/server; then
    # Deliberately not fatal. A build that fails while something is already
    # listening should leave it listening: the alternative is that one typo
    # takes the instance away and the address stops being dependable, which
    # is the thing this script exists to provide.
    echo "==> build failed; the previous build is still serving" >&2
    return
  fi
  stop
  "$BINARY" &
  PID=$!
  echo "==> http://localhost:${PORT}  (db ${DB}, pid ${PID})"
}

start

if [ "$watch" = false ]; then
  wait "$PID"
  exit 0
fi

# Polling rather than fswatch or air, so that this works on a checkout with
# nothing installed but Go. A second of latency on a rebuild that takes
# several is not worth a dependency.
# Spelled out with if rather than `[ … ] && …`, which under `set -e` exits
# the script the first time the condition is false.
watching="Go"
if [ "$web" = true ]; then watching="${watching}, web"; fi
if [ "$docs" = true ]; then watching="${watching}, docs"; fi
echo "==> watching ${watching} sources; Ctrl-C to stop"

mtimes() { xargs stat -f '%m' 2>/dev/null | sort -rn | head -1; }

# Three watches rather than one, because they must not see each other. The Go
# watch is confined to .go and .sql: widening it to the web or markdown
# extensions would sweep in internal/web/dist and internal/docs/site, which
# are what the other two builders write — so every console or manual build
# would look like a source change and start another one.
newest_go() {
  find cmd internal migrations -name '*.go' -o -name '*.sql' 2>/dev/null | mtimes
}
newest_web() {
  [ "$web" = true ] || return 0
  find web/src web/index.html web/vite.config.ts web/package.json \
    -type f 2>/dev/null | mtimes
}
newest_docs() {
  [ "$docs" = true ] || return 0
  find docs mkdocs.yml -type f 2>/dev/null | mtimes
}

last_go="$(newest_go)"
last_web="$(newest_web)"
last_docs="$(newest_docs)"
while true; do
  sleep 1
  current_go="$(newest_go)"
  current_web="$(newest_web)"
  current_docs="$(newest_docs)"

  # All of them go through start, which rebuilds whichever is behind before
  # the Go build. A console or manual change reaches the port only once
  # something embeds it, so there is no path here that skips that build.
  if [ "$current_web" != "$last_web" ] || [ "$current_docs" != "$last_docs" ]; then
    if [ "$current_web" != "$last_web" ]; then echo "==> console change detected"; fi
    if [ "$current_docs" != "$last_docs" ]; then echo "==> manual change detected"; fi
    last_web="$current_web"
    last_docs="$current_docs"
    start
    # Taken after the builds, not before: they write into internal/, and a
    # value read earlier would make the next tick see their output as a
    # source change.
    last_go="$(newest_go)"
    continue
  fi

  if [ "$current_go" != "$last_go" ]; then
    last_go="$current_go"
    echo "==> change detected"
    start
  fi
done
