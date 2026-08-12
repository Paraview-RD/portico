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
#
# For frontend work run `npm run dev` in web/ alongside this: Vite serves on
# 5410 with hot module replacement and proxies /api here, so a component
# change needs no rebuild at all. Only Go changes come through this script.
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
for arg in "$@"; do
  case "$arg" in
    --reseed) reseed=true ;;
    --once) watch=false ;;
    -h|--help) sed -n '2,17p' "$0" | sed 's|^# \{0,1\}||'; exit 0 ;;
    *) echo "dev.sh: unknown argument $arg" >&2; exit 2 ;;
  esac
done

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

start() {
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
echo "==> watching for changes to Go source; Ctrl-C to stop"
newest() {
  find cmd internal migrations -name '*.go' -o -name '*.sql' 2>/dev/null |
    xargs stat -f '%m' 2>/dev/null | sort -rn | head -1
}
last="$(newest)"
while true; do
  sleep 1
  current="$(newest)"
  if [ "$current" != "$last" ]; then
    last="$current"
    echo "==> change detected"
    start
  fi
done
