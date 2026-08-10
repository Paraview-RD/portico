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

# And the run says why, not only how many. A count on its own sent this
# walkthrough looking in the wrong place for several rounds — the reason was
# known to the code and recorded nowhere.
SKIP_DETAIL=$(jq -r '.skippedDetail // ""' <<<"$RUN")
[[ -n "$SKIP_DETAIL" ]] || die "the run records how many entries were skipped and not why"
grep -q "admin" <<<"$SKIP_DETAIL" \
  || die "the reason does not name the entry to go and look at: $SKIP_DETAIL"
ok "and says why: $SKIP_DETAIL"

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

# ------------------------------------------------------------------- SCIM

say "Push an account in over SCIM, rename it there, and get one account"

# The token is shown once, so a run cannot reuse the one an earlier run was
# given. The credential is revoked and re-issued instead — which is what an
# operator who has lost theirs does, and is the only honest way to make this
# step repeatable.
OLD_CREDENTIAL=$(data "$(api GET /api/v1/scim-credentials "$TOKEN")" "list credentials" \
  | jq -r '(.items? // .)[] | select(.name == "walkthrough") | .id' | head -1)
if [[ -n "$OLD_CREDENTIAL" ]]; then
  api DELETE "/api/v1/scim-credentials/$OLD_CREDENTIAL" "$TOKEN" >/dev/null
  ok "revoked the credential from an earlier run"
fi

SCIM_TOKEN=$(data "$(api POST /api/v1/scim-credentials "$TOKEN" \
  '{"name":"walkthrough"}')" "issue a SCIM credential" | jq -r '.token // .secret // .credential')
[[ -n "$SCIM_TOKEN" && "$SCIM_TOKEN" != "null" ]] || die "no SCIM token in the response"
ok "issued a provisioning credential"

scim() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-X "$method" "$PORTICO_URL/scim/v2$path"
    -H "Authorization: Bearer $SCIM_TOKEN"
    -H 'Content-Type: application/scim+json')
  [[ -n "$body" ]] && args+=(-d "$body")
  "${CURL[@]}" "${args[@]}"
}

SCIM_EXTERNAL="walkthrough-scim-1"
CREATED=$(scim POST /Users "$(jq -nc --arg e "$SCIM_EXTERNAL" '{
  schemas: ["urn:ietf:params:scim:schemas:core:2.0:User"],
  userName: "nadia", externalId: $e, active: true,
  name: {givenName: "Nadia", familyName: "Haddad"},
  emails: [{value: "nadia@example.org", primary: true}]
}')")
SCIM_ID=$(jq -r '.id // empty' <<<"$CREATED")
if [[ -z "$SCIM_ID" ]]; then
  # Already provisioned by an earlier run: find it by its externalId, which
  # is the same lookup the directory would do.
  SCIM_ID=$(scim GET "/Users?filter=externalId%20eq%20%22$SCIM_EXTERNAL%22" \
    | jq -r '.Resources[0].id // empty')
  [[ -n "$SCIM_ID" ]] || die "SCIM create failed: $(jq -c '.' <<<"$CREATED")"
  ok "reusing the account provisioned earlier"
else
  ok "provisioned nadia"
fi

# The rename a directory sends when somebody changes their name: same
# externalId, different userName. It must land on the same account.
RENAMED=$(scim POST /Users "$(jq -nc --arg e "$SCIM_EXTERNAL" '{
  schemas: ["urn:ietf:params:scim:schemas:core:2.0:User"],
  userName: "nadia.haddad", externalId: $e, active: true,
  name: {givenName: "Nadia", familyName: "Haddad"}
}')")
equals "$(jq -r '.id' <<<"$RENAMED")" "$SCIM_ID" \
  "the account id after a rename, which must be the one that already existed"
equals "$(jq -r '.userName' <<<"$RENAMED")" "nadia.haddad" "the new username"

# --------------------------------------------------- registration and email

say "Register from the sign-in screen and confirm the address by email"

api PUT /api/v1/settings "$TOKEN" \
  '{"registrationEnabled":true,"registrationVerification":true}' >/dev/null

