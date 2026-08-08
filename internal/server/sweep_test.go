package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// The cleanup that runs on a timer, and the one property it must not break.
//
// Deleting expired rows is the obvious implementation and it is wrong here.
// TokenRequestByRefreshToken checks used_at BEFORE it checks expiry, on
// purpose: presenting a spent token means a copy leaked, and the answer is to
// revoke everything descended from it. A token being past its expiry does not
// make that less true — a stolen token turning up late is exactly when you
// want to know. So a row may only go once nothing downstream of it can be
// used, which is what these tests hold in place.

// insertRefreshToken writes a row directly. Driving the real rotation would
// need a live sign-in per link and could not produce the expiry combinations
// this is about.
func (f *federationTest) insertRefreshToken(t *testing.T, id, tenantID, subject string, expiresAt time.Time, replacedBy *string, used bool) {
	t.Helper()

	var usedAt *time.Time
	if used {
		when := time.Now().Add(-time.Hour)
		usedAt = &when
	}

	_, err := f.db.Exec(`
		INSERT INTO oauth_refresh_tokens
			(id, tenant_id, client_id, subject, token_hash, auth_time,
			 replaced_by, used_at, created_at, expires_at)
		VALUES ($1, $2, 'sweep-client', $3, $4, now(), $5, $6, now(), $7)`,
		id, tenantID, subject, "hash-"+id, replacedBy, usedAt, expiresAt)
	if err != nil {
		t.Fatalf("insert refresh token %s: %v", id, err)
	}
}

func (f *federationTest) refreshTokenExists(t *testing.T, id string) bool {
	t.Helper()

	var count int
	if err := f.db.QueryRow(
		"SELECT count(*) FROM oauth_refresh_tokens WHERE id = $1", id).Scan(&count); err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	return count > 0
}

// tenantAndUser returns ids the foreign keys will accept.
func (f *federationTest) tenantAndUser(t *testing.T) (tenantID, userID string) {
	t.Helper()

	if err := f.db.QueryRow(
		"SELECT id FROM tenants WHERE code = 'default'").Scan(&tenantID); err != nil {
		t.Fatalf("read default tenant: %v", err)
	}
	if err := f.db.QueryRow(
		"SELECT id FROM users WHERE tenant_id = $1 LIMIT 1", tenantID).Scan(&userID); err != nil {
		t.Fatalf("read a user: %v", err)
	}
	return tenantID, userID
}

// A chain whose last token is still live must survive entirely, however old
// its ancestors are. Deleting the ancestor would turn a stolen token from
// "reuse detected, revoke the chain" into "unknown token", leaving the
// replacements the thief's own refresh produced still working.
func TestSweepKeepsAncestorsOfLiveRefreshTokens(t *testing.T) {
	f := newFederationTest(t)
	tenantID, userID := f.tenantAndUser(t)

	live := "chain-live"
	ancient := "chain-ancient"

	// The replacement is live; the token it replaced expired a year ago.
	f.insertRefreshToken(t, live, tenantID, userID,
		time.Now().Add(24*time.Hour), nil, false)
	replacedBy := live
	f.insertRefreshToken(t, ancient, tenantID, userID,
		time.Now().Add(-365*24*time.Hour), &replacedBy, true)

	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if !f.refreshTokenExists(t, ancient) {
		t.Error("the sweep deleted a spent ancestor whose replacement is still " +
			"live; presenting the stolen ancestor would now read as an unknown " +
			"token and its descendants would survive")
	}
	if !f.refreshTokenExists(t, live) {
		t.Error("the sweep deleted a live refresh token")
	}
}

// Once the whole chain is long dead there is nothing left to protect, and
// the rows are pure weight.
func TestSweepDeletesWhollyDeadRefreshChains(t *testing.T) {
	f := newFederationTest(t)
	tenantID, userID := f.tenantAndUser(t)

	terminal := "dead-terminal"
	middle := "dead-middle"
	oldest := "dead-oldest"

	// Expiries increase along a chain, which is what makes the terminal
	// token the one worth testing. All three are far past retention.
	long := 400 * 24 * time.Hour
	f.insertRefreshToken(t, terminal, tenantID, userID,
		time.Now().Add(-long), nil, false)
	toTerminal := terminal
	f.insertRefreshToken(t, middle, tenantID, userID,
		time.Now().Add(-long-time.Hour), &toTerminal, true)
	toMiddle := middle
	f.insertRefreshToken(t, oldest, tenantID, userID,
		time.Now().Add(-long-2*time.Hour), &toMiddle, true)

	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	for _, id := range []string{terminal, middle, oldest} {
		if f.refreshTokenExists(t, id) {
			t.Errorf("%s survived the sweep, but its whole chain is long dead", id)
		}
	}
}

