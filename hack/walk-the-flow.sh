#!/usr/bin/env bash
# Walk one account through every hop, asserting at each one.
#
# The test suite proves each piece to a test runner. This proves the pieces
# fit, against a real directory and a real inbox, on the deployment in front
# of you — which is the thing a suite of unit and container tests cannot say.
#
#   docker compose -f deploy/dev-stack/compose.yml up -d --wait
#   ./hack/walk-the-flow.sh
#
# Every step asserts and the script exits non-zero on the first failure. A
# walkthrough that printed progress and always exited 0 would read as "the
# flow works" while proving nothing, which is the failure this is meant to
# catch in the product rather than commit itself.
#
# Everything happens inside a scratch tenant, so a run cannot touch the
# accounts of the deployment it is pointed at. The tenant is reused across
# runs; there is no tenant delete, and inventing one for a walkthrough would
# be the wrong reason to have it.
set -euo pipefail

cd "$(dirname "$0")/.."

PORTICO_URL="${PORTICO_URL:-http://127.0.0.1:8412}"
TENANT="${WALK_TENANT:-walkthrough}"
LDAP_CONTAINER="${WALK_LDAP_CONTAINER:-portico-dev-ldap}"
LDAP_ADMIN_DN="cn=admin,dc=example,dc=org"
LDAP_ADMIN_PW="portico-dev"

# curl must not go through a proxy: these are loopback addresses, and a
# desktop VPN client that exports http_proxy will otherwise swallow them.
CURL=(curl --silent --show-error --noproxy '*')

step=0
say() { step=$((step + 1)); printf '\n\033[1m%d. %s\033[0m\n' "$step" "$1"; }
ok() { printf '   ✓ %s\n' "$1"; }
die() { printf '   ✗ %s\n' "$1" >&2; exit 1; }

# equals compares and explains. The message names what the number means
# rather than repeating the assertion, because the person reading it is
# looking at a failure and not at this file.
equals() {
  local actual="$1" expected="$2" what="$3"
  if [[ "$actual" != "$expected" ]]; then
    die "$what: got $actual, want $expected"
  fi
  ok "$what: $actual"
}

api() {
  local method="$1" path="$2" token="${3:-}" body="${4:-}"
  local args=(-X "$method" "$PORTICO_URL$path" -H 'Content-Type: application/json')
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$body" ]] && args+=(-d "$body")
  "${CURL[@]}" "${args[@]}"
}

# data unwraps the response envelope, failing loudly on an error code rather
# than returning null for a caller to trip over three lines later.
data() {
  local response="$1" what="$2"
  local code
  code=$(jq -r '.code // "SUCCESS"' <<<"$response")
  if [[ "$code" != "SUCCESS" ]]; then
    die "$what failed: $code — $(jq -r '.message // ""' <<<"$response")"
  fi
  jq '.data' <<<"$response"
}

ldap() { docker exec -i "$LDAP_CONTAINER" "$@"; }

ldap_modify() {
  ldap ldapmodify -x -D "$LDAP_ADMIN_DN" -w "$LDAP_ADMIN_PW" -H ldap://localhost >/dev/null
}

# ---------------------------------------------------------------- preflight

say "Check what this needs before changing anything"

for tool in jq curl docker openssl; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done
[[ -x ./portico ]] || die "./portico is not built — go build -o portico ./cmd/server"
[[ -n "${PORTICO_DB_DSN:-}" ]] || die "PORTICO_DB_DSN is unset; the CLI needs it to create a tenant"

"${CURL[@]}" --fail --max-time 5 "$PORTICO_URL/api/v1/ready" >/dev/null \
  || die "$PORTICO_URL is not answering /api/v1/ready"
ok "Portico is up at $PORTICO_URL"

ldap ldapsearch -x -D "$LDAP_ADMIN_DN" -w "$LDAP_ADMIN_PW" -H ldap://localhost \
  -b "ou=people,dc=example,dc=org" -s base >/dev/null 2>&1 \
  || die "the directory in $LDAP_CONTAINER is not answering — compose up --wait"
ok "the directory is up in $LDAP_CONTAINER"

# ------------------------------------------------------------------- tenant

say "Get an administrator in a scratch tenant"

ADMIN_PASSWORD="walkthrough-Pass-1"
# Captured first rather than piped, and this is not style. Under `set -o
# pipefail`, `grep -q` exits the moment it matches, the CLI upstream gets
# SIGPIPE and a non-zero status, and the pipeline is judged to have failed —
# so "the tenant exists" reported false precisely when it was true, and every
# run tried to create it again. Two bugs in one line: the anchor also used
# `\b`, a GNU extension that matches nothing on macOS's grep.
TENANT_LIST=$(./portico tenant list 2>/dev/null || true)
if grep -qE "^${TENANT}[[:space:]]" <<<"$TENANT_LIST"; then
  ok "tenant $TENANT already exists, reusing it"