# A new person each run. Accounts are never deleted here — that is the
# product's rule and a good one — so reusing a name would mean the second run
# finds somebody already verified and cannot assert that an unverified
# account is refused, which is the only assertion in this step that matters.
# The scratch tenant collects one of these per run; that is the price.
NEWCOMER="walk-newcomer-$(date +%s)"
NEWCOMER_MAIL="$NEWCOMER@example.org"
NEWCOMER_PASSWORD="walk-Newcomer-1"

REGISTERED=$(api POST /api/v1/auth/register "" "$(jq -nc \
  --arg t "$TENANT" --arg u "$NEWCOMER" --arg m "$NEWCOMER_MAIL" --arg p "$NEWCOMER_PASSWORD" \
  '{tenant:$t, username:$u, displayName:"Walk Newcomer", email:$m, password:$p}')")
equals "$(jq -r '.code' <<<"$REGISTERED")" "SUCCESS" "registration"

# Unverified means unusable. This is the whole reason the setting exists:
# without it the address on a new account is whatever was typed, and that
# address is where a reset link goes.
BLOCKED=$(api POST /api/v1/auth/login "" "$(jq -nc \
  --arg t "$TENANT" --arg u "$NEWCOMER" --arg p "$NEWCOMER_PASSWORD" \
  '{tenant:$t, identifier:$u, password:$p}')")
equals "$(jq -r '.code' <<<"$BLOCKED")" "ACCOUNT_UNVERIFIED" \
  "sign-in before confirming, which must be refused and say why"

# The link, out of the inbox rather than out of the database — the point is
# that a message was actually sent and is readable by the person who got it.
MAILPIT="${WALK_MAILPIT:-http://127.0.0.1:8426}"
LINK=""
for _ in $(seq 1 20); do
  MESSAGE_ID=$("${CURL[@]}" "$MAILPIT/api/v1/search?query=to:$NEWCOMER_MAIL&limit=1" \
    | jq -r '.messages[0].ID // empty')
  if [[ -n "$MESSAGE_ID" ]]; then
    # tr -d '\r' is load-bearing. A mail body is CRLF, so the link comes out
    # with a carriage return on the end and the token becomes a different
    # string — the confirmation is then refused as invalid, which reads like
    # a broken link rather than like whitespace.
    LINK=$("${CURL[@]}" "$MAILPIT/api/v1/message/$MESSAGE_ID" \
      | jq -r '.Text' | tr -d '\r' | tr ' ' '\n' \
      | grep -o 'http[^ ]*verify?[^ ]*' | head -1)
    [[ -n "$LINK" ]] && break
  fi
  sleep 0.5
done
[[ -n "$LINK" ]] || die "no confirmation message arrived at $MAILPIT for $NEWCOMER_MAIL"
ok "the confirmation message arrived"

VERIFY_TOKEN=$(sed -n 's/.*token=\([^&]*\).*/\1/p' <<<"$LINK")
[[ -n "$VERIFY_TOKEN" ]] || die "the link carries no token: $LINK"

CONFIRMED=$(api POST /api/v1/auth/register/verify "" "$(jq -nc \
  --arg t "$TENANT" --arg k "$VERIFY_TOKEN" '{tenant:$t, token:$k}')")
# Strictly SUCCESS. An earlier version accepted INVALID_VERIFICATION_TOKEN
# too, on the theory that a repeat run would find a spent link — and that
# tolerance hid a real failure for exactly one run: the token was carrying a
# stray carriage return and every confirmation was being refused.
equals "$(jq -r '.code' <<<"$CONFIRMED")" "SUCCESS" "confirming the address"

NOW_IN=$(api POST /api/v1/auth/login "" "$(jq -nc \
  --arg t "$TENANT" --arg u "$NEWCOMER" --arg p "$NEWCOMER_PASSWORD" \
  '{tenant:$t, identifier:$u, password:$p}')")
equals "$(jq -r '.code' <<<"$NOW_IN")" "SUCCESS" "sign-in after confirming"

api PUT /api/v1/settings "$TOKEN" \
  '{"registrationEnabled":false,"registrationVerification":false}' >/dev/null

# ------------------------------------------------------- the export it ends with

say "Take the directory out as a spreadsheet"