// A chain that is dead but not yet past the retention window stays. The
// window is there so that "when did this application last refresh" has an
// answer for a while after it stopped.
func TestSweepRespectsRefreshTokenRetention(t *testing.T) {
	f := newFederationTest(t)
	tenantID, userID := f.tenantAndUser(t)

	recent := "recently-expired"
	f.insertRefreshToken(t, recent, tenantID, userID,
		time.Now().Add(-time.Hour), nil, false)

	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !f.refreshTokenExists(t, recent) {
		t.Error("a token that expired an hour ago was deleted; the retention " +
			"window is what keeps recent history answerable")
	}
}

func TestSweepClearsOldPasswordResets(t *testing.T) {
	f := newFederationTest(t)
	tenantID, userID := f.tenantAndUser(t)

	insert := func(id string, expiresAt time.Time) {
		t.Helper()
		_, err := f.db.Exec(`
			INSERT INTO password_resets
				(id, tenant_id, user_id, token_hash, channel, expires_at, created_at)
			VALUES ($1, $2, $3, $4, 'EMAIL', $5, now())`,
			id, tenantID, userID, "hash-"+id, expiresAt)
		if err != nil {
			t.Fatalf("insert password reset %s: %v", id, err)
		}
	}
	exists := func(id string) bool {
		t.Helper()
		var count int
		if err := f.db.QueryRow(
			"SELECT count(*) FROM password_resets WHERE id = $1", id).Scan(&count); err != nil {
			t.Fatalf("count password resets: %v", err)
		}
		return count > 0
	}

	old := "reset-ancient"
	recent := "reset-recent"
	insert(old, time.Now().Add(-400*24*time.Hour))
	insert(recent, time.Now().Add(-time.Hour))

	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if exists(old) {
		t.Error("a reset request from over a year ago survived the sweep")
	}
	if !exists(recent) {
		t.Error("a reset request that expired an hour ago was deleted; the " +
			"retention window is what keeps recent history answerable")
	}
}

// Audit retention is the one setting that deletes a compliance record, so
// the property that matters most is that it does nothing unless asked.

func (a *apiTest) auditEntryCount(t *testing.T, token string) int {
	t.Helper()

	res := a.do(http.MethodGet, "/api/v1/audit-logs?pageSize=100", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list audit logs: %d %s", res.Status, res.Code)
	}
	var page struct {
		Total int `json:"total"`
	}
	res.into(t, &page)
	return page.Total
}

func TestAuditLogIsKeptForeverByDefault(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Age everything well past any plausible retention.
	api.execSQL(t, "UPDATE audit_logs SET created_at = now() - interval '5 years'")
	before := api.auditEntryCount(t, token)
	if before == 0 {
		t.Fatal("no audit entries to test with")
	}

	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if after := api.auditEntryCount(t, token); after != before {
		t.Errorf("the sweep deleted %d audit entries with no retention configured. "+
			"A product that quietly starts shortening its own audit trail is doing "+
			"the worst thing an audit log can do.", before-after)
	}
}

func TestAuditLogIsPrunedOnlyWhenConfigured(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	current := api.do(http.MethodGet, "/api/v1/settings", token, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["auditRetentionDays"] = 30

	if res := api.do(http.MethodPut, "/api/v1/settings", token, settings); res.Status != http.StatusOK {
		t.Fatalf("set retention: %d %s %s", res.Status, res.Code, res.Message)
	}

	// One entry old enough to go, and whatever the sign-in wrote, which is
	// not.
	api.execSQL(t, `INSERT INTO audit_logs (id, tenant_id, kind, action, result, created_at)
		SELECT 'ancient-entry', id, 'OPERATION', 'ANCIENT', 'SUCCESS', now() - interval '400 days'
		FROM tenants WHERE code = 'default'`)

	before := api.auditEntryCount(t, token)
	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	after := api.auditEntryCount(t, token)

	if after != before-1 {
		t.Errorf("after pruning there are %d entries, want %d — exactly the one "+
			"past the window should have gone", after, before-1)
	}
}

// A retention short enough to be a typo is refused. The difference between
// "we keep nothing" and "we keep a week" is the difference between an
// incident nobody can reconstruct and one somebody can.
func TestVeryShortAuditRetentionIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	current := api.do(http.MethodGet, "/api/v1/settings", token, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["auditRetentionDays"] = 1

	res := api.do(http.MethodPut, "/api/v1/settings", token, settings)
	if res.Status != http.StatusBadRequest || res.Code != "INVALID_SETTINGS" {
		t.Errorf("a one-day retention = %d %s, want 400 INVALID_SETTINGS",
			res.Status, res.Code)
	}
}