else
  ./portico tenant create --code "$TENANT" --name "Walkthrough" \
    --admin-password "$ADMIN_PASSWORD" >/dev/null
  ok "created tenant $TENANT"
fi

TOKEN=$(data "$(api POST /api/v1/auth/login "" \
  "$(jq -nc --arg t "$TENANT" --arg p "$ADMIN_PASSWORD" \
    '{tenant:$t, identifier:"admin", password:$p}')")" "sign-in" | jq -r '.token')
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || die "no token came back from sign-in"
ok "signed in as the tenant administrator"

users_named() {
  data "$(api GET "/api/v1/users?pageSize=200" "$TOKEN")" "list users" \
    | jq -r --arg u "$1" '.items[] | select(.username == $u)'
}
user_count() {
  data "$(api GET "/api/v1/users?pageSize=200" "$TOKEN")" "list users" | jq '.items | length'
}

# ------------------------------------------------------------ the directory

say "Put the directory back to the state the walkthrough starts from"

# Every step below changes the directory and asserts on what changed. Run
# twice without this, and the second run finds the rename already applied,
# sees nothing change, and reports a working connector as broken. A
# walkthrough that only passes on a fresh fixture is one nobody runs twice.
ldap ldapmodrdn -x -D "$LDAP_ADMIN_DN" -w "$LDAP_ADMIN_PW" -H ldap://localhost \
  -s "ou=support,ou=people,dc=example,dc=org" \
  "uid=grace,ou=archive,dc=example,dc=org" "uid=grace" >/dev/null 2>&1 || true

ldap_modify <<'LDIF'
dn: uid=mei,ou=engineering,ou=people,dc=example,dc=org
changetype: modify
replace: displayName
displayName: Mei Chen
-
replace: mail
mail: mei@example.org
LDIF
ok "directory reset"

say "Point it at the directory and synchronize"

EXISTING=$(data "$(api GET /api/v1/directories "$TOKEN")" "list directories" \
  | jq -r '.[]? | select(.name == "walkthrough") | .id')

if [[ -z "$EXISTING" ]]; then
  SOURCE=$(data "$(api POST /api/v1/directories "$TOKEN" '{
    "name": "walkthrough",
    "host": "127.0.0.1",
    "port": 3890,
    "encryption": "none",
    "bindDn": "cn=admin,dc=example,dc=org",
    "bindPassword": "portico-dev",
    "baseDn": "ou=people,dc=example,dc=org",
    "userFilter": "(objectClass=inetOrgPerson)",
    "attrUsername": "uid",
    "attrDisplayName": "displayName",
    "attrEmail": "mail",
    "attrPhone": "telephoneNumber",
    "attrExternalId": "entryUUID"
  }')" "create directory source" | jq -r '.id')
  ok "registered the directory source"
else
  SOURCE="$EXISTING"
  ok "reusing the directory source"
fi

# A run that failed still answers 200 — the request to record it worked —
# so the outcome has to be read out of the run itself. Without this a broken
# source returns a record full of zeros and every assertion below passes on
# nothing, which is how a left-over filter from an earlier session made the
# whole walkthrough look like a working connector doing no work.
sync_now() {
  local run
  run=$(data "$(api POST "/api/v1/directories/$SOURCE/sync" "$TOKEN")" "synchronize")
  local outcome
  outcome=$(jq -r '.outcome' <<<"$run")
  if [[ "$outcome" != "SUCCEEDED" ]]; then
    die "the run did not succeed: $outcome — $(jq -r '.errorCode // ""' <<<"$run")"
  fi
  printf '%s' "$run"
}

# The same, for the one step that wants the failure.
sync_expecting_failure() {
  data "$(api POST "/api/v1/directories/$SOURCE/sync" "$TOKEN")" "synchronize"
}

RUN=$(sync_now)
equals "$(jq -r '.skippedCount' <<<"$RUN")" "1" \
  "entries skipped, which must be the uid=admin collision and not somebody else"
# This run absorbs whatever the reset above changed, so the steps that follow
# assert against a settled starting point rather than against leftovers.
ok "created $(jq -r '.createdCount' <<<"$RUN"), updated $(jq -r '.updatedCount' <<<"$RUN"), deactivated $(jq -r '.deactivatedCount' <<<"$RUN")"