# An explicit template rather than `mktemp -t portico-export`, because -t
# means two different things. BSD mktemp treats the argument as a prefix and
# appends randomness; GNU mktemp treats it as a template and refuses one with
# fewer than three X's — so the form that works on the laptop this was written
# on dies on the runner with "too few X's in template". Passing the full path
# with the X's spelled out is the one spelling both agree on.
EXPORT=$(mktemp "${TMPDIR:-/tmp}/portico-export.XXXXXX").xlsx
"${CURL[@]}" -H "Authorization: Bearer $TOKEN" \
  "$PORTICO_URL/api/v1/users/export" -o "$EXPORT"

# A real workbook, not an error body with a spreadsheet's name on it.
head -c 2 "$EXPORT" | grep -q 'PK' || die "the export is not a workbook: $(head -c 120 "$EXPORT")"
ok "the export is a workbook"

# The column is there and every cell in it is empty, and both halves are
# deliberate. The importer reads by position so that a translated header
# still works, which means dropping the column from the export would shift
# every field on the way back in — silently, and into the wrong columns. So
# the header stays and the values do not: what must never appear is a
# password, not a heading.
unzip -p "$EXPORT" xl/sharedStrings.xml 2>/dev/null | grep -qi '>password<' \
  || die "the export has no password column, so feeding it back through import \
would put every field one column to the left"

for secret in "$ADMIN_PASSWORD" "$NEWCOMER_PASSWORD"; do
  if unzip -p "$EXPORT" xl/sharedStrings.xml 2>/dev/null | grep -qF "$secret"; then
    die "a password is in the export — a report carrying credentials is a \
credential-distribution mechanism nobody meant to build"
  fi
done
ok "the password column is present and empty; no password is in the file"

unzip -p "$EXPORT" xl/sharedStrings.xml 2>/dev/null | grep -qi 'nadia' \
  || die "the export does not contain the accounts it should"
ok "the accounts are in it"
rm -f "$EXPORT"

EXPORTS=$(data "$(api GET "/api/v1/audit-logs?action=USER_EXPORT&pageSize=5" "$TOKEN")" \
  "read the audit log" | jq '.items | length')
[[ "$EXPORTS" -ge 1 ]] || die "the export is not in the audit log; \
\"who took a copy, and when\" is asked after an incident, not before one"
ok "recorded in the audit log as USER_EXPORT"

# ----------------------------------------------------- through a real client

say "Sign in to a relying party over OpenID Connect"

# examples/mock-sp is a real client: it runs discovery, builds the
# authorization request with PKCE, verifies the ID token against the key set
# the discovery document named, and calls userinfo. Driving it proves the
# whole exchange rather than the endpoints one at a time.
#
# Skipped rather than assumed when it is absent — it is not part of every
# checkout — and said out loud, because a step that quietly passes when it did
# not run is the failure this script exists to avoid.
if [[ ! -d examples/mock-sp ]]; then
  printf '   – skipped: examples/mock-sp is not in this checkout\n'
