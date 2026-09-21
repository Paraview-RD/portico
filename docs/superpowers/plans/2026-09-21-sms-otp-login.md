# SMS 验证码登录 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a standalone phone-number + SMS-code login method alongside the existing username/password login, backed by a real Aliyun Dysmsapi SMS transport.

**Architecture:** A new `sms_login_codes` table (tenant-scoped, like every other table) holds the OTP itself; a new `SMSLoginService` issues and verifies codes and hands off to `UserService`'s existing post-credential-check tail (shared with `IssueSessionForExternalIdentity`) to mint a session. Four abuse-prevention counters (phone cooldown, phone/day, IP/day, deployment/day) live in a new in-memory day-window limiter next to the existing `httpx.RateLimiter`, deliberately outside the tenant-scoped data model. `notify.SMSSender` changes from "send this free text" to "send this named template with these variables," because Aliyun requires pre-approved templates — this also fixes password-recovery-over-SMS and registration-verification-over-SMS, which were dead code until now.

**Tech Stack:** Go (backend, stdlib `crypto/hmac`+`sha1` for Aliyun's request signing, no SDK), sqlc + goose migrations + PostgreSQL, React/TypeScript (frontend), chi router.

**Spec:** `docs/superpowers/specs/2026-09-21-sms-otp-login-design.md` — read it first; this plan implements it and does not repeat its reasoning, only its decisions.

## Global Constraints

- OTP: 6 digits, 5-minute TTL, single use, max 5 wrong verify attempts per code (spec §3.2)
- Rate limits, **all in-memory, none in the database** (spec §3.2, and its "为什么不进数据库" note): phone cooldown 60s, phone/day ≤5, IP/day ≤20, deployment/day ≤1000 (default, env-configurable). Phone-keyed counters aggregate **across tenants** — never filter by `tenant_id`.
- `sms_login_codes` itself stays fully tenant-scoped: every query on it filters by `tenant_id` like any other table. No edits to `internal/store/tenancy_guard_test.go`.
- Anti-enumeration (spec §2.1): `/auth/sms/code` always answers success; `/auth/sms/login` always answers the same `INVALID_CODE` for "wrong code", "expired", "too many attempts", and "no such phone" alike. Only after the code itself checks out do locked/disabled/closed get their own messages.
- Login tail: locked/disabled/closed block; `PasswordPolicy().Expired(...)` and `MustChangePassword` do not (spec §2.2) — reuse `UserService`'s shared tail, do not duplicate the check chain a third time.
- `notify.SMSSender.Send` takes `(ctx, phone, kind SMSKind, params map[string]string)`, not free text (spec §4.1). This touches `RecoveryService.deliver` and `VerificationService.deliver`.
- Migrations follow this repo's goose convention: `migrations/NNNNN_description.sql`, `-- +goose Up` / `-- +goose Down`.
- Same-commit obligations (spec §7): `openapi.yaml` + `redocly lint`; `docs/integrations.md`; `.env.example` + `docs/ops/environment-variables.md`; Playwright e2e for the login-page copy change.

---

## Task 1: `sms_login_codes` migration

**Files:**
- Create: `migrations/00029_sms_login_codes.sql`

**Interfaces:**
- Produces: table `sms_login_codes(id, tenant_id, phone, code_hash, attempts, consumed_at, expires_at, created_at, ip)`, referenced by every later task's queries.

- [ ] **Step 1: Check the next free migration number**

Run: `ls migrations | tail -5`
Expected: the newest file is `00028_invitations.sql`, so the next one is `00029`.

- [ ] **Step 2: Write the migration**

```sql
-- +goose Up

-- The OTP itself, for the standalone phone+code login (§2, §3.1 of
-- docs/superpowers/specs/2026-09-21-sms-otp-login-design.md). Tenant-scoped
-- like every other table here: a code is issued for one tenant's account
-- and must not verify against another tenant's. The abuse-prevention
-- counters (cooldown, per-phone/IP/deployment daily caps) deliberately do
-- NOT live here — see the spec's §3.2 for why they are in-memory instead of
-- a query that would have to skip the tenant_id filter every other query on
-- a scoped table carries.
CREATE TABLE sms_login_codes (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- Same format validateContactDetails already enforces on users.phone:
    -- digits only, optional leading '+'.
    phone TEXT NOT NULL,

    -- sha256(code), not a password hash: the code is 6 digits by design,
    -- there is nothing here for a slow hash to protect against that the
    -- attempt counter and the 5-minute TTL do not already bound. Hashing
    -- only keeps the code out of plaintext at rest.
    code_hash TEXT NOT NULL,

    attempts    INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    ip          TEXT NOT NULL,

    CONSTRAINT uq_sms_login_codes_tenant_id UNIQUE (tenant_id, id)
);

-- The lookup verification runs: the live code for one tenant's phone number.
CREATE INDEX idx_sms_login_codes_tenant_phone ON sms_login_codes (tenant_id, phone, created_at DESC);

-- The retention sweep, same shape as DeleteExpiredPasswordResets.
CREATE INDEX idx_sms_login_codes_expires ON sms_login_codes (expires_at);

COMMENT ON COLUMN sms_login_codes.code_hash IS
    'sha256 of the 6-digit code. Not a password hash -- see the CREATE TABLE comment.';

-- +goose Down
DROP TABLE sms_login_codes;
```

- [ ] **Step 3: Run the migration against the test database**

Run: `go test ./internal/store/... -run TestNothing 2>&1 | head -5` (any package test triggers `testdb`'s migration bootstrap; use the actual test entry point this repo's `internal/testdb` package documents if `TestNothing` does not exist — check `internal/testdb/testdb.go` for how migrations are applied to the test DB, and run any existing store test, e.g. `go test ./internal/store/...`).
Expected: migration applies cleanly, no errors, and `TestTenantScopedQueriesFilterByTenant` (which reads all migrations to build its list of scoped tables) still passes because `sms_login_codes` has a `tenant_id` column and will now be included in that guard automatically.

- [ ] **Step 4: Commit**

```bash
git add migrations/00029_sms_login_codes.sql
git commit -m "$(cat <<'EOF'
db: add sms_login_codes table

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: sqlc queries + `Scoped` wrapper methods for `sms_login_codes`

**Files:**
- Create: `internal/store/queries/sms_login_codes.sql`
- Modify: `internal/store/scoped.go` (append a new `--- sms login codes ---` section, same style as `--- password recovery ---`)

**Interfaces:**
- Consumes: table from Task 1.
- Produces: `(*Scoped) CreateSMSLoginCode(ctx, arg sqlcgen.CreateSMSLoginCodeParams) error`, `GetLiveSMSLoginCode(ctx, phone string, now time.Time) (sqlcgen.SmsLoginCode, error)`, `IncrementSMSLoginCodeAttempts(ctx, id string) error`, `ConsumeSMSLoginCode(ctx, id string, at time.Time) error`, `DeleteExpiredSMSLoginCodes(ctx, before time.Time) error` — every one of these is used by Task 9's `SMSLoginService`.

- [ ] **Step 1: Write the sqlc query file**

```sql
-- name: CreateSMSLoginCode :exec
INSERT INTO sms_login_codes (
    id, tenant_id, phone, code_hash, expires_at, created_at, ip
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetLiveSMSLoginCode :one
-- The newest unconsumed, unexpired code for this tenant's phone number.
-- Newest rather than "the one matching the code" because the caller does
-- not know the code is right yet -- it has to compare the hash itself,
-- and comparing against anything but the most recent request would let an
-- old, superseded code still verify.
SELECT * FROM sms_login_codes
WHERE tenant_id = $1
  AND phone = $2
  AND consumed_at IS NULL
  AND expires_at > $3
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementSMSLoginCodeAttempts :exec
UPDATE sms_login_codes SET attempts = attempts + 1
WHERE tenant_id = $1 AND id = $2;

-- name: ConsumeSMSLoginCode :exec
UPDATE sms_login_codes SET consumed_at = $3
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteExpiredSMSLoginCodes :exec
-- Same retention shape as DeleteExpiredPasswordResets: clears rows well
-- past their usefulness rather than deleting them the instant they expire.
DELETE FROM sms_login_codes
WHERE tenant_id = $1 AND expires_at < $2;
```

- [ ] **Step 2: Generate sqlc code**

Run: `sqlc generate` (from the repo root, using `sqlc.yaml`)
Expected: `internal/store/sqlcgen/` gains `SmsLoginCode`, `CreateSMSLoginCodeParams`, `GetLiveSMSLoginCodeParams`, etc. — no manual edits to generated files.

- [ ] **Step 3: Add the `Scoped` wrapper methods**

Append to `internal/store/scoped.go`, after the `--- password recovery ---` section (near `SupersedePasswordResets`):

```go
// --- sms login codes ------------------------------------------------------

// CreateSMSLoginCode records an outstanding SMS login code.
func (s *Scoped) CreateSMSLoginCode(ctx context.Context, arg sqlcgen.CreateSMSLoginCodeParams) error {
	arg.TenantID = s.tenantID
	return s.q.CreateSMSLoginCode(ctx, arg)
}

// GetLiveSMSLoginCode returns the newest unconsumed, unexpired code for this
// tenant's phone number. A consumed or expired one is not returned at all.
func (s *Scoped) GetLiveSMSLoginCode(ctx context.Context, phone string, now time.Time) (sqlcgen.SmsLoginCode, error) {
	return s.q.GetLiveSMSLoginCode(ctx, sqlcgen.GetLiveSMSLoginCodeParams{
		TenantID: s.tenantID, Phone: phone, ExpiresAt: now,
	})
}

// IncrementSMSLoginCodeAttempts counts one more wrong guess against a code.
func (s *Scoped) IncrementSMSLoginCodeAttempts(ctx context.Context, id string) error {
	return s.q.IncrementSMSLoginCodeAttempts(ctx,
		sqlcgen.IncrementSMSLoginCodeAttemptsParams{TenantID: s.tenantID, ID: id})
}

// ConsumeSMSLoginCode marks a code used, making it single-use.
func (s *Scoped) ConsumeSMSLoginCode(ctx context.Context, id string, at time.Time) error {
	return s.q.ConsumeSMSLoginCode(ctx,
		sqlcgen.ConsumeSMSLoginCodeParams{TenantID: s.tenantID, ID: id, ConsumedAt: &at})
}

// DeleteExpiredSMSLoginCodes clears codes past the retention window.
func (s *Scoped) DeleteExpiredSMSLoginCodes(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredSMSLoginCodes(ctx,
		sqlcgen.DeleteExpiredSMSLoginCodesParams{TenantID: s.tenantID, ExpiresAt: before})
}
```

- [ ] **Step 4: Verify the tenancy guard still passes**

Run: `go test ./internal/store/... -run TestTenantScopedQueriesFilterByTenant -v`
Expected: PASS. Every statement in `sms_login_codes.sql` contains `tenant_id`, so this needs no allowlist changes.

- [ ] **Step 5: Commit**

```bash
git add internal/store/queries/sms_login_codes.sql internal/store/scoped.go internal/store/sqlcgen/
git commit -m "$(cat <<'EOF'
db: add sqlc queries and Scoped wrappers for sms_login_codes

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: In-memory day-window limiter (`httpx.DayLimiter`)

**Files:**
- Create: `internal/httpx/daylimit.go`
- Create: `internal/httpx/daylimit_test.go`

**Interfaces:**
- Produces: `NewDayLimiter(limit int, window time.Duration) *DayLimiter`, `(*DayLimiter) Allow(key string) bool`. Used by Task 9 for phone/day, IP/day, and deployment/day (the last with a constant key).

- [ ] **Step 1: Write the failing test**

```go
package httpx

import "testing"

func TestDayLimiterAllowsUpToLimit(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	l := NewDayLimiter(3, 24*time.Hour)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("phone-1") {
			t.Fatalf("attempt %d: want allowed", i)
		}
	}
	if l.Allow("phone-1") {
		t.Fatal("4th attempt within the window: want refused")
	}
	// A different key has its own allowance.
	if !l.Allow("phone-2") {
		t.Fatal("a different key should not share phone-1's count")
	}
}

func TestDayLimiterResetsAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	l := NewDayLimiter(1, 24*time.Hour)
	l.now = func() time.Time { return now }

	if !l.Allow("phone-1") {
		t.Fatal("first attempt: want allowed")
	}
	if l.Allow("phone-1") {
		t.Fatal("second attempt inside the window: want refused")
	}

	// Nine at 09:00 the next morning does not carry the earlier count: a
	// token bucket would have refilled continuously by then, but this is a
	// day window, so 09:00 the next day is a fresh window regardless of
	// how much of the previous day's allowance was used and when.
	now = now.Add(24*time.Hour + time.Minute)
	if !l.Allow("phone-1") {
		t.Fatal("attempt a day later: want allowed, window reset")
	}
}
```

Add `import "time"` to the test file's import block alongside `"testing"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/httpx/... -run TestDayLimiter -v`
Expected: FAIL — `NewDayLimiter` undefined.

- [ ] **Step 3: Write the implementation**

```go
package httpx

import (
	"sync"
	"time"
)

// dayLimiterIdleTimeout mirrors bucketIdleTimeout in ratelimit.go: how long
// a key's window outlives its last request, so a flood does not leave a map
// entry per key forever.
const dayLimiterIdleTimeout = 25 * time.Hour

// dayWindow is one key's count within its current window.
type dayWindow struct {
	windowStart time.Time
	count       int
	lastSeen    time.Time
}

// DayLimiter caps how many times a key may be allowed within a fixed
// window, reset entirely once the window passes rather than refilling
// continuously.
//
// This is deliberately not RateLimiter (ratelimit.go): a token bucket
// configured for "N per day" by setting perMinute to N/1440 refills
// continuously, so five uses at 09:00 do not block a sixth at 13:00 -- the
// cap silently stops being a daily cap. A day window resets as a whole,
// which is the right shape for "how many SMS codes has this phone number
// received today."
//
// Per-process, like RateLimiter: counts reset on restart and are not
// shared across instances. That is the same tradeoff RateLimiter already
// makes for password brute-force protection; see its doc comment. These
// counters protect a deployment's shared SMS budget, which is exactly the
// kind of thing that already lives outside the tenant-scoped data model
// (see docs/superpowers/specs/2026-09-21-sms-otp-login-design.md §3.2).
type DayLimiter struct {
	limit  int
	window time.Duration

	// now is time.Now, replaced in tests.
	now func() time.Time

	mu        sync.Mutex
	windows   map[string]*dayWindow
	lastSweep time.Time
}

// NewDayLimiter returns a limiter allowing limit uses of a key within
// window, reset as a whole once window has passed since the key's first use
// in its current window.
func NewDayLimiter(limit int, window time.Duration) *DayLimiter {
	return &DayLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		windows: map[string]*dayWindow{},
	}
}

// Allow reports whether key may be used once more, and counts it if so.
func (l *DayLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	w, ok := l.windows[key]
	if !ok || now.Sub(w.windowStart) >= l.window {
		w = &dayWindow{windowStart: now}
		l.windows[key] = w
	}
	w.lastSeen = now

	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// sweep drops windows nobody has used recently. Called with the lock held.
func (l *DayLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < dayLimiterIdleTimeout {
		return
	}
	l.lastSweep = now
	for key, w := range l.windows {
		if now.Sub(w.lastSeen) > dayLimiterIdleTimeout {
			delete(l.windows, key)
		}
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/httpx/... -run TestDayLimiter -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpx/daylimit.go internal/httpx/daylimit_test.go
git commit -m "$(cat <<'EOF'
httpx: add DayLimiter for fixed-window per-key rate limits

RateLimiter's token bucket refills continuously, which is wrong for
a daily cap: 5 uses at 09:00 would not block a 6th at 13:00. DayLimiter
resets the whole window at once instead. Needed by the upcoming SMS
login rate limits, in-memory by design -- see
docs/superpowers/specs/2026-09-21-sms-otp-login-design.md §3.2.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `notify.SMSSender` interface change (kind + params, not free text)

**Files:**
- Modify: `internal/notify/notify.go`
- Modify: `internal/notify/notify_test.go` (or wherever `NotConfiguredSMS` is tested — locate it first with `grep -rn "NotConfiguredSMS" internal/notify/*_test.go`; if no such test file exists, create `internal/notify/sms_test.go`)

**Interfaces:**
- Produces: `type SMSKind string` with `SMSKindLoginCode`, `SMSKindRecovery`, `SMSKindVerification`; `SMSSender.Send(ctx, phone string, kind SMSKind, params map[string]string) error`. Consumed by Task 5 (Aliyun sender), Task 6 (recovery/verification call sites), Task 9 (SMS login).

- [ ] **Step 1: Write the failing test**

```go
package notify

import (
	"context"
	"errors"
	"testing"
)

func TestNotConfiguredSMSAlwaysFails(t *testing.T) {
	var s SMSSender = NotConfiguredSMS{}
	err := s.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456", "Minutes": "5"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails to compile**

Run: `go test ./internal/notify/... -run TestNotConfiguredSMSAlwaysFails -v`
Expected: FAIL — `SMSKindLoginCode` undefined, `Send` signature mismatch.

- [ ] **Step 3: Change the interface**

In `internal/notify/notify.go`, replace:

```go
// SMSSender sends a text message.
//
// Every provider has its own API, so this is the seam rather than a
// half-hearted attempt at a common one. Implementing it against a provider
// means one type with one method; nothing above this interface changes.
type SMSSender interface {
	Send(ctx context.Context, phone, text string) error
}
```

with:

```go
// SMSKind is which purpose a message serves, and therefore which template a
// provider that requires one (Aliyun) sends it through.
//
// A provider that has no such requirement is free to ignore this and treat
// every kind the same; the type exists for the providers that cannot.
type SMSKind string

const (
	// SMSKindLoginCode is a standalone phone+code sign-in. Params: "Code",
	// "Minutes".
	SMSKindLoginCode SMSKind = "login_code"
	// SMSKindRecovery is a password-reset link sent over SMS. Params:
	// "Link", "Minutes".
	SMSKindRecovery SMSKind = "recovery"
	// SMSKindVerification is a registration address-proof link sent over
	// SMS. Params: "Link", "Hours".
	SMSKindVerification SMSKind = "verification"
)

// SMSSender sends a text message of one of the kinds above.
//
// Every provider has its own API, so this is the seam rather than a
// half-hearted attempt at a common one. Implementing it against a provider
// means one type with one method; nothing above this interface changes.
//
// Params rather than a rendered string: providers that require an
// approved template (Aliyun, and every mainland Chinese carrier gateway)
// cannot send arbitrary text -- they send a fixed, reviewed template with
// named variables filled in, and reviewing a template with a raw URL or an
// unbounded sentence in it is not something a provider grants. A sender
// that has no such requirement (there is none yet) is free to render these
// into a sentence of its own.
type SMSSender interface {
	Send(ctx context.Context, phone string, kind SMSKind, params map[string]string) error
}
```

And replace:

```go
// Send always fails with ErrNotConfigured.
func (NotConfiguredSMS) Send(context.Context, string, string) error { return ErrNotConfigured }
```

with:

```go
// Send always fails with ErrNotConfigured.
func (NotConfiguredSMS) Send(context.Context, string, SMSKind, map[string]string) error {
	return ErrNotConfigured
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/notify/... -v`
Expected: PASS, including every pre-existing test in the package (this is a breaking signature change; anything else referencing the old signature will now fail to compile — that is Task 6).

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go internal/notify/*_test.go
git commit -m "$(cat <<'EOF'
notify: change SMSSender to take a kind and named params, not free text

Aliyun (and every mainland Chinese SMS gateway) requires a
pre-approved template with named variables; it cannot send arbitrary
text. This is the seam change that makes a real transport possible --
callers stop rendering a sentence and start passing the kind of
message plus its variables. Breaks RecoveryService and
VerificationService's SMS branches, fixed in the next commit.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Aliyun Dysmsapi `SMSSender`

**Files:**
- Create: `internal/notify/aliyun_sms.go`
- Create: `internal/notify/aliyun_sms_test.go`

**Interfaces:**
- Consumes: `SMSSender`, `SMSKind`, `ErrNotConfigured` from Task 4.
- Produces: `AliyunSMSConfig{AccessKeyID, AccessKeySecret, SignName string, TemplateCodes map[SMSKind]string, Endpoint string}`, `NewAliyunSMSSender(cfg AliyunSMSConfig) (SMSSender, error)`. Consumed by Task 10 (config wiring).

- [ ] **Step 1: Write the failing tests**

```go
package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAliyunSMSSenderRequiresCoreFields(t *testing.T) {
	cases := []AliyunSMSConfig{
		{AccessKeySecret: "s", SignName: "n"},
		{AccessKeyID: "k", SignName: "n"},
		{AccessKeyID: "k", AccessKeySecret: "s"},
	}
	for _, cfg := range cases {
		if _, err := NewAliyunSMSSender(cfg); err == nil {
			t.Errorf("cfg %+v: want an error for a missing required field", cfg)
		}
	}
}

func TestAliyunSMSSenderSendsTheConfiguredTemplate(t *testing.T) {
	var gotQuery map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Code": "OK", "Message": "OK"})
	}))
	defer server.Close()

	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
		SignName:        "Portico",
		TemplateCodes:   map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
		Endpoint:        server.URL,
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}

	err = sender.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456", "Minutes": "5"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := gotQuery.Get("PhoneNumbers"); got != "+8613800138000" {
		t.Errorf("PhoneNumbers = %q", got)
	}
	if got := gotQuery.Get("TemplateCode"); got != "SMS_000001" {
		t.Errorf("TemplateCode = %q", got)
	}
	if got := gotQuery.Get("SignName"); got != "Portico" {
		t.Errorf("SignName = %q", got)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(gotQuery.Get("TemplateParam")), &params); err != nil {
		t.Fatalf("TemplateParam is not JSON: %v", err)
	}
	if params["Code"] != "123456" || params["Minutes"] != "5" {
		t.Errorf("TemplateParam = %v", params)
	}
	if gotQuery.Get("Signature") == "" {
		t.Error("request was not signed")
	}
}

func TestAliyunSMSSenderRefusesAnUnconfiguredKind(t *testing.T) {
	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID: "key", AccessKeySecret: "secret", SignName: "Portico",
		TemplateCodes: map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}
	err = sender.Send(context.Background(), "+8613800138000", SMSKindRecovery, nil)
	if err == nil {
		t.Fatal("want an error: no template configured for SMSKindRecovery")
	}
}

func TestAliyunSMSSenderReportsAGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Code": "isv.BUSINESS_LIMIT_CONTROL", "Message": "触发分钟级流控Permits:1",
		})
	}))
	defer server.Close()

	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID: "key", AccessKeySecret: "secret", SignName: "Portico",
		TemplateCodes: map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
		Endpoint:      server.URL,
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}
	err = sender.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456"})
	if err == nil {
		t.Fatal("want an error when Aliyun's Code is not OK")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/notify/... -run TestAliyunSMS -v` and `go test ./internal/notify/... -run TestNewAliyunSMS -v`
Expected: FAIL — `AliyunSMSConfig`, `NewAliyunSMSSender` undefined.

- [ ] **Step 3: Pin the signing scheme, then implement**

Before writing `sign`, fetch and read Aliyun's current Dysmsapi RPC signing documentation (query-parameter HMAC-SHA1, `SignatureMethod=HMAC-SHA1`, `SignatureVersion=1.0`, percent-encoding per RFC 3986, the `POST&%2F&<sorted, encoded query string>` string-to-sign, `<AccessKeySecret>&` as the HMAC key) — do not write this from memory. Confirm the exact percent-encoding exceptions (`*` must be encoded as `%2A`, `~` must NOT be encoded) against the current page before coding `percentEncode`.

```go
package notify

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const aliyunSMSEndpoint = "https://dysmsapi.aliyuncs.com/"

// AliyunSMSConfig describes sending SMS through Alibaba Cloud's Dysmsapi.
//
// Every message this project sends over SMS goes through a template Aliyun
// has reviewed and approved in advance -- see the SMSKind doc comment on
// why. TemplateCodes is therefore a map rather than one field: a login code,
// a recovery link, and a verification link are three different reviewed
// templates with three different variable names, and a deployment may have
// approval for only some of them.
type AliyunSMSConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	// SignName is the approved SMS signature shown before the message body,
	// e.g. "【Portico】". Required by Aliyun on every send.
	SignName string
	// TemplateCodes maps each SMSKind this deployment can send to the
	// template code Aliyun issued for it. A kind absent from this map is
	// refused at Send time, not at construction: a deployment may have
	// approval for the login-code template only, and still is not asking
	// this to fail before it ever tries to use the one it has.
	TemplateCodes map[SMSKind]string
	// Endpoint overrides the API address; empty means Aliyun's default.
	// Set by tests.
	Endpoint string
	// client is the HTTP client, overridden by tests.
	client *http.Client
}

type aliyunSMSSender struct {
	cfg    AliyunSMSConfig
	client *http.Client
}

// NewAliyunSMSSender builds an SMSSender backed by Alibaba Cloud's
// Dysmsapi.
//
// The three account-identifying fields are required and checked here,
// reported at startup rather than at the first sign-in. TemplateCodes is
// allowed to be a subset of SMSKind -- see the field's doc comment.
func NewAliyunSMSSender(cfg AliyunSMSConfig) (SMSSender, error) {
	if cfg.AccessKeyID == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_ACCESS_KEY_ID is required")
	}
	if cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_ACCESS_KEY_SECRET is required")
	}
	if cfg.SignName == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_SIGN_NAME is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = aliyunSMSEndpoint
	}
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 10 * time.Second}
	}
	return &aliyunSMSSender{cfg: cfg, client: cfg.client}, nil
}

// Send posts one message through Dysmsapi's SendSms action.
func (a *aliyunSMSSender) Send(ctx context.Context, phone string, kind SMSKind, params map[string]string) error {
	templateCode, ok := a.cfg.TemplateCodes[kind]
	if !ok {
		return fmt.Errorf("notify: no Aliyun template configured for SMS kind %q", kind)
	}

	paramJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal template params: %w", err)
	}

	nonce, err := randomNonce()
	if err != nil {
		return err
	}

	query := url.Values{
		"AccessKeyId":      {a.cfg.AccessKeyID},
		"Action":           {"SendSms"},
		"Format":           {"JSON"},
		"PhoneNumbers":     {phone},
		"SignName":         {a.cfg.SignName},
		"SignatureMethod":  {"HMAC-SHA1"},
		"SignatureNonce":   {nonce},
		"SignatureVersion": {"1.0"},
		"TemplateCode":     {templateCode},
		"TemplateParam":    {string(paramJSON)},
		"Timestamp":        {time.Now().UTC().Format("2006-01-02T15:04:05Z")},
		"Version":          {"2017-05-25"},
	}
	query.Set("Signature", a.sign(http.MethodGet, query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.Endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build Aliyun SMS request: %w", err)
	}

	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Aliyun SMS: %w", err)
	}
	defer res.Body.Close()

	var body struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode Aliyun SMS response: %w", err)
	}
	if body.Code != "OK" {
		return fmt.Errorf("Aliyun SMS gateway refused: %s: %s", body.Code, body.Message)
	}
	return nil
}

// sign computes Dysmsapi's RPC-style signature: HMAC-SHA1 over
// "<method>&<percent-encoded '/'>&<percent-encoded, sorted query string>",
// keyed by "<AccessKeySecret>&".
func (a *aliyunSMSSender) sign(method string, query url.Values) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, percentEncode(k)+"="+percentEncode(query.Get(k)))
	}
	canonical := strings.Join(pairs, "&")

	toSign := method + "&" + percentEncode("/") + "&" + percentEncode(canonical)

	mac := hmac.New(sha1.New, []byte(a.cfg.AccessKeySecret+"&"))
	mac.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// percentEncode follows Aliyun's RFC 3986 variant: url.QueryEscape's output
// is adjusted because Aliyun additionally requires '~' left un-encoded and
// '*' encoded (the reverse of Go's default), and a literal space encoded as
// %20 rather than QueryEscape's '+'.
func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func randomNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate signature nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
```

Add `"context"` to the import block (used in the `Send` signature).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/notify/... -v`
Expected: PASS. If `TestAliyunSMSSenderSendsTheConfiguredTemplate` fails on the `Signature` assertion or on percent-encoding, re-check step 3's documentation fetch — do not adjust the test to match a guessed implementation.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/aliyun_sms.go internal/notify/aliyun_sms_test.go
git commit -m "$(cat <<'EOF'
notify: add Aliyun Dysmsapi SMS transport

RPC-style HMAC-SHA1 request signing, hand-written against Aliyun's
current documentation -- no SDK dependency, matching this package's
existing Mailer/Resend style. One template code per SMSKind, since
each is a separately-reviewed Aliyun template.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Fix `RecoveryService` and `VerificationService` for the new `SMSSender` signature

**Files:**
- Modify: `internal/service/recovery.go`
- Modify: `internal/service/verification.go`
- Modify: `internal/i18n/i18n.go` (remove `KeyRecoverySMS`, `KeyVerificationSMS`)
- Modify: `internal/i18n/messages/en-US/mail.json`, `internal/i18n/messages/zh-CN/mail.json` (remove `"recovery.sms"`, `"verification.sms"`)

**Interfaces:**
- Consumes: `notify.SMSKindRecovery`, `notify.SMSKindVerification` from Task 4.

- [ ] **Step 1: Run the build to see the breakage Task 4 caused**

Run: `go build ./... 2>&1 | head -30`
Expected: compile errors in `internal/service/recovery.go` and `internal/service/verification.go` at the `s.sms.Send(ctx, row.Phone, text)` call sites (wrong argument count/types), and an "unused variable" or similar around the `i18n.Render(..., i18n.KeyRecoverySMS, ...)` call once its result is no longer consumed the old way.

- [ ] **Step 2: Fix `recovery.go`**

In `internal/service/recovery.go`, inside `deliver`, replace:

```go
	case model.RecoverySMS:
		text, renderErr := s.messages.Render(locale, i18n.KeyRecoverySMS, data)
		if renderErr != nil {
			return renderErr
		}
		err = s.sms.Send(ctx, row.Phone, text)
```

with:

```go
	case model.RecoverySMS:
		// No i18n.Render here: the message text lives in Aliyun's approved
		// template now, not in this codebase -- see notify.SMSKind.
		err = s.sms.Send(ctx, row.Phone, notify.SMSKindRecovery, map[string]string{
			"Link":    link,
			"Minutes": strconv.Itoa(int(RecoveryTokenTTL.Minutes())),
		})
```

Add `"strconv"` to the import block if not already present (check first with `grep -n '"strconv"' internal/service/recovery.go`).

`locale` is still used by the `model.RecoveryEmail` branch above, so leave its assignment alone; if `data` (the `i18n.RecoveryData` value) becomes unused outside the email branch, that is fine — it is still passed into `s.email(...)` for that branch.

- [ ] **Step 3: Fix `verification.go`**

In `internal/service/verification.go`, inside `deliver`, replace:

```go
	case model.RecoverySMS:
		text, err := s.messages.Render(locale, i18n.KeyVerificationSMS, data)
		if err != nil {
			return err
		}
		return s.sms.Send(ctx, row.Phone, text)
```

with:

```go
	case model.RecoverySMS:
		return s.sms.Send(ctx, row.Phone, notify.SMSKindVerification, map[string]string{
			"Link":  link,
			"Hours": strconv.Itoa(hours),
		})
```

Add `"strconv"` to the import block if not already present.

- [ ] **Step 4: Remove the now-dead i18n keys and messages**

In `internal/i18n/i18n.go`, remove the `KeyRecoverySMS` and `KeyVerificationSMS` lines from the `const` block (leave the comment above `KeyRecoverySMS` describing why SMS messages are one string — delete that comment too, since it is no longer true of anything in this file).

In `internal/i18n/messages/en-US/mail.json` and `internal/i18n/messages/zh-CN/mail.json`, remove the `"recovery.sms"` and `"verification.sms"` entries.

- [ ] **Step 5: Run the package tests**

Run: `go build ./... && go test ./internal/service/... ./internal/i18n/... -v 2>&1 | tail -60`
Expected: build succeeds; PASS on every test. Watch specifically for an i18n catalogue test that asserts every locale defines the same key set — if `TestCatalogHasEveryKeyInEveryLocale` (or similarly named) exists, it should still pass because both `.json` files lost the same two keys together. If it fails, one locale's file was edited and the other was not — fix the mismatch, do not add the key back to one side only.

- [ ] **Step 6: Commit**

```bash
git add internal/service/recovery.go internal/service/verification.go internal/i18n/i18n.go internal/i18n/messages/en-US/mail.json internal/i18n/messages/zh-CN/mail.json
git commit -m "$(cat <<'EOF'
service: adapt recovery/verification SMS to the templated SMSSender

Follows the interface change in notify: these two branches stop
rendering a sentence through i18n and start passing the link/TTL as
named template variables, which is what actually reaches Aliyun's
approved template. The two SMS-only i18n keys and their translations
have nowhere left to render, so they go with this change.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `SMSLoginEnabled` tenant setting

**Files:**
- Modify: `internal/service/settings.go`
- Modify: `internal/service/settings_test.go` (locate the existing `RegistrationVerification` round-trip test with `grep -n RegistrationVerification internal/service/settings_test.go` and add a sibling test next to it)

**Interfaces:**
- Produces: `Settings.SMSLoginEnabled bool` (json `"smsLoginEnabled"`), `SettingSMSLoginEnabled = "sms_login_enabled"`, `(*SettingsService) CanDeliverSMS() bool`. Consumed by Task 9 (refusing to issue codes when off) and Task 12 (exposing the flag to the login screen).

- [ ] **Step 1: Write the failing test**

Add near the existing `RegistrationVerification` settings test:

```go
func TestSMSLoginRequiresADeliverableSMSChannel(t *testing.T) {
	svc, tenantID := newSettingsServiceForTest(t) // use whatever helper the existing RegistrationVerification test uses to build a *SettingsService
	svc.WithDeliveryChannels(func() []model.RecoveryChannel { return nil }) // no SMS configured

	current, err := svc.Get(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	current.SMSLoginEnabled = true
	_, err = svc.Update(context.Background(), tenantID, current)
	if err == nil {
		t.Fatal("want an error: no SMS channel is configured")
	}

	svc.WithDeliveryChannels(func() []model.RecoveryChannel { return []model.RecoveryChannel{model.RecoverySMS} })
	saved, err := svc.Update(context.Background(), tenantID, current)
	if err != nil {
		t.Fatalf("Update with SMS configured: %v", err)
	}
	if !saved.SMSLoginEnabled {
		t.Error("SMSLoginEnabled did not persist")
	}
}
```

Adjust the helper call (`newSettingsServiceForTest`) and the `Update` method name/signature to whatever the file already uses for `RegistrationVerification` — read that test first and mirror its exact setup rather than guessing the helper's name.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/service/... -run TestSMSLoginRequiresADeliverableSMSChannel -v`
Expected: FAIL — `SMSLoginEnabled` undefined on `Settings`.

- [ ] **Step 3: Add the setting**

In `internal/service/settings.go`, add the key constant next to `SettingInvitationOnlyRegistration`:

```go
	// SettingSMSLoginEnabled turns on the standalone phone-number + SMS-code
	// login method. Off by default, and refused at the point of turning it
	// on if this deployment cannot actually send SMS -- see
	// SettingsService.Update, same reasoning as SettingRegistrationVerification.
	SettingSMSLoginEnabled = "sms_login_enabled"
```

Add the field to `Settings`, next to `InvitationOnlyRegistration`:

```go
	// SMSLoginEnabled turns on phone-number + SMS-code sign-in as an
	// alternative to username/password. Requires this deployment to have a
	// real SMS sender configured; see SettingsService.Update.
	SMSLoginEnabled bool `json:"smsLoginEnabled"`
```

In the `Get` method's `switch row.Key`, add:

```go
		case SettingSMSLoginEnabled:
			loaded.SMSLoginEnabled = row.Value == "true"
```

In `Update`'s validation, next to the `RegistrationVerification`/`CanDeliver()` check:

```go
	// Same refusal as RegistrationVerification, and for the same reason:
	// turning this on when nothing can send SMS would accept the setting
	// and then strand a login screen offering a button nobody can use.
	if next.SMSLoginEnabled && !s.CanDeliverSMS() {
		return Settings{}, httpx.UnprocessableEntity("NO_SMS_CHANNEL",
			"SMS login needs a way to send SMS. Configure the Aliyun SMS transport and restart, "+
				"or leave SMS login off.")
	}
```

In the `values := map[string]string{...}` persisted at the end of `Update`, add:

```go
		SettingSMSLoginEnabled: strconv.FormatBool(next.SMSLoginEnabled),
```

Add the `CanDeliverSMS` method next to `CanDeliver`:

```go
// CanDeliverSMS reports whether this deployment has a real SMS sender
// configured, independent of whether email is also configured. CanDeliver
// answers "can it send anything" and is not specific enough for a setting
// that only needs SMS.
func (s *SettingsService) CanDeliverSMS() bool {
	if s.deliverable == nil {
		return false
	}
	for _, channel := range s.deliverable() {
		if channel == model.RecoverySMS {
			return true
		}
	}
	return false
}
```

Add `"slices"` to the import block only if you use `slices.Contains` instead of the loop above — the loop needs no new import.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/service/... -run TestSMSLoginRequiresADeliverableSMSChannel -v`
Expected: PASS.

- [ ] **Step 5: Run the full settings test file to check nothing else broke**

Run: `go test ./internal/service/... -run TestSettings -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/settings.go internal/service/settings_test.go
git commit -m "$(cat <<'EOF'
service: add the SMSLoginEnabled tenant setting

Follows RegistrationVerification's pattern exactly: off by default,
refused at the point of turning on if the deployment cannot actually
send SMS. CanDeliverSMS is separate from CanDeliver because a
deployment can have email but not SMS, or the reverse.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Extract `UserService`'s shared post-credential session tail

**Files:**
- Modify: `internal/service/auth_flow.go`

**Interfaces:**
- Produces: `(s *UserService) issueSessionForUser(ctx context.Context, tenant model.Tenant, row sqlcgen.User, ip, userAgent, auditDetail string) (Session, error)`. Consumed by Task 9's `SMSLoginService` (via a `*UserService` reference) and by the refactored `IssueSessionForExternalIdentity` in this same task.
- Consumes: nothing new — this is a pure refactor of existing code, no behavior change.

- [ ] **Step 1: Confirm the behavior this refactor must not change**

Run: `go test ./internal/service/... ./internal/server/... -run TestExternalIdentity -v` (adjust the `-run` pattern to whatever test names actually cover `IssueSessionForExternalIdentity` — find them first with `grep -rln "IssueSessionForExternalIdentity" internal/service internal/server`)
Expected: PASS, and note the exact set of passing test names so step 4 can re-run the same set.

- [ ] **Step 2: Extract the shared tail**

In `internal/service/auth_flow.go`, replace the body of `IssueSessionForExternalIdentity` from the `row.LockedUntil` check onward with a call to a new private method, and move that code into the new method verbatim (no logic changes — this step is pure extraction):

```go
func (s *UserService) IssueSessionForExternalIdentity(ctx context.Context, tenant model.Tenant, userID, ip, userAgent string) (session Session, err error) {
	defer func() { s.metrics.RecordSignIn(signInOutcome(err)) }()

	q := s.store.ForTenant(tenant.ID)

	row, err := q.GetUserByID(ctx, userID)
	if err != nil {
		if store.IsNoRows(err) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("look up user: %w", err)
	}

	return s.issueSessionForUser(ctx, tenant, row, ip, userAgent, "external identity provider")
}

// issueSessionForUser is everything a sign-in does once the credential
// itself has already been checked by the caller: the account has to still
// be usable, and then it gets a session.
//
// Shared between IssueSessionForExternalIdentity and SMSLoginService's
// LoginWithCode, which check two different credentials (a provider's
// word, a phone code) and agree on everything past that -- a disabled
// account is disabled whoever vouched for it, and a locked one is locked.
// A third copy of this chain, next to Login's, is exactly the shape of
// thing that gets a check added to two of three paths and not the third.
//
// PasswordPolicy().Expired and MustChangePassword are deliberately absent,
// same as in the caller this was extracted from: both are conditions on
// the password, and neither caller is presenting one.
//
// The lockout counter (FailedLoginAttempts / LockedUntil) is not touched
// here in either direction. It counts password guesses; a wrong SMS code
// is counted by sms_login_codes.attempts instead, and a successful sign-in
// through a different credential is not evidence that whoever was guessing
// the password has stopped.
func (s *UserService) issueSessionForUser(ctx context.Context, tenant model.Tenant, row sqlcgen.User, ip, userAgent, auditDetail string) (Session, error) {
	if row.LockedUntil != nil && row.LockedUntil.After(store.Now()) {
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account locked")
		return Session{}, ErrAccountLocked
	}
	if model.Status(row.Status) != model.StatusActive {
		if row.ClosedAt != nil {
			s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account closed by its owner")
			return Session{}, ErrAccountClosed
		}
		s.logLoginFailure(ctx, tenant.ID, row.ID, row.Username, ip, "account disabled")
		return Session{}, ErrAccountDisabled
	}

	user, err := s.Get(ctx, tenant.ID, row.ID)
	if err != nil {
		return Session{}, err
	}

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return Session{}, err
	}

	q := s.store.ForTenant(tenant.ID)
	sessionID := uuid.NewString()
	now := store.Now()
	ttl := settings.TokenTTL()

	if err := q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID: sessionID, UserID: user.ID, Ip: ip, UserAgent: userAgent,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}); err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	token, expiresAt, err := s.tokens.Issue(user, tenant.Code, sessionID, row.TokenVersion, ttl)
	if err != nil {
		return Session{}, err
	}
	s.metrics.RecordTokenIssued(metrics.TokenSession)

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogLogin, Action: model.ActionLoginSuccess,
		Result:  model.LogSuccess,
		ActorID: user.ID, ActorName: user.Username,
		IP: ip, Detail: auditDetail,
	})

	return Session{Token: token, ExpiresAt: expiresAt, User: user}, nil
}
```

`auditDetail` is `"external identity provider"` for the existing caller and will be a login-specific string (Task 9) for the new one — this is the one behavior-preserving parameter change: the old code hard-coded that string inline, so pin it exactly at the call site to keep this step a pure extraction.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Re-run the tests noted in Step 1**

Run the exact `-run` pattern found in Step 1.
Expected: PASS, same test names, same count — this proves the extraction changed nothing observable.

- [ ] **Step 5: Commit**

```bash
git add internal/service/auth_flow.go
git commit -m "$(cat <<'EOF'
service: extract UserService's post-credential session tail

Pure refactor, no behavior change (verified by re-running the
existing IssueSessionForExternalIdentity tests unchanged). Makes the
tail reusable by the upcoming SMS login, which checks a different
credential but agrees with external-identity sign-in on everything
after it.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `SMSLoginService` — request code, verify code, issue session

**Files:**
- Create: `internal/service/sms_login.go`
- Create: `internal/service/sms_login_test.go`

**Interfaces:**
- Consumes: `httpx.RateLimiter`/`httpx.DayLimiter` (Tasks 3), `notify.SMSSender`/`SMSKindLoginCode` (Task 4), `Scoped.CreateSMSLoginCode`/`GetLiveSMSLoginCode`/`IncrementSMSLoginCodeAttempts`/`ConsumeSMSLoginCode` (Task 2), `SettingsService.CanDeliverSMS`/`Settings.SMSLoginEnabled` (Task 7), `UserService.issueSessionForUser` (Task 8, same package so directly callable).
- Produces: `NewSMSLoginService(st *store.Store, users *UserService, settings *SettingsService, audit *AuditService, metrics *metrics.Registry, sms notify.SMSSender) *SMSLoginService`, `(*SMSLoginService) RequestCode(ctx, tenant model.Tenant, phone, ip string) error`, `(*SMSLoginService) LoginWithCode(ctx, tenant model.Tenant, phone, code, ip, userAgent string) (Session, error)`. Consumed by Task 11 (server wiring) and Task 12 (handlers).

- [ ] **Step 1: Write the failing tests**

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/notify"
)

// recordingSMS captures every Send call, for asserting what a code request
// actually sent -- and how many times.
type recordingSMS struct {
	sent []struct {
		phone  string
		kind   notify.SMSKind
		params map[string]string
	}
}

func (r *recordingSMS) Send(_ context.Context, phone string, kind notify.SMSKind, params map[string]string) error {
	r.sent = append(r.sent, struct {
		phone  string
		kind   notify.SMSKind
		params map[string]string
	}{phone, kind, params})
	return nil
}

func TestSMSLoginRequestCodeIsSilentWhetherOrNotThePhoneExists(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t) // build using this test file's existing fixtures -- mirror whatever recovery_window_test.go or settings_test.go in this same package already uses to get a *store.Store + seeded tenant + user with a bound phone

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode (existing phone): %v", err)
	}
	if err := svc.RequestCode(context.Background(), tenant, "+8619999999999", "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode (unknown phone): %v", err)
	}

	waitForSMS(t, sms, 1) // helper: polls sms.sent, same reasoning as recordingMailer.waitFor in internal/server/recovery_test.go -- delivery happens after the response
	if len(sms.sent) != 1 {
		t.Fatalf("sent %d messages, want exactly 1 (only the existing phone)", len(sms.sent))
	}
	if sms.sent[0].phone != existingPhone {
		t.Errorf("sent to %q, want %q", sms.sent[0].phone, existingPhone)
	}
	if sms.sent[0].kind != notify.SMSKindLoginCode {
		t.Errorf("kind = %q, want SMSKindLoginCode", sms.sent[0].kind)
	}
}

func TestSMSLoginVerifyRefusesWithOneGenericErrorWhateverTheReason(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	waitForSMS(t, sms, 1)
	code := sms.sent[0].params["Code"]

	cases := []struct {
		name  string
		phone string
		code  string
	}{
		{"wrong code", existingPhone, "000000"},
		{"unknown phone", "+8619999999999", code},
	}
	for _, c := range cases {
		if c.code == code && c.phone == existingPhone {
			t.Fatalf("test bug: %q accidentally uses the real code", c.name)
		}
		_, err := svc.LoginWithCode(context.Background(), tenant, c.phone, c.code, "1.2.3.4", "test-agent")
		if err != ErrInvalidCredentials {
			t.Errorf("%s: err = %v, want ErrInvalidCredentials", c.name, err)
		}
	}

	// The real code still works afterwards -- the wrong attempts above must
	// not have consumed or blocked it.
	session, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent")
	if err != nil {
		t.Fatalf("LoginWithCode with the real code: %v", err)
	}
	if session.Token == "" {
		t.Error("want a session token")
	}

	// And it is single-use: asking again with the same code fails now that
	// it is consumed.
	_, err = svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent")
	if err != ErrInvalidCredentials {
		t.Errorf("reusing a consumed code: err = %v, want ErrInvalidCredentials", err)
	}
}

func TestSMSLoginLocksOutAfterFiveWrongAttempts(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)
	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	waitForSMS(t, sms, 1)
	code := sms.sent[0].params["Code"]

	for i := 0; i < 5; i++ {
		if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, "000000", "1.2.3.4", "test-agent"); err != ErrInvalidCredentials {
			t.Fatalf("wrong attempt %d: err = %v", i, err)
		}
	}
	// The 6th attempt uses the *real* code, but the code should already be
	// dead from too many wrong guesses.
	if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent"); err != ErrInvalidCredentials {
		t.Errorf("real code after 5 wrong attempts: err = %v, want ErrInvalidCredentials (code should be dead)", err)
	}
}

func TestSMSLoginCooldownIsPerPhoneAcrossTenants(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)
	otherTenant := newSMSLoginTestSecondTenant(t, svc) // second tenant sharing the store, with a different account bound to the SAME existingPhone value

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("first tenant's request: %v", err)
	}
	waitForSMS(t, sms, 1)

	// Same phone number, a different tenant's account, immediately after:
	// must not get a second message out of the shared cooldown.
	if err := svc.RequestCode(context.Background(), otherTenant, existingPhone, "5.6.7.8"); err != nil {
		t.Fatalf("second tenant's request: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // give any (incorrect) async send a chance to land
	if len(sms.sent) != 1 {
		t.Errorf("sent %d messages, want still 1 -- cooldown must be shared across tenants for the same phone", len(sms.sent))
	}
}
```

Write `newSMSLoginTestService`, `newSMSLoginTestSecondTenant`, and `waitForSMS` as this test file's own helpers: read `internal/service/recovery_window_test.go` and `internal/service/settings_test.go` first for how this package already builds a `*store.Store` + tenant + user fixture in a test, and mirror that exactly rather than inventing a new fixture style. `waitForSMS` polls `len(sms.sent)` with a deadline, the same shape as `recordingMailer.waitFor` in `internal/server/recovery_test.go`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/service/... -run TestSMSLogin -v`
Expected: FAIL — `SMSLoginService` undefined.

- [ ] **Step 3: Implement `SMSLoginService`**

```go
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
)

// SMSLoginCodeTTL is how long a code stays usable.
//
// Short, because unlike a password-reset link this is not sitting in a
// mailbox waiting for a person to get back to their laptop -- it is typed
// back into the same form within seconds.
const SMSLoginCodeTTL = 5 * time.Minute

// SMSLoginMaxAttempts is how many wrong guesses one code tolerates before
// it is dead, without waiting for it to expire on its own.
const SMSLoginMaxAttempts = 5

// SMSLoginCooldown is the minimum spacing between two codes sent to the
// same phone number, enforced in memory -- see internal/httpx.DayLimiter's
// doc comment and docs/superpowers/specs/2026-09-21-sms-otp-login-design.md
// §3.2 for why this is not a database column.
const SMSLoginCooldown = 60 * time.Second

// SMSLoginPerPhonePerDay, SMSLoginPerIPPerDay, and
// SMSLoginPerDeploymentPerDay are the three daily caps, all in-memory for
// the same reason as the cooldown. Per-phone and per-deployment mirror
// RecoveryPerAccountPerDay's reasoning: this spends a shared sending quota
// and the sender's reputation. Per-phone deliberately aggregates *across
// tenants* -- see SMSLoginService.RequestCode.
const (
	SMSLoginPerPhonePerDay      = 5
	SMSLoginPerIPPerDay         = 20
	SMSLoginPerDeploymentPerDay = 1000
)

// smsLoginBudgetKey is the constant key SMSLoginService's deployment-wide
// daily limiter is checked against -- there is exactly one such budget, so
// there is exactly one key.
const smsLoginBudgetKey = "deployment"

// SMSLoginService issues and verifies the codes behind the standalone
// phone-number + SMS-code login method (spec §2).
type SMSLoginService struct {
	store    *store.Store
	users    *UserService
	settings *SettingsService
	audit    *AuditService
	metrics  *metrics.Registry
	sms      notify.SMSSender

	cooldown           *httpx.RateLimiter
	perPhonePerDay      *httpx.DayLimiter
	perIPPerDay          *httpx.DayLimiter
	perDeploymentPerDay  *httpx.DayLimiter
}

// NewSMSLoginService wires up a code-issuing service. perDeploymentPerDay
// lets a deployment tune SMSLoginPerDeploymentPerDay via configuration;
// pass 0 to use the default.
func NewSMSLoginService(
	st *store.Store, users *UserService, settings *SettingsService,
	audit *AuditService, registry *metrics.Registry, sms notify.SMSSender,
	perDeploymentPerDay int,
) *SMSLoginService {
	if perDeploymentPerDay <= 0 {
		perDeploymentPerDay = SMSLoginPerDeploymentPerDay
	}
	return &SMSLoginService{
		store: st, users: users, settings: settings, audit: audit, metrics: registry, sms: sms,
		cooldown:            httpx.NewRateLimiter(1, 1), // one per minute, burst one == a 60s cooldown per key
		perPhonePerDay:      httpx.NewDayLimiter(SMSLoginPerPhonePerDay, 24*time.Hour),
		perIPPerDay:         httpx.NewDayLimiter(SMSLoginPerIPPerDay, 24*time.Hour),
		perDeploymentPerDay: httpx.NewDayLimiter(perDeploymentPerDay, 24*time.Hour),
	}
}

// ErrSMSLoginUnavailable means this deployment or this tenant has not
// turned SMS login on.
var ErrSMSLoginUnavailable = httpx.NewError(503, "SMS_LOGIN_UNAVAILABLE",
	"SMS login is not available. Ask an administrator to enable it, or sign in with a password.")

// smsLoginDeliveryTimeout bounds the work that continues after the
// response, same reasoning as recoveryDeliveryTimeout.
const smsLoginDeliveryTimeout = 30 * time.Second

// RequestCode asks for a login code to be sent to phone.
//
// Reveals nothing about whether phone belongs to an account -- the lookup
// and the send both happen after this returns (spec §2.1). The two
// synchronous checks below (deployment/day, IP/day) are safe to refuse
// distinctly: neither depends on which phone was typed, so a 429 from them
// says nothing about whether an account exists. The phone-scoped checks
// (cooldown, phone/day) are NOT safe to refuse distinctly -- doing so would
// let a caller learn "this phone recently received a code" == "this phone
// has an account" -- so they are folded into the same silent path as the
// account lookup itself, inside completeRequest.
func (s *SMSLoginService) RequestCode(ctx context.Context, tenant model.Tenant, phone, ip string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return httpx.BadRequest("MISSING_PHONE", "A phone number is required.")
	}

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return err
	}
	if !settings.SMSLoginEnabled || !s.settings.CanDeliverSMS() {
		return ErrSMSLoginUnavailable
	}

	if !s.perDeploymentPerDay.Allow(smsLoginBudgetKey) {
		return httpx.TooManyRequests("TOO_MANY_ATTEMPTS",
			"This deployment has reached its daily SMS limit. Try again tomorrow.")
	}
	if !s.perIPPerDay.Allow(ip) {
		return httpx.TooManyRequests("TOO_MANY_ATTEMPTS",
			"Too many requests from this address today. Try again tomorrow.")
	}

	go s.completeRequest(context.WithoutCancel(ctx), tenant, phone, ip)
	return nil
}

// completeRequest does the part that must stay invisible to the caller:
// the phone-scoped rate limits, the account lookup, and the send.
func (s *SMSLoginService) completeRequest(ctx context.Context, tenant model.Tenant, phone, ip string) {
	ctx, cancel := context.WithTimeout(ctx, smsLoginDeliveryTimeout)
	defer cancel()

	// Phone-scoped limits are checked here, not in RequestCode, precisely
	// because a synchronous refusal would disclose that this phone has
	// received a code recently -- which discloses that it is bound to an
	// account. Silently declining to send is indistinguishable from a
	// phone number nobody has ever typed.
	if allowed, _ := s.cooldown.Allow(phone); !allowed {
		return
	}
	if !s.perPhonePerDay.Allow(phone) {
		return
	}

	q := s.store.ForTenant(tenant.ID)
	row, err := q.GetUserByPhone(ctx, phone)
	if err != nil {
		return // no such phone in this tenant; nothing to send
	}

	code, hash, err := newSMSLoginCode()
	if err != nil {
		return
	}
	now := store.Now()
	if err := q.CreateSMSLoginCode(ctx, sqlcgen.CreateSMSLoginCodeParams{
		ID: uuid.NewString(), Phone: phone, CodeHash: hash,
		ExpiresAt: now.Add(SMSLoginCodeTTL), CreatedAt: now, Ip: ip,
	}); err != nil {
		return
	}

	_ = s.sms.Send(ctx, phone, notify.SMSKindLoginCode, map[string]string{
		"Code":    code,
		"Minutes": strconv.Itoa(int(SMSLoginCodeTTL.Minutes())),
	})
}

// LoginWithCode verifies a code and, if it is right, signs the phone's
// account in.
//
// Wrong code, expired code, too-many-attempts, and no-such-phone all
// return the same ErrInvalidCredentials (spec §2.1) -- see
// UserService.Login's doc comment for why a sign-in path answers this way.
// Only once the code itself checks out does the account's own state
// (locked/disabled/closed) get its own answer, through
// UserService.issueSessionForUser.
func (s *SMSLoginService) LoginWithCode(ctx context.Context, tenant model.Tenant, phone, code, ip, userAgent string) (session Session, err error) {
	defer func() { s.metrics.RecordSignIn(signInOutcome(err)) }()

	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	if phone == "" || code == "" {
		return Session{}, httpx.BadRequest("MISSING_CREDENTIALS", "A phone number and code are required.")
	}

	q := s.store.ForTenant(tenant.ID)
	now := store.Now()

	row, err := q.GetLiveSMSLoginCode(ctx, phone, now)
	if err != nil {
		if store.IsNoRows(err) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("look up sms login code: %w", err)
	}

	if row.Attempts >= SMSLoginMaxAttempts || hashSMSLoginCode(code) != row.CodeHash {
		if row.Attempts < SMSLoginMaxAttempts {
			_ = q.IncrementSMSLoginCodeAttempts(ctx, row.ID)
		}
		return Session{}, ErrInvalidCredentials
	}

	user, err := q.GetUserByPhone(ctx, phone)
	if err != nil {
		if store.IsNoRows(err) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("look up user by phone: %w", err)
	}

	if err := q.ConsumeSMSLoginCode(ctx, row.ID, now); err != nil {
		return Session{}, fmt.Errorf("consume sms login code: %w", err)
	}

	return s.users.issueSessionForUser(ctx, tenant, user, ip, userAgent, "sms code")
}

// newSMSLoginCode generates a 6-digit code and its stored hash.
func newSMSLoginCode() (code, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", fmt.Errorf("generate sms login code: %w", err)
	}
	code = fmt.Sprintf("%06d", n.Int64())
	return code, hashSMSLoginCode(code), nil
}

// hashSMSLoginCode is a plain SHA-256, not a password hash -- see the
// CREATE TABLE comment on sms_login_codes.code_hash for why that is the
// right choice at this entropy.
func hashSMSLoginCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
```

Add `"github.com/Paraview-RD/portico/internal/store/sqlcgen"` to the import block (used by `sqlcgen.CreateSMSLoginCodeParams`).

Note the two rate-limiter field names in the struct literal have inconsistent alignment spacing in the snippet above (`perPhonePerDay`, `perIPPerDay`, `perDeploymentPerDay`) — run `gofmt -w internal/service/sms_login.go` after pasting this in; do not hand-align struct tags.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/service/... -run TestSMSLogin -v`
Expected: PASS on all five tests. If `TestSMSLoginCooldownIsPerPhoneAcrossTenants` fails because a second message was sent, check that `s.cooldown.Allow(phone)` is keyed on `phone` alone with no tenant prefix anywhere in `completeRequest` — this is the one place a stray `tenant.ID+phone` composite key would silently reintroduce the bug the spec's §3.2 exists to prevent.

- [ ] **Step 5: Injection check — confirm the "invalid code" tests actually catch a wrong implementation**

Temporarily change the comparison in `LoginWithCode` from `hashSMSLoginCode(code) != row.CodeHash` to always-false (`false`, i.e. accept any code), run `go test ./internal/service/... -run TestSMSLoginVerifyRefusesWithOneGenericErrorWhateverTheReason -v`, confirm it now FAILS, then revert the change and confirm it PASSES again. This proves the test is not a false green.

- [ ] **Step 6: Commit**

```bash
git add internal/service/sms_login.go internal/service/sms_login_test.go
git commit -m "$(cat <<'EOF'
service: add SMSLoginService for standalone phone+code sign-in

Anti-enumeration matches Login/RecoveryService: RequestCode always
answers success and does the phone-scoped work (lookup, cooldown,
per-phone daily cap) after responding, because a synchronous refusal
on those would leak whether a phone is bound to an account. Only the
IP-scoped and deployment-scoped caps are safe to refuse synchronously.
LoginWithCode delegates the post-credential-check tail to
UserService.issueSessionForUser, shared with external-identity
sign-in.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Config — Aliyun SMS environment variables

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/server/main.go` (the environment-variable reference block near `PORTICO_RESEND_API_KEY`)
- Modify: `.env.example`
- Modify: `docs/ops/environment-variables.md`

**Interfaces:**
- Produces: `Config.SMS notify.AliyunSMSConfig`, `Config.SMSLoginDeploymentDailyCap int`. Consumed by Task 11.

- [ ] **Step 1: Add the config fields and parsing**

In `internal/config/config.go`, add a field to `Config` next to `Mail notify.MailConfig`:

```go
	// SMS is the Aliyun transport config. Empty AccessKeyID means "not
	// configured" -- server.New falls back to notify.NotConfiguredSMS, same
	// as an unset PORTICO_SMTP_HOST falls back for mail.
	SMS notify.AliyunSMSConfig
	// SMSLoginDeploymentDailyCap overrides
	// service.SMSLoginPerDeploymentPerDay. Zero means use the default.
	SMSLoginDeploymentDailyCap int
```

Add the parsing block after the existing mail-transport block (after the `switch cfg.Mail.Transport` closes):

```go
	// SMS, unlike mail, has exactly one transport today -- there is no
	// PORTICO_SMS_TRANSPORT switch because there is nothing to switch
	// between yet. An empty PORTICO_ALIYUN_SMS_ACCESS_KEY_ID means this
	// deployment has not configured SMS at all, which is a valid, common
	// state (see notify.NotConfiguredSMS) rather than an error.
	if accessKeyID := os.Getenv("PORTICO_ALIYUN_SMS_ACCESS_KEY_ID"); accessKeyID != "" {
		cfg.SMS = notify.AliyunSMSConfig{
			AccessKeyID:     accessKeyID,
			AccessKeySecret: os.Getenv("PORTICO_ALIYUN_SMS_ACCESS_KEY_SECRET"),
			SignName:        os.Getenv("PORTICO_ALIYUN_SMS_SIGN_NAME"),
			TemplateCodes:   map[notify.SMSKind]string{},
		}
		if code := os.Getenv("PORTICO_ALIYUN_SMS_TEMPLATE_LOGIN_CODE"); code != "" {
			cfg.SMS.TemplateCodes[notify.SMSKindLoginCode] = code
		}
		if code := os.Getenv("PORTICO_ALIYUN_SMS_TEMPLATE_RECOVERY"); code != "" {
			cfg.SMS.TemplateCodes[notify.SMSKindRecovery] = code
		}
		if code := os.Getenv("PORTICO_ALIYUN_SMS_TEMPLATE_VERIFICATION"); code != "" {
			cfg.SMS.TemplateCodes[notify.SMSKindVerification] = code
		}
	}

	smsCap, err := envInt("PORTICO_SMS_LOGIN_DEPLOYMENT_DAILY_CAP", 0)
	if err != nil {
		return nil, err
	}
	cfg.SMSLoginDeploymentDailyCap = smsCap
```

- [ ] **Step 2: Build and run the config package tests**

Run: `go build ./... && go test ./internal/config/... -v`
Expected: PASS. If `envInt` does not accept a default of `0` the way it accepts `587` elsewhere, read its signature first (`grep -n "^func envInt" internal/config/config.go`) and match it exactly rather than guessing.

- [ ] **Step 3: Document the environment variables**

In `cmd/server/main.go`'s environment-variable reference block (the same block that documents `PORTICO_SMTP_HOST` etc.), add:

```
  PORTICO_ALIYUN_SMS_ACCESS_KEY_ID       Aliyun SMS. Unset means SMS login,
  PORTICO_ALIYUN_SMS_ACCESS_KEY_SECRET   password recovery over SMS, and
  PORTICO_ALIYUN_SMS_SIGN_NAME           registration verification over SMS
                                         are all unavailable
  PORTICO_ALIYUN_SMS_TEMPLATE_LOGIN_CODE      template code for each kind of
  PORTICO_ALIYUN_SMS_TEMPLATE_RECOVERY        message; each is optional --
  PORTICO_ALIYUN_SMS_TEMPLATE_VERIFICATION    a deployment may have Aliyun
                                              approval for only some
  PORTICO_SMS_LOGIN_DEPLOYMENT_DAILY_CAP  default 1000; the whole
                                          deployment's shared daily SMS
                                          login code budget
```

Match the existing block's column alignment exactly (read the surrounding lines first) rather than approximating it.

- [ ] **Step 4: Update `.env.example`**

Add, near the existing `PORTICO_SMTP_*`/`PORTICO_RESEND_*` block:

```
# Aliyun SMS (optional -- unset means SMS login, password recovery over
# SMS, and registration verification over SMS are all unavailable)
# PORTICO_ALIYUN_SMS_ACCESS_KEY_ID=
# PORTICO_ALIYUN_SMS_ACCESS_KEY_SECRET=
# PORTICO_ALIYUN_SMS_SIGN_NAME=
# PORTICO_ALIYUN_SMS_TEMPLATE_LOGIN_CODE=
# PORTICO_ALIYUN_SMS_TEMPLATE_RECOVERY=
# PORTICO_ALIYUN_SMS_TEMPLATE_VERIFICATION=
# PORTICO_SMS_LOGIN_DEPLOYMENT_DAILY_CAP=1000
```

- [ ] **Step 5: Update `docs/ops/environment-variables.md`**

Read the existing SMTP/Resend rows in that file first and add a matching row per variable, in the same table format, with the same purpose/default/required-when columns that file already uses.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go cmd/server/main.go .env.example docs/ops/environment-variables.md
git commit -m "$(cat <<'EOF'
config: wire Aliyun SMS environment variables

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: `server.New` wiring — default SMS sender, `SMSLoginService` construction

**Files:**
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `Config.SMS`, `Config.SMSLoginDeploymentDailyCap` (Task 10), `service.NewSMSLoginService` (Task 9).
- Produces: `Server` now constructs and holds an `*service.SMSLoginService`, passed to `handler.New` (Task 12).

- [ ] **Step 1: Build the default SMS sender from config**

In `internal/server/server.go`'s `New`, replace:

```go
	// V0.1 ships the SMS interface and no provider; see internal/notify.
	deps := dependencies{mailer: mailer, sms: notify.NotConfiguredSMS{}}
```

with:

```go
	defaultSMS, err := smsSenderFromConfig(cfg.SMS)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	deps := dependencies{mailer: mailer, sms: defaultSMS}
```

Add the helper near `New`:

```go
// smsSenderFromConfig builds the SMS sender this deployment asked for.
// An empty AccessKeyID means "not configured," same as an unset
// PORTICO_SMTP_HOST does for mail -- see notify.NotConfiguredSMS.
func smsSenderFromConfig(cfg notify.AliyunSMSConfig) (notify.SMSSender, error) {
	if cfg.AccessKeyID == "" {
		return notify.NotConfiguredSMS{}, nil
	}
	return notify.NewAliyunSMSSender(cfg)
}
```

- [ ] **Step 2: Construct `SMSLoginService`**

After the existing `verification := service.NewVerificationService(...)` line, add:

```go
	smsLogin := service.NewSMSLoginService(
		st, users, settings, audit, registry, deps.sms, cfg.SMSLoginDeploymentDailyCap)
```

- [ ] **Step 3: Pass it to `handler.New`**

Change the `handler.New(...)` call (Task 12 will have already added the new parameter to `handler.New`'s signature — do this step together with Task 12's Step 1, or come back to it once that signature exists):

```go
		handler: handler.New(users, orgs, audit, settings, tenants, recovery, verification, sessions,
			clients, serviceProviders, samlKeys, casServices, scimCredentials,
			directories, webhooks, externalIDP, groups, invitations, logos, attributes, fields, fieldMappings,
			providers, samlProviders, casServer, trials, smsLogin),
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: fails until Task 12's `handler.New` signature change lands — that is expected; do Tasks 11 and 12 in the same working session before committing either, or commit them together as one task if your workflow does not allow a deliberately-broken intermediate commit.

- [ ] **Step 5: Commit** (after Task 12's handler changes are also in place)

```bash
git add internal/server/server.go
git commit -m "$(cat <<'EOF'
server: wire the Aliyun SMS sender and SMSLoginService

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: HTTP handlers, routes, route guard, registration-status flag

**Files:**
- Modify: `internal/handler/handler.go` (add `smsLogin` field + constructor param)
- Create: `internal/handler/sms_login.go`
- Modify: `internal/handler/auth.go` (`RegistrationStatus` gains `smsLoginEnabled`)
- Modify: `internal/server/routes.go`
- Modify: `internal/server/route_guard_test.go` (`publicAPIRoutes`)
- Modify: `internal/server/server.go` (see Task 11 Step 3, done together with this task)

**Interfaces:**
- Consumes: `service.SMSLoginService.RequestCode`/`LoginWithCode` (Task 9).
- Produces: `POST /api/v1/auth/sms/code`, `POST /api/v1/auth/sms/login`; `GET /api/v1/auth/registration-status` response gains `"smsLoginEnabled"`.

- [ ] **Step 1: Add `smsLogin` to `Handler`**

In `internal/handler/handler.go`, add a field:

```go
	// The standalone phone-number + SMS-code login method.
	smsLogin *service.SMSLoginService
```

Add a parameter at the end of `New`'s signature and the struct literal:

```go
func New(
	users *service.UserService,
	// ... every existing parameter, unchanged ...
	trials *service.TrialService,
	smsLogin *service.SMSLoginService,
) *Handler {
	return &Handler{
		// ... every existing field, unchanged ...
		trials:   trials,
		smsLogin: smsLogin,
	}
}
```

- [ ] **Step 2: Write the handlers**

```go
package handler

import (
	"net/http"

	"github.com/Paraview-RD/portico/internal/httpx"
)

type smsLoginCodeRequest struct {
	Tenant string `json:"tenant"`
	Phone  string `json:"phone"`
}

// RequestSMSLoginCode asks for a login code to be sent to a phone number.
//
// Always answers 200 whether or not the number is bound to an account --
// see SMSLoginService.RequestCode's doc comment for why.
func (h *Handler) RequestSMSLoginCode(w http.ResponseWriter, r *http.Request) {
	var req smsLoginCodeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.smsLogin.RequestCode(r.Context(), tenant, req.Phone, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"sent": true})
}

type smsLoginRequest struct {
	Tenant string `json:"tenant"`
	Phone  string `json:"phone"`
	Code   string `json:"code"`
}

// LoginWithSMSCode authenticates a user by phone number and SMS code and
// returns a token.
func (h *Handler) LoginWithSMSCode(w http.ResponseWriter, r *http.Request) {
	var req smsLoginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	session, err := h.smsLogin.LoginWithCode(r.Context(), tenant, req.Phone, req.Code,
		httpx.ClientIP(r), userAgent(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, session)
}
```

Check `userAgent(r)`'s exact package/location first (`grep -rn "^func userAgent" internal/handler`) — it is already used by `Login` in `auth.go`, so it exists in this package; just confirm no import is missing.

- [ ] **Step 3: Add the `smsLoginEnabled` flag to `RegistrationStatus`**

In `internal/handler/auth.go`'s `RegistrationStatus`, add one line to the `httpx.OK(w, map[string]any{...})` call:

```go
	httpx.OK(w, map[string]any{
		"registrationEnabled": settings.RegistrationEnabled,
		"invitationOnly":      settings.InvitationOnlyRegistration,
		"systemName":          settings.SystemName,
		"tenant":              tenant.Code,
		"tenantName":          tenant.Name,
		"branding":            brandingOf(settings),
		// The sign-in screen needs to know whether to offer the SMS-code
		// tab, the same as it already needs registrationEnabled for its
		// own tab. Both conditions matter: the tenant switched it on AND
		// this deployment can actually send SMS.
		"smsLoginEnabled": settings.SMSLoginEnabled && h.settings.CanDeliverSMS(),
	})
```

- [ ] **Step 4: Register the routes**

In `internal/server/routes.go`, next to the existing recovery routes:

```go
	r.Get("/auth/recovery-channels", h.RecoveryChannels)
	r.Post("/auth/password-recovery", h.RequestPasswordRecovery)
	r.Post("/auth/password-recovery/confirm", h.ConfirmPasswordRecovery)

	// Standalone phone-number + SMS-code login (§2 of
	// docs/superpowers/specs/2026-09-21-sms-otp-login-design.md). Both
	// public for the same reason password recovery is: the caller cannot
	// sign in, which is the entire point.
	r.Post("/auth/sms/code", h.RequestSMSLoginCode)
	r.Post("/auth/sms/login", h.LoginWithSMSCode)
```

- [ ] **Step 5: Add the two routes to the route guard's allowlist**

In `internal/server/route_guard_test.go`, add to `publicAPIRoutes`:

```go
	"POST /api/v1/auth/sms/code":  "asks for a code before anybody has signed in",
	"POST /api/v1/auth/sms/login": "the endpoint that issues the credential for this method, same as /auth/login",
```

- [ ] **Step 6: Build and run the route guard and seed tests**

Run: `go build ./... && go test ./internal/server/... -run "TestRouteGuard|TestOnlyThe|TestSeed" -v`
Expected: PASS. If a route-guard test calls every listed route and asserts a non-404, `RequestSMSLoginCode`/`LoginWithSMSCode` must return something other than 404 for a malformed body (they will, via `httpx.DecodeJSON`'s error path) — this should pass without further changes.

- [ ] **Step 7: Commit** (together with Task 11's `server.go` changes, per Task 11 Step 5)

```bash
git add internal/handler/handler.go internal/handler/sms_login.go internal/handler/auth.go internal/server/routes.go internal/server/route_guard_test.go internal/server/server.go
git commit -m "$(cat <<'EOF'
handler,server: expose SMS login over HTTP

POST /auth/sms/code and /auth/sms/login, both public for the same
reason password recovery is. registration-status gains
smsLoginEnabled so the sign-in screen knows whether to offer the tab.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: OpenAPI spec + lint

**Files:**
- Modify: `openapi.yaml`

**Interfaces:**
- Consumes: the two new endpoints and the `registrationStatus` response shape from Task 12.

- [ ] **Step 1: Locate the existing recovery paths as a template**

Run: `grep -n "password-recovery:" openapi.yaml`

- [ ] **Step 2: Add the two new paths**

Read the full `password-recovery` and `password-recovery/confirm` path definitions first (request body schema, response schema, error responses, tags) and add `/auth/sms/code` and `/auth/sms/login` in the same style, in the same file section, with:

- `/auth/sms/code`: request body `{tenant?: string, phone: string}`, response `200 {sent: boolean}` — mirror `password-recovery`'s "always 200" documentation note if that path has one.
- `/auth/sms/login`: request body `{tenant?: string, phone: string, code: string}`, response `200` with the same `Session` schema `/auth/login` already documents (reuse that schema reference, do not redefine it), and a `401 INVALID_CODE` error response.

- [ ] **Step 3: Add `smsLoginEnabled` to the `registrationStatus` response schema**

Find the schema `/auth/registration-status`'s `200` response uses and add `smsLoginEnabled: boolean` alongside `registrationEnabled`.

- [ ] **Step 4: Lint**

Run: `npx @redocly/cli lint openapi.yaml` (or whatever exact command `hack/lint-ci.sh` / the repo's own scripts use — check first with `grep -rn redocly hack/ package.json 2>/dev/null`)
Expected: no errors. Fix any schema-reference or required-field issues it reports before moving on.

- [ ] **Step 5: Commit**

```bash
git add openapi.yaml
git commit -m "$(cat <<'EOF'
docs: add SMS login endpoints to openapi.yaml

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Server-level integration tests

**Files:**
- Create: `internal/server/sms_login_test.go`

**Interfaces:**
- Consumes: everything from Tasks 9–12, exercised end-to-end through `apiTest`/`server.New`/`server.WithSMSSender`.

- [ ] **Step 1: Write the tests**

```go
package server_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// recordingSMS captures every Send call, the SMS analogue of recordingMailer
// in recovery_test.go.
type recordingSMS struct {
	mu   sync.Mutex
	sent []sentSMS
}

type sentSMS struct {
	phone  string
	kind   notify.SMSKind
	params map[string]string
}

func (r *recordingSMS) Send(_ context.Context, phone string, kind notify.SMSKind, params map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentSMS{phone, kind, params})
	return nil
}

func (r *recordingSMS) waitFor(t *testing.T, count int) sentSMS {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.sent)
		var got sentSMS
		if n >= count {
			got = r.sent[count-1]
		}
		r.mu.Unlock()
		if n >= count {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for SMS #%d; got %d", count, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// newSMSLoginTest builds a server with SMS login enabled for the default
// tenant and a real sender substituted for a recorder.
func newSMSLoginTest(t *testing.T) (*apiTest, *recordingSMS) {
	t.Helper()
	silenceLogs(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = testdb.DSN(t)
	cfg.InitialAdminUsername = adminUsername
	cfg.InitialAdminPassword = adminPassword
	cfg.PublicURL = "https://portico.example.com"

	sms := &recordingSMS{}
	srv, err := server.New(cfg, server.WithSMSSender(sms))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}

	// Turn the tenant setting on -- read this test file's existing helper
	// for updating settings as an administrator (grep -n "settings" in the
	// other _test.go files in this package) and mirror it, then also bind
	// a phone number to the seeded admin account the same way, since the
	// bootstrap admin has no phone by default.
	return api, sms
}

func TestSMSLoginEndToEnd(t *testing.T) {
	api, sms := newSMSLoginTest(t)
	phone := "+8613800138000" // whatever phone this test's setup bound to the admin account

	res := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": phone})
	if res.Status != http.StatusOK {
		t.Fatalf("request code: %d %s", res.Status, res.Message)
	}

	sent := sms.waitFor(t, 1)
	code := sent.params["Code"]
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	login := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": phone, "code": code,
	})
	if login.Status != http.StatusOK {
		t.Fatalf("login: %d %s", login.Status, login.Message)
	}
	var session struct {
		Token string `json:"token"`
	}
	login.into(t, &session)
	if session.Token == "" {
		t.Error("want a session token")
	}
}

func TestSMSLoginRequestCodeDoesNotRevealWhetherThePhoneExists(t *testing.T) {
	api, sms := newSMSLoginTest(t)

	known := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": "+8613800138000"})
	unknown := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": "+8619999999999"})

	if known.Status != unknown.Status || known.Status != http.StatusOK {
		t.Errorf("known = %d, unknown = %d, want both 200", known.Status, unknown.Status)
	}

	sms.waitFor(t, 1) // the known phone's message
	time.Sleep(100 * time.Millisecond)
	sms.mu.Lock()
	n := len(sms.sent)
	sms.mu.Unlock()
	if n != 1 {
		t.Errorf("sent %d messages, want exactly 1 (only the phone with an account)", n)
	}
}

func TestSMSLoginRefusesWhenTheTenantHasNotEnabledIt(t *testing.T) {
	// Build a server the same way newAPITest does (SMS sender configured
	// at the process level, but the tenant setting left at its default of
	// off) and assert /auth/sms/code returns 503 SMS_LOGIN_UNAVAILABLE.
}

func TestSMSLoginRefusesWhenNoSMSSenderIsConfigured(t *testing.T) {
	// Build a server via newAPITest (no SMS sender substituted, so it is
	// notify.NotConfiguredSMS) and assert the same 503, even if somehow the
	// tenant setting were on.
}
```

Fill in the two placeholder tests using whichever existing helper this test package already has for signing in as the seeded administrator and calling the settings-update endpoint (grep `internal/server/*_test.go` for `"/api/v1/settings"` to find it) — do not leave them as empty bodies; the "No Placeholders" rule applies here too, this plan is only leaving them last because their exact shape depends on that helper's existing signature, which must be read, not guessed.

- [ ] **Step 2: Run to verify they fail before the feature exists**

This task runs after Tasks 9–12 are already committed, so instead of "verify it fails," run the whole file once against the finished implementation directly:

Run: `go test ./internal/server/... -run TestSMSLogin -v`
Expected: PASS on all tests once the fill-ins from Step 1 are complete. If something fails, it is either a real bug in Tasks 9–12 or a wrong assumption in this test's setup (e.g., about how to bind a phone to the seeded admin) — check the seed/bootstrap code before assuming the test is wrong.

- [ ] **Step 3: Commit**

```bash
git add internal/server/sms_login_test.go
git commit -m "$(cat <<'EOF'
server: add end-to-end tests for SMS login

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Frontend — API types, endpoints, `LoginPage.tsx`

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/endpoints.ts`
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/i18n/en-US.ts`, `web/src/i18n/zh-CN.ts`

**Interfaces:**
- Consumes: `POST /auth/sms/code`, `POST /auth/sms/login`, `registrationStatus.smsLoginEnabled` (Task 12/13).

- [ ] **Step 1: Add the API types**

In `web/src/api/types.ts`, find the `RegistrationStatus`-shaped type (search for `registrationEnabled`) and add:

```ts
  smsLoginEnabled: boolean;
```

- [ ] **Step 2: Add the endpoint functions**

In `web/src/api/endpoints.ts`, next to `authApi.registrationStatus`/`requestPasswordRecovery`, read their exact request/response wiring first (which HTTP client wrapper they call, how errors surface) and add two functions in the same style:

```ts
  requestSMSLoginCode(phone: string, tenant: string) {
    return postJSON<{ sent: boolean }>("/auth/sms/code", { phone, tenant });
  },
  loginWithSMSCode(phone: string, code: string, tenant: string) {
    return postJSON<Session>("/auth/sms/login", { phone, code, tenant });
  },
```

Replace `postJSON`/`Session` with whatever this file's existing helpers and types are actually named — read `authApi.requestPasswordRecovery`'s real implementation before writing this, do not invent a helper name.

- [ ] **Step 3: Add the login-page UI**

In `web/src/pages/LoginPage.tsx`:

1. Add state: `const [mode, setMode] = useState<"password" | "sms">("password")`, `const [smsPhone, setSmsPhone] = useState("")`, `const [smsCode, setSmsCode] = useState("")`, `const [smsCooldown, setSmsCooldown] = useState(0)`, `const [smsSending, setSmsSending] = useState(false)`.
2. Fetch `smsLoginEnabled` from the existing `registrationStatus` call (it already sets `registrationOpen`/`systemName`/`branding` from the same response — add one more `setSmsLoginEnabled(status.smsLoginEnabled)` alongside those).
3. Add a countdown effect:

```tsx
  useEffect(() => {
    if (smsCooldown <= 0) return;
    const id = setInterval(() => setSmsCooldown((s) => Math.max(0, s - 1)), 1000);
    return () => clearInterval(id);
  }, [smsCooldown]);
```

4. Add a toggle link near the top of the form (next to the tenant field, only rendered when `smsLoginEnabled && !completingAuthorization` — external-option buttons are already hidden during `completingAuthorization` for a documented reason in this file; SMS login should follow the same rule since it is also a full-page-replacing sign-in path):

```tsx
  {smsLoginEnabled && !completingAuthorization && (
    <div className="text-center">
      <AuthLink onClick={() => setMode(mode === "password" ? "sms" : "password")}>
        {mode === "password" ? t("login.useSmsCode") : t("login.usePassword")}
      </AuthLink>
    </div>
  )}
```

5. Render the SMS form instead of the password fields when `mode === "sms"`:

```tsx
  {mode === "sms" ? (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        setError("");
        setSubmitting(true);
        try {
          setLookedUpTenant(tenant.trim());
          const session = await authApi.loginWithSMSCode(smsPhone.trim(), smsCode.trim(), tenant.trim());
          // Whatever the password branch's handleSubmit does with a
          // successful session (it currently calls signIn(...), which
          // both calls the API and updates session state) -- read
          // useSession's exact contract first: this form calls the API
          // itself, so it likely needs a "session already obtained,
          // just store it" entry point rather than signIn's own fetch.
          // Add that entry point to useSession if one does not exist,
          // rather than calling the API twice.
          if (!completingAuthorization) navigate("/");
        } catch (err) {
          setError(describeError(err));
        } finally {
          setSubmitting(false);
        }
      }}
      className="flex flex-col gap-4"
    >
      <Field label={t("login.phone")} required>
        <Input
          value={smsPhone}
          onChange={(e) => setSmsPhone(e.target.value)}
          type="tel"
          autoComplete="tel"
          autoFocus
          required
        />
      </Field>
      <Field label={t("login.smsCode")} required>
        <div className="flex gap-2">
          <Input
            value={smsCode}
            onChange={(e) => setSmsCode(e.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
            required
          />
          <Button
            type="button"
            variant="secondary"
            disabled={smsCooldown > 0 || smsSending || smsPhone.trim() === ""}
            onClick={async () => {
              setSmsSending(true);
              setError("");
              try {
                await authApi.requestSMSLoginCode(smsPhone.trim(), tenant.trim());
                setSmsCooldown(60);
              } catch (err) {
                setError(describeError(err));
              } finally {
                setSmsSending(false);
              }
            }}
          >
            {smsCooldown > 0 ? t("login.smsResendIn", smsCooldown) : t("login.smsSend")}
          </Button>
        </div>
      </Field>
      {error && <Alert tone="danger">{error}</Alert>}
      <Button type="submit" disabled={submitting}>
        {submitting ? t("login.signingIn") : t("login.submit")}
      </Button>
    </form>
  ) : (
    // ... existing password <form> unchanged ...
  )}
```

Read `useSession`'s current implementation before this step (`grep -n "function useSession" -A 30 web/src/session.tsx` or wherever it lives) to find or add the "store an already-obtained session" entry point mentioned in the comment above — do not leave that as a TODO in the actual code; resolve it as part of this step, since without it the SMS branch cannot update the app's signed-in state after a successful login.

- [ ] **Step 4: Add the i18n strings**

In both `web/src/i18n/en-US.ts` and `web/src/i18n/zh-CN.ts`, add next to the existing `login.*` keys:

```ts
  "login.useSmsCode": "Sign in with a code instead", // en
  "login.usePassword": "Sign in with a password instead",
  "login.phone": "Phone number",
  "login.smsCode": "Code",
  "login.smsSend": "Send code",
  "login.smsResendIn": "Resend in {0}s", // adjust to this file's actual interpolation syntax -- check an existing parameterized key like login.forgotPassword or verify.resent first
```

```ts
  "login.useSmsCode": "改用验证码登录", // zh-CN
  "login.usePassword": "改用密码登录",
  "login.phone": "手机号",
  "login.smsCode": "验证码",
  "login.smsSend": "发送验证码",
  "login.smsResendIn": "{0} 秒后重新发送",
```

- [ ] **Step 5: Type-check and run the frontend's fast tests**

Run: `cd web && npm run typecheck && npm run test` (or whatever this repo's actual scripts are named — check `web/package.json` first; the four-command frontend verification this project's own conventions call for is `format:check`, `lint`, `typecheck`, and `test` — run all four, do not stop after typecheck).
Expected: PASS on all four.

- [ ] **Step 6: Manually verify in a browser**

Start the dev server per this repo's own run instructions (check `hack/dev.sh` or the README's "Running it" section), open the login page, toggle to SMS mode, and confirm the layout does not break with `smsLoginEnabled: false` from the API (the toggle link must not render) and, separately, with a stubbed `true` response, confirm the cooldown button visibly counts down.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/types.ts web/src/api/endpoints.ts web/src/pages/LoginPage.tsx web/src/i18n/en-US.ts web/src/i18n/zh-CN.ts web/src/session.tsx
git commit -m "$(cat <<'EOF'
web: add SMS code login to the sign-in screen

A toggle next to the password form, shown only when the tenant has
turned it on and this deployment can send SMS. 60-second resend
cooldown, matching the backend's cooldown window.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Frontend — admin settings toggle

**Files:**
- Modify: `web/src/api/types.ts` (the `Settings` type, distinct from the `RegistrationStatus`-shaped type touched in Task 15)
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/i18n/en-US.ts`, `web/src/i18n/zh-CN.ts`

**Interfaces:**
- Consumes: `Settings.smsLoginEnabled` (Task 7's backend field, same JSON key).

- [ ] **Step 1: Add the field to the `Settings` type**

In `web/src/api/types.ts`, find the `Settings` interface (it already has `registrationVerification: boolean;`) and add:

```ts
  smsLoginEnabled: boolean;
```

- [ ] **Step 2: Add the toggle to `SettingsPage.tsx`**

Near the existing `registrationEnabled`/`registrationVerification` checkboxes (around line 295–341 as read in this plan's research), add a sibling checkbox — not nested under registration, since SMS login is independent of it:

```tsx
                <label className="flex items-start gap-2.5">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={settings.smsLoginEnabled}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        smsLoginEnabled: e.target.checked,
                      })
                    }
                  />
                  <span>
                    <span className="block font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                      {t("settings.smsLoginEnabled")}
                    </span>
                    <span className="block text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {t("settings.smsLoginEnabledHelp")}
                    </span>
                  </span>
                </label>
```

- [ ] **Step 3: Add the i18n strings**

```ts
  "settings.smsLoginEnabled": "Allow signing in with an SMS code", // en
  "settings.smsLoginEnabledHelp": "Lets anyone with a bound phone number sign in with a text-message code instead of a password. Requires this deployment to have SMS configured.",
```

```ts
  "settings.smsLoginEnabled": "允许使用短信验证码登录", // zh-CN
  "settings.smsLoginEnabledHelp": "允许绑定了手机号的账号用短信验证码代替密码登录。需要本部署配置了短信发送能力。",
```

- [ ] **Step 4: Type-check, lint, test**

Run: `cd web && npm run format:check && npm run lint && npm run typecheck && npm run test`
Expected: PASS on all four.

- [ ] **Step 5: Manually verify**

Sign in to the admin console's settings page, confirm the new checkbox renders, toggle it on with no SMS configured on the backend, and confirm the save attempt surfaces the `NO_SMS_CHANNEL` error from Task 7 legibly rather than a raw error code.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/pages/SettingsPage.tsx web/src/i18n/en-US.ts web/src/i18n/zh-CN.ts
git commit -m "$(cat <<'EOF'
web: add the SMS login toggle to the settings page

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: Playwright e2e coverage for the login-page copy change

**Files:**
- Modify or create: whatever this repo's existing login-page Playwright spec file is (locate first with `grep -rln "login.title\|data-testid=\"login" web/e2e` or the project's actual e2e directory — check the README/CLAUDE.md for the exact path and run command first)

**Interfaces:**
- Consumes: the rendered `LoginPage.tsx` from Task 15.

- [ ] **Step 1: Find the existing login e2e spec and its literal-copy assertions**

Run: `grep -rln "Sign in\|登录" web/e2e 2>/dev/null` (adjust the path once found) to locate hard-coded copy assertions this change could break, per this project's own convention that a copy change requires running the full e2e suite (not just the four-command frontend check).

- [ ] **Step 2: Add a test for the new toggle**

In that spec file, following its existing style (page-object helpers, selectors, etc. — read at least one existing test in the file before writing this one), add a test that:
1. Seeds or configures a tenant with `smsLoginEnabled: true` and a bound phone (reuse whatever fixture/seed helper the suite's other tests already use for tenant setup).
2. Navigates to the login page, clicks the "sign in with a code instead" link, and asserts the password field disappears and the phone/code fields appear.
3. Clicks "send code," asserts the button becomes disabled and shows a counting-down label.

- [ ] **Step 3: Run the full local e2e suite**

Run whatever this repo's own documented command is for a full local Playwright run (per this project's convention that a login-page copy change requires the whole suite, not a subset) — find it in the README or `package.json` scripts, e.g. `cd web && npm run test:e2e`.
Expected: PASS, including every pre-existing login-page test — a literal string assertion elsewhere in the suite (e.g. asserting the password form is the only thing on the page) is exactly what this step exists to catch.

- [ ] **Step 4: Commit**

```bash
git add web/e2e/  # or the actual path found in Step 1
git commit -m "$(cat <<'EOF'
e2e: cover the SMS login toggle on the sign-in screen

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: `docs/integrations.md`

**Files:**
- Modify: `docs/integrations.md`

**Interfaces:**
- None — documentation only, but required in the same body of work as the feature per this project's own third-party-service rule.

- [ ] **Step 1: Read the existing entry for one third-party service already documented there (e.g. Resend) to match its exact section structure.**

- [ ] **Step 2: Add an Aliyun SMS entry**

Using that structure, document: purpose (SMS delivery for login codes, password recovery, and registration verification), auth method (AccessKey ID/Secret, HMAC-SHA1 request signing), account owner (leave as a placeholder the user fills in — this is the one field this plan cannot know), costs (per-message, billed by Aliyun; note the deployment-wide daily cap in `PORTICO_SMS_LOGIN_DEPLOYMENT_DAILY_CAP` as the cost control), and the environment variables from Task 10.

- [ ] **Step 3: Commit**

```bash
git add docs/integrations.md
git commit -m "$(cat <<'EOF'
docs: add Aliyun SMS to the integrations reference

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Final full-suite verification

Run once, after every task above is committed, before calling the feature done:

- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `golangci-lint run` (per this project's own convention: run it locally, do not skip or truncate its output)
- [ ] `go test ./... -v 2>&1 | tee /tmp/portico-test-full.log` and then read the **complete** summary line at the end of the log — per this project's own convention, do not conclude "all green" from a `tail` or a `grep`, and do not conclude it from a truncated terminal scrollback
- [ ] `npx @redocly/cli lint openapi.yaml` (or the project's own wrapper script)
- [ ] `cd web && npm run format:check && npm run lint && npm run typecheck && npm run test`
- [ ] `cd web && npm run test:e2e` (or this project's actual e2e command)
- [ ] Start the dev server and manually walk through: request a code for a bound phone with a fake/sandboxed Aliyun sender (or `WithSMSSender` swapped to a logger for manual testing), verify it, land signed in; then try an unbound phone and confirm the response looks identical up to the point of typing a code.