[[ -n "$(users_named mei)" ]] || die "mei did not arrive"
ADMIN_SOURCE=$(users_named admin | jq -r '.source')
# ADMIN is what the bootstrap administrator carries. Whatever it is, it must
# not have become LDAP: the directory listing somebody with the same username
# is not a claim on the account, and treating it as one locks an operator out
# of their own console.
equals "$ADMIN_SOURCE" "ADMIN" \
  "the administrator's source, which the directory must not have taken over"

AFTER_FIRST=$(user_count)

# ------------------------------------------------------------------ renames

say "Rename somebody in the directory: it must stay one account"

ldap_modify <<'LDIF'
dn: uid=mei,ou=engineering,ou=people,dc=example,dc=org
changetype: modify
replace: displayName
displayName: Mei Chen-Alvarez
-
replace: mail
mail: mei.chen-alvarez@example.org
LDIF

RUN=$(sync_now)
equals "$(jq -r '.updatedCount' <<<"$RUN")" "1" "accounts updated"
equals "$(user_count)" "$AFTER_FIRST" \
  "total accounts, which must not grow — a rename is a rename, not a second account"
equals "$(users_named mei | jq -r '.displayName')" "Mei Chen-Alvarez" "the new display name"

# --------------------------------------------------------- leaving, and not

say "Move somebody out of the directory's reach, then back"

# Moved rather than deleted. Deleting and re-adding gives slapd a reason to
# generate a new entryUUID, and what comes back is then a different entry
# sharing a username — correctly skipped rather than reactivated. Moving the
# entry keeps its identity and changes only whether the search can see it,
# which is what a leaver and a returner actually look like.
ldap ldapmodrdn -x -D "$LDAP_ADMIN_DN" -w "$LDAP_ADMIN_PW" -H ldap://localhost \
  -s "ou=archive,dc=example,dc=org" \
  "uid=grace,ou=support,ou=people,dc=example,dc=org" "uid=grace" >/dev/null

RUN=$(sync_now)
equals "$(jq -r '.deactivatedCount' <<<"$RUN")" "1" "accounts deactivated"
equals "$(users_named grace | jq -r '.status')" "DISABLED" "grace's status"

ldap ldapmodrdn -x -D "$LDAP_ADMIN_DN" -w "$LDAP_ADMIN_PW" -H ldap://localhost \
  -s "ou=support,ou=people,dc=example,dc=org" \
  "uid=grace,ou=archive,dc=example,dc=org" "uid=grace" >/dev/null

RUN=$(sync_now)
equals "$(users_named grace | jq -r '.status')" "ACTIVE" \
  "grace's status after the directory listed her again"
equals "$(jq -r '.skippedCount' <<<"$RUN")" "1" \
  "entries skipped, still only the collision — a returner must not arrive as a second account"

# A directory that comes back empty is a typo far more often than an
# organisation everybody has left, so the run must refuse rather than
# deactivate everyone. This is the assertion the whole connector exists for.
say "Point the filter at nothing: the run must refuse rather than empty the tenant"

# The whole object goes back, with one field changed. A partial body is
# rejected — the fields are not optional on update — and the first version of
# this step sent one, had it refused, and then asserted against a run that
# used the original filter and therefore succeeded. It reported the guard as
# broken when the guard had not been reached.
source_json() {
  data "$(api GET "/api/v1/directories/$SOURCE" "$TOKEN")" "read directory source"
}
set_filter() {
  local filter="$1"
  data "$(api PUT "/api/v1/directories/$SOURCE" "$TOKEN" \
    "$(source_json | jq -c --arg f "$filter" \
      '{name,host,port,encryption,bindDn,baseDn,attrUsername,attrDisplayName,
        attrEmail,attrPhone,attrExternalId,organizationId} + {userFilter:$f}')")" \
    "update the filter" >/dev/null
}

BEFORE=$(user_count)
set_filter "(objectClass=nothingMatchesThis)"
BROKEN=$(sync_expecting_failure)
set_filter "(objectClass=inetOrgPerson)"

# The refusal is in the run, not in the response. A run that failed is still
# a run that was recorded, so the request succeeds and the record says
# FAILED with the reason — which is also what the history has to show an
# operator afterwards. Asserting on the envelope instead reported the guard
# as missing while it was working.
equals "$(jq -r '.outcome' <<<"$BROKEN")" "FAILED" "the run's outcome"
equals "$(jq -r '.errorCode' <<<"$BROKEN")" "DIRECTORY_RETURNED_NOTHING" "the refusal"
equals "$(user_count)" "$BEFORE" "accounts after the refused run, which must be unchanged"
equals "$(users_named mei | jq -r '.status')" "ACTIVE" \
  "mei's status, because the point of the refusal is that nobody was deactivated"

printf '\n\033[1mWalked %d steps, all asserted.\033[0m\n' "$step"