else
  SP_PORT="${WALK_SP_PORT:-8414}"
  SP_BASE="http://localhost:$SP_PORT"
  SP_CLIENT="walk-mock-sp"

  # Registered exactly as the client will send it. Portico matches redirect
  # URIs as strings, so localhost and 127.0.0.1 are two different URIs.
  ./portico client register --tenant "$TENANT" --id "$SP_CLIENT" --name "Walk mock-sp" \
    --public --redirect-uri "$SP_BASE/oidc/callback" \
    --scope openid --scope profile --scope email >/dev/null 2>&1 || true
  ok "registered $SP_CLIENT in $TENANT"

  # A stable state directory, outside the repository. mock-sp generates its
  # SAML key there, and its metadata — entity id, certificate — is what gets
  # registered with Portico. A fresh directory on every run would mean a new
  # certificate against a registration holding the old one, which fails on the
  # second run only. Delete it to start clean.
  SP_STATE="${WALK_SP_STATE:-$HOME/.cache/portico-walkthrough-sp}"
  mkdir -p "$SP_STATE"

  # Built and run, not `go run`. `go run` compiles to a cache and executes a
  # child, so the process this script holds is the compiler's wrapper: killing
  # it leaves the server listening. The next run then either collides on the
  # port or, worse, talks to a stale instance still configured for whatever
  # the previous run set up — and passes.
  go build -o "$SP_STATE/mock-sp" ./examples/mock-sp \
    || die "examples/mock-sp does not build"
  "$SP_STATE/mock-sp" \
    -addr "127.0.0.1:$SP_PORT" \
    -issuer "$PORTICO_URL/t/$TENANT" \
    -client-id "$SP_CLIENT" \
    -state-dir "$SP_STATE" >"$SP_STATE/log" 2>&1 &
  SP_PID=$!
  stop_sp() {
    kill "$SP_PID" 2>/dev/null || true
    # Waited on so bash does not print its own "Terminated" line over the
    # walkthrough's output.
    wait "$SP_PID" 2>/dev/null || true
  }
  trap 'stop_sp' EXIT

  for _ in $(seq 1 40); do
    "${CURL[@]}" --max-time 2 -o /dev/null "$SP_BASE/" 2>/dev/null && break
    sleep 0.5
  done
  "${CURL[@]}" --max-time 2 -o /dev/null "$SP_BASE/" \
    || die "mock-sp did not come up on $SP_PORT: $(tail -3 "$SP_STATE/log")"
  ok "mock-sp is up on $SP_PORT and discovered $PORTICO_URL/t/$TENANT"

  JAR=$(mktemp)
  # Its cookies matter: state and the PKCE verifier live in them, and the
  # library checks both on the way back. A flow driven without them looks
  # like a stolen code.
  location_of() { awk 'tolower($1) == "location:" {print $2}' | tr -d '\r' | tail -1; }

  # What a page says, rather than the first 300 bytes of it. Every one of
  # these pages opens with the same stylesheet, so quoting the head of the
  # HTML in a failure message shows CSS and no reason — which is the defect
  # this walkthrough keeps finding elsewhere.
  page_text() {
    sed -n '/<\/style>/,$p' | sed 's/<[^>]*>//g' | grep -vE '^[[:space:]]*$' | head -6
  }

  AUTH_URL=$("${CURL[@]}" -c "$JAR" -b "$JAR" -o /dev/null -D - "$SP_BASE/oidc" | location_of)
  grep -q "$PORTICO_URL" <<<"$AUTH_URL" \
    || die "mock-sp did not send us to Portico: $AUTH_URL"
  grep -q "code_challenge" <<<"$AUTH_URL" \
    || die "the authorization request carries no PKCE challenge: $AUTH_URL"
  ok "it sent a PKCE authorization request to Portico"

  AUTH_RESPONSE=$("${CURL[@]}" -D - -o /dev/null -w '%{http_code}' "$AUTH_URL")
  LOGIN_URL=$(location_of <<<"$AUTH_RESPONSE")
  AUTH_REQUEST=$(sed -n 's/.*auth_request=\([^&]*\).*/\1/p' <<<"$LOGIN_URL")
  # With what Portico actually answered. An empty location and no explanation
  # is the same failure this project just finished fixing in the directory
  # connector: a symptom with the cause withheld. The usual cause here is a
  # redirect URI that does not match the registered one to the character.
  [[ -n "$AUTH_REQUEST" ]] || die "Portico did not send us to the sign-in screen.
   It answered: $(tr -d '\r' <<<"$AUTH_RESPONSE" | grep -Ei '^(HTTP/|location:)' | tr '\n' ' ')
   The usual cause is a redirect URI registered differently from the one the
   client sends — they are matched as strings, so localhost and 127.0.0.1 are
   two different URIs."

  # The sign-in screen's own call, made here because there is no browser.
  RETURN_TO=$(data "$(api POST /api/v1/oauth/authorize "$TOKEN" \
    "$(jq -nc --arg r "$AUTH_REQUEST" '{authRequestId:$r}')")" "authorize" | jq -r '.redirectTo')
  CALLBACK=$("${CURL[@]}" -o /dev/null -D - "$RETURN_TO" | location_of)
  grep -q "$SP_BASE/oidc/callback" <<<"$CALLBACK" \
    || die "Portico did not send the browser back to the client: $CALLBACK"
  ok "Portico issued a code and sent it back to the client"

  # The client exchanges the code, verifies the ID token, and renders what it
  # was told. Everything asserted below came out of a token this client
  # verified — not out of an API call this script made.
  PAGE=$("${CURL[@]}" -c "$JAR" -b "$JAR" "$CALLBACK")
  grep -q "admin" <<<"$PAGE" || die "the client did not learn who signed in:
