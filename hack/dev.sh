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
#   hack/dev.sh --no-web     never build the frontend
#
# For frontend work run `npm run dev` in web/ alongside this: Vite serves on
# 5410 with hot module replacement and proxies /api here, so a component
# change needs no rebuild at all — seconds against tens of them.
#
# This still rebuilds the embedded console when web/ changes, because the
# alternative turned out to be worse. internal/web/dist is a build product
# and is not in git, so pulling somebody else's console work changes no file
# this script was watching: the address kept serving a UI built hours
# earlier, from a checkout that no longer existed, with nothing to say so.
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
for arg in "$@"; do
  case "$arg" in
    --reseed) reseed=true ;;
    --once) watch=false ;;
    --no-web) web=false ;;
    -h|--help) sed -n '2,26p' "$0" | sed 's|^# \{0,1\}||'; exit 0 ;;
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

# The console is compiled into the binary by go:embed, so a change to it is
# a change to the Go build's inputs — which is why this runs before the
# build below rather than beside it.
#
# The saved copy is not caution for its own sake. `npm run build` has a
# prebuild step that deletes dist/assets before tsc has said whether the
# code compiles, so a typo does not leave the previous console in place: it
# leaves an index.html pointing at files that are no longer there, which
# serves a blank page. Restoring is what makes the promise this script makes
# about Go — a failed build keeps the last good one serving — true of the
# console as well.
build_web() {
  [ "$web" = true ] || return 0

  echo "==> building the console"
  local saved
  saved="$(mktemp -d -t portico-dist)"
  cp -R "$DIST/." "$saved/" 2>/dev/null || true

  if (cd web && npm run build >/dev/null 2>&1); then
    rm -rf "$saved"
    return 0
  fi

  rm -rf "${DIST:?}/"* 2>/dev/null || true
  cp -R "$saved/." "$DIST/" 2>/dev/null || true
  rm -rf "$saved"
  echo "==> the console did not build; the previous one is still embedded." >&2
  echo "    Run 'npm run build' in web/ to see why." >&2
}

# Whether anything under web/ is newer than what was last built from it.
#
# This is the case the whole change exists for. dist is a build product and
# is not in git, so switching branches or pulling somebody else's console
# work leaves it untouched and older than the source it came from — and
# nothing about a running server would tell you.
web_is_stale() {
  [ "$web" = true ] || return 1
  [ -f "$DIST/index.html" ] || return 0
  [ -n "$(find web/src web/index.html web/vite.config.ts web/package.json \
            -newer "$DIST/index.html" 2>/dev/null | head -1)" ]
}

start() {
  if web_is_stale; then
    build_web
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
if [ "$web" = true ]; then
  echo "==> watching Go and web sources; Ctrl-C to stop"
else
  echo "==> watching Go sources; Ctrl-C to stop"
fi

mtimes() { xargs stat -f '%m' 2>/dev/null | sort -rn | head -1; }

# Two watches rather than one, because they must not see each other. The Go
# watch is confined to .go and .sql; widening it to the web extensions would
# sweep in internal/web/dist, which is what npm writes — so every console
# build would look like a source change and start another one.
newest_go() {
  find cmd internal migrations -name '*.go' -o -name '*.sql' 2>/dev/null | mtimes
}
newest_web() {
  [ "$web" = true ] || return 0
  find web/src web/index.html web/vite.config.ts web/package.json \
    -type f 2>/dev/null | mtimes
}

last_go="$(newest_go)"
last_web="$(newest_web)"
while true; do
  sleep 1
  current_go="$(newest_go)"
  current_web="$(newest_web)"

  # Either one goes through start, which builds the console first when it is
  # behind. A console change reaches the port only once something embeds it,
  # so there is no path here that skips the Go build.
  if [ "$current_web" != "$last_web" ]; then
    last_web="$current_web"
    echo "==> console change detected"
    start
    last_go="$(newest_go)"
    continue
  fi

  if [ "$current_go" != "$last_go" ]; then
    last_go="$current_go"
    echo "==> change detected"
    start
  fi
done