$(page_text <<<"$PAGE")"
  ok "the client verified the ID token and knows who signed in"
  grep -q "$TENANT" <<<"$PAGE" \
    || die "the tenant claim did not reach the client, so a downstream system \
serving more than one tenant could not tell them apart"
  ok "the tenant claim reached it"
  grep -qi "SUPER_ADMIN" <<<"$PAGE" || die "the role claim did not reach the client"
  ok "the role claim reached it"

  # ------------------------------------------------------------ SAML 2.0

  say "Sign in to the same client over SAML 2.0"

  # The service provider is registered from its own metadata, fetched from
  # the running client rather than written out here — the document is what
  # the two sides actually exchange, and a hand-made copy is a third version
  # of the truth.
  "${CURL[@]}" "$SP_BASE/saml/metadata" -o "$SP_STATE/sp-metadata.xml"
  grep -q "EntityDescriptor" "$SP_STATE/sp-metadata.xml" \
    || die "the client did not serve SAML metadata"
  # Pushed every run, updating the registration where one exists rather than
  # skipping it. The metadata carries the client's encryption certificate,
  # and Portico encrypts the assertion with whatever it was registered with —
  # so a registration left over from a client that has since regenerated its
  # key produces "certificate does not match provided key" at the far end,
  # which reads as a broken assertion rather than as a stale registration.
  SP_ENTITY="$SP_BASE/saml/metadata"
  SP_BODY=$(jq -n --rawfile xml "$SP_STATE/sp-metadata.xml" \
    '{metadataXml: $xml, name: "Walk mock-sp"}')
  SP_EXISTING=$(data "$(api GET /api/v1/applications/saml-service-providers "$TOKEN")" \
    "list service providers" \
    | jq -r --arg e "$SP_ENTITY" '(.items? // .)[] | select(.entityId == $e) | .id' | head -1)
  if [[ -n "$SP_EXISTING" ]]; then
    data "$(api PUT "/api/v1/applications/saml-service-providers/$SP_EXISTING" \
      "$TOKEN" "$SP_BODY")" "update the service provider" >/dev/null
    ok "updated the service provider with the metadata it is serving now"
  else
    data "$(api POST /api/v1/applications/saml-service-providers "$TOKEN" "$SP_BODY")" \
      "register the service provider" >/dev/null
    ok "registered the service provider from its own metadata"
  fi

  SAML_JAR=$(mktemp)
  SSO_URL=$("${CURL[@]}" -c "$SAML_JAR" -b "$SAML_JAR" -o /dev/null -D - "$SP_BASE/saml" | location_of)
  grep -q "SAMLRequest=" <<<"$SSO_URL" || die "the client sent no SAMLRequest: $SSO_URL"
  ok "it sent an authentication request over the redirect binding"

  SAML_LOGIN=$("${CURL[@]}" -o /dev/null -D - "$SSO_URL" | location_of)
  SAML_REQUEST=$(sed -n 's/.*saml_request=\([^&]*\).*/\1/p' <<<"$SAML_LOGIN")
  [[ -n "$SAML_REQUEST" ]] || die "Portico did not park the request: $SAML_LOGIN"

  SAML_RETURN=$(data "$(api POST /api/v1/saml/authenticate "$TOKEN" \
    "$(jq -nc --arg r "$SAML_REQUEST" '{samlRequestId:$r}')")" "authenticate" \
    | jq -r '.redirectTo')

  # The assertion comes back as a form the browser posts. There is no browser,
  # so the form is read and posted here — which is also the only way to see
  # that it is a POST binding at all.
  "${CURL[@]}" "$SAML_RETURN" -o "$SP_STATE/post.html"
  ACS=$(grep -o 'action="[^"]*"' "$SP_STATE/post.html" | head -1 | sed 's/action="//;s/"//')
  # Entities decoded, because a browser does that before submitting and curl
  # does not. The form escapes + as &#43;, which is not a base64 character —
  # the client refuses the assertion with "illegal base64 data at input byte
  # 491", which reads like a broken assertion rather than like an unescaped
  # form field.
  SAML_RESPONSE=$(grep -o 'name="SAMLResponse"[^>]*value="[^"]*"' "$SP_STATE/post.html" \
    | sed 's/.*value="//; s/"$//; s/&#43;/+/g; s/&#47;/\//g; s/&#61;/=/g; s/&amp;/\&/g')

  # And a guard against that fix being too narrow: base64 is exactly this
  # alphabet, so anything left over is an entity nobody decoded, and it must
  # fail here rather than three lines later as a parse error.
  if printf '%s' "$SAML_RESPONSE" | tr -d 'A-Za-z0-9+/=' | grep -q .; then
    die "the assertion still contains something that is not base64 after \
decoding entities: $(printf '%s' "$SAML_RESPONSE" | tr -d 'A-Za-z0-9+/=' | head -c 40)"
  fi
  [[ -n "$ACS" && -n "$SAML_RESPONSE" ]] \
    || die "no SAMLResponse form came back: $(head -c 200 "$SP_STATE/post.html")"
  ok "Portico returned a signed assertion for $ACS"

  SAML_PAGE=$("${CURL[@]}" -c "$SAML_JAR" -b "$SAML_JAR" -X POST "$ACS" \
    --data-urlencode "SAMLResponse=$SAML_RESPONSE")
  grep -q "admin" <<<"$SAML_PAGE" \
    || die "the client did not accept the assertion:
$(page_text <<<"$SAML_PAGE")"
  ok "the client validated the assertion and knows who signed in"
  grep -q "$TENANT" <<<"$SAML_PAGE" || die "the tenant attribute did not arrive"
  ok "the tenant attribute arrived"
  rm -f "$SAML_JAR"

  # ----------------------------------------------------------------- CAS

  say "Sign in to the same client over CAS"

  # A prefix, not a pattern. Registering the base means every path under it
  # may receive a ticket, which is what a CAS client expects.
  ./portico cas register --tenant "$TENANT" --url "$SP_BASE/" \
    --name "Walk mock-sp" >/dev/null 2>&1 || true
  ok "registered the CAS service prefix"

  CAS_JAR=$(mktemp)
  CAS_LOGIN=$("${CURL[@]}" -c "$CAS_JAR" -b "$CAS_JAR" -o /dev/null -D - "$SP_BASE/cas" | location_of)
  CAS_SERVICE=$(sed -n 's/.*[?&]service=\([^&]*\).*/\1/p' <<<"$CAS_LOGIN")
  [[ -n "$CAS_SERVICE" ]] || die "the client named no service: $CAS_LOGIN"

  # Portico parks nothing for CAS — the service URL is the whole request —
  # so it is checked against the registrations rather than trusted, and a
  # sign-in screen carrying it in a query parameter cannot become a ticket
  # for somewhere else.
  CAS_SERVICE=$(printf '%b' "${CAS_SERVICE//%/\\x}")
  CAS_RETURN=$(data "$(api POST /api/v1/cas/authorize "$TOKEN" \
    "$(jq -nc --arg s "$CAS_SERVICE" '{service:$s}')")" "authorize a CAS ticket" \
    | jq -r '.redirectTo')
  grep -q "ticket=ST-" <<<"$CAS_RETURN" || die "no service ticket: $CAS_RETURN"
  ok "Portico issued a service ticket"

  CAS_PAGE=$("${CURL[@]}" -c "$CAS_JAR" -b "$CAS_JAR" "$CAS_RETURN")
  grep -q "admin" <<<"$CAS_PAGE" \
    || die "the client did not validate the ticket:
$(page_text <<<"$CAS_PAGE")"
  ok "the client validated the ticket and knows who signed in"
  rm -f "$CAS_JAR"

  rm -f "$JAR"
  stop_sp
  trap - EXIT

  # The port has to be free afterwards, or the next run inherits this one.
  if lsof -nP -iTCP:"$SP_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    die "something is still listening on $SP_PORT after the client was stopped"
  fi
  ok "the client stopped and released $SP_PORT"
fi

printf '\n\033[1mWalked %d steps, all asserted.\033[0m\n' "$step"
