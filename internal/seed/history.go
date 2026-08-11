package seed

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/model"
)

// The past, written directly.
//
// This is the one place in the seed that does not go through the service
// layer, and the reason is the only reason that would justify it: the service
// layer stamps store.Now() on what it writes, and every row here exists to
// have happened at some other time. Ninety days of sign-ins recorded in the
// same second is not history, it is a list.
//
// What is given up by writing these rows directly is validation, and these are
// the tables where there is almost none to give up: an audit entry, a session,
// a delivery attempt, a synchronization run. They are append-only records of
// moments, constrained by foreign keys and a CHECK on their enumerations, and
// nothing reads them back expecting an invariant the writer had to maintain.
// Every table with a rule attached — accounts, organizations, applications,
// credentials — went through the service layer in an earlier stage.
//
// One consequence worth stating: this file knows column names. It breaks if a
// column is renamed, which is the correct outcome — and the guard test is what
// turns that break into something somebody sees.

// HistoryDays is how far back the seed reaches.
//
// Ninety, matching the strict tenant's password age and its session cap, so
// that the oldest data is exactly as old as the oldest thing the product will
// still act on. Shorter and the retention and expiry screens have nothing in
// range; longer and the seed is inventing a past nobody asked about.
const HistoryDays = 90

func (s *Seeder) seedHistory(ctx context.Context, w *world) error {
	for i := range w.tenants {
		t := &w.tenants[i]

		if err := s.spreadSeedAudit(ctx, t, w.opts.Now); err != nil {
			return err
		}
		if err := s.writeSignInHistory(ctx, t, w); err != nil {
			return err
		}
		if err := s.writeSessions(ctx, t, w); err != nil {
			return err
		}
		// Every tenant, not just the first. The accounts of a tenant this
		// skipped would keep the timestamp the service stamped on them, which
		// is whenever the seed happened to run — so two runs would produce
		// data that differed in a way nobody could see and nothing could
		// explain. The account-specific statements match no rows in the other
		// tenant, which is the correct outcome rather than a special case.
		if err := s.ageAccountCredentials(ctx, t, w.opts.Now); err != nil {
			return err
		}
	}

	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}
	if err := s.writeSyncRuns(ctx, t, w); err != nil {
		return err
	}
	return s.writeDeliveries(ctx, t, w)
}

// db is the raw handle, for the statements below.
func (s *Seeder) db(tenantID string) *sql.DB { return s.store.ForTenant(tenantID).DB() }

// spreadSeedAudit backdates the audit entries the earlier stages produced.
//
// Every service call the seed made wrote one, all within the same few seconds,
// which would leave the audit screen showing a hundred entries at one instant
// and nothing before it. Spreading them is not cosmetic: the screen's filters
// are a date range, and a range filter over data with no range in it cannot be
// tried out.
//
// Ordered by timestamp then id so the result is stable across runs, and spaced
// arithmetically for the same reason.
func (s *Seeder) spreadSeedAudit(ctx context.Context, t *seededTenant, now time.Time) error {
	const spread = `
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS n,
           count(*) OVER () AS total
    FROM audit_logs
    WHERE tenant_id = $1
)
UPDATE audit_logs a
SET created_at = $2::timestamptz - make_interval(mins =>
        ((ordered.total - ordered.n) * $3 / GREATEST(ordered.total, 1))::int)
FROM ordered
WHERE a.id = ordered.id AND a.tenant_id = $1`

	window := HistoryDays * 24 * 60
	if _, err := s.db(t.tenant.ID).ExecContext(ctx, spread, t.tenant.ID, now, window); err != nil {
		return fmt.Errorf("spread audit history for tenant %s: %w", t.tenant.Code, err)
	}
	return nil
}

// writeSignInHistory adds the sign-ins nobody made.
//
// Successes and failures both, because the screen exists to answer "was this
// account being guessed at" and a trail of successes cannot. The failures are
// concentrated on one account on one afternoon, which is what a real attempt
// looks like and what makes the account and date filters worth trying.
func (s *Seeder) writeSignInHistory(ctx context.Context, t *seededTenant, w *world) error {
	if len(t.users) == 0 {
		return nil
	}
	now := w.opts.Now

	type entry struct {
		user   model.User
		action string
		result model.LogResult
		at     time.Time
		ip     string
	}
	var entries []entry

	// A steady background of successful sign-ins, spread over the window and
	// uneven between accounts, so the busiest are visibly busier.
	for i, user := range t.users {
		if user.Status == model.StatusDisabled {
			continue
		}
		for k := 0; k < 3+i%4; k++ {
			daysAgo := (i*7+k*11)%HistoryDays + 1
			entries = append(entries, entry{
				user: user, action: model.ActionLoginSuccess, result: model.LogSuccess,
				at: now.AddDate(0, 0, -daysAgo).Add(time.Duration(i%9) * time.Hour),
				ip: officeIP(i),
			})
		}
	}

	// And one account having a bad afternoon nine days ago: five failures.
	// Five is the strict tenant's lockout threshold, so this is also the shape
	// that produced the locked account.
	if target := findUser(t.users, "locked.out"); target.ID != "" {
		bad := now.AddDate(0, 0, -9).Add(14 * time.Hour)
		for k := 0; k < 5; k++ {
			entries = append(entries, entry{
				user: target, action: model.ActionLoginFailure, result: model.LogFailure,
				at: bad.Add(time.Duration(k) * 90 * time.Second), ip: "203.0.113.77",
			})
		}
	}

	const insert = `
INSERT INTO audit_logs (
    id, tenant_id, kind, action, actor_id, actor_username,
    target_type, target_id, target_name, result, detail, ip, created_at
) VALUES ($1, $2, $3, $4, $5, $6, 'USER', $5, $6, $7, $8, $9, $10)`

	db := s.db(t.tenant.ID)
	for _, e := range entries {
		detail := "seeded"
		if e.result == model.LogFailure {
			detail = "wrong password"
		}
		_, err := db.ExecContext(ctx, insert,
			uuid.NewString(), t.tenant.ID, string(model.LogLogin), e.action,
			e.user.ID, e.user.Username, string(e.result), detail, e.ip, e.at)
		if err != nil {
			return fmt.Errorf("write sign-in history: %w", err)
		}
		w.summary.AuditEntries++
	}
	return nil
}

// officeIP varies the source address so the audit trail has more than one.
// Documentation ranges only: a seed that invented plausible public addresses
// would be putting somebody else's network in a screenshot.
func officeIP(i int) string {
	switch i % 3 {
	case 0:
		return "192.0.2.10"
	case 1:
		return "198.51.100.24"
	default:
		return "203.0.113.5"
	}
}

// writeSessions gives some accounts devices they are still signed in on.
//
// The portal lists them and offers to revoke them, and three states matter:
// live here, live elsewhere, and revoked. A seed with only live sessions leaves
// the revoked rendering — the one somebody checks after a laptop is stolen —
// never drawn.
func (s *Seeder) writeSessions(ctx context.Context, t *seededTenant, w *world) error {
	const insert = `
INSERT INTO sessions (
    id, tenant_id, user_id, ip, user_agent,
    created_at, last_seen_at, expires_at, revoked_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	agents := []string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/131.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Edg/131.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148",
	}

	db := s.db(t.tenant.ID)
	now := w.opts.Now

	for i, user := range t.users {
		if user.Status == model.StatusDisabled || i%4 != 0 {
			continue
		}
		for k := 0; k < 2; k++ {
			created := now.AddDate(0, 0, -(i%20 + k*3 + 1))
			lastSeen := created.Add(time.Duration(2+k) * time.Hour)
			expires := created.AddDate(0, 0, 30)

			// Every third session was revoked, which is what signing out of
			// another device leaves behind.
			var revoked any
			if (i+k)%3 == 0 {
				revoked = lastSeen.Add(20 * time.Minute)
			}

			_, err := db.ExecContext(ctx, insert,
				uuid.NewString(), t.tenant.ID, user.ID, officeIP(i+k),
				agents[(i+k)%len(agents)], created, lastSeen, expires, revoked)
			if err != nil {
				return fmt.Errorf("write session: %w", err)
			}
			w.summary.Sessions++
		}
	}
	return nil
}

// ageAccountCredentials produces the states an account reaches by the passage
// of time rather than by anybody doing anything.
//
// There is no API for "this password is 87 days old" because nothing does it;
// time does. So the timestamps are set here, and the screens that read them —
// the portal's expiry warning, the user list's unlock button — have something
// to show.
func (s *Seeder) ageAccountCredentials(ctx context.Context, t *seededTenant, now time.Time) error {
	db := s.db(t.tenant.ID)

	// Passwords set at various points in the past rather than all at once, so
	// the expiry column reads as a distribution.
	if _, err := db.ExecContext(ctx, `
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY username) AS n FROM users WHERE tenant_id = $1
)
UPDATE users u
SET password_changed_at = $2::timestamptz - make_interval(days => (ordered.n % 80)::int)
FROM ordered
WHERE u.id = ordered.id AND u.tenant_id = $1`, t.tenant.ID, now); err != nil {
		return fmt.Errorf("age passwords: %w", err)
	}

	// And one account deliberately at 87 days against a 90-day policy, so the
	// portal's "your password expires in 3 days" warning is on screen for
	// somebody to read rather than described in a document.
	if _, err := db.ExecContext(ctx, `
UPDATE users SET password_changed_at = $2::timestamptz - make_interval(days => 87)
WHERE tenant_id = $1 AND username = 'password.stale'`, t.tenant.ID, now); err != nil {
		return fmt.Errorf("age the stale password: %w", err)
	}

	// The locked account, matching the five failures in the audit trail:
	// locked for another twenty minutes, so the unlock button is live rather
	// than describing a lock that has already lapsed.
	if _, err := db.ExecContext(ctx, `
UPDATE users SET failed_login_attempts = 5,
    locked_until = $2::timestamptz + make_interval(mins => 20)
WHERE tenant_id = $1 AND username = 'locked.out'`, t.tenant.ID, now); err != nil {
		return fmt.Errorf("lock the locked account: %w", err)
	}

	// Accounts created over the window rather than in one afternoon, so that
	// the user list sorted by creation date is not arbitrary.
	if _, err := db.ExecContext(ctx, `
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY username) AS n,
           count(*) OVER () AS total
    FROM users WHERE tenant_id = $1
)
UPDATE users u
SET created_at = $2::timestamptz - make_interval(days =>
        ($3 * (ordered.total - ordered.n) / GREATEST(ordered.total, 1))::int)
FROM ordered
WHERE u.id = ordered.id AND u.tenant_id = $1`, t.tenant.ID, now, HistoryDays); err != nil {
		return fmt.Errorf("age accounts: %w", err)
	}
	return nil
}

// syncRun is one row of a directory's run history.
type syncRun struct {
	// actor is empty for a scheduled run, which is what the console renders as
	// "scheduled" — and most of these are scheduled, because that is what
	// having a schedule means.
	actor         string
	daysAgo       int
	outcome       string
	created       int
	updated       int
	deactivated   int
	skipped       int
	skippedDetail string
	errCode       string
	errText       string
}

// The working directory: a fortnight of quiet successes with one day where the
// deactivated count jumps. That row is what somebody scrolls to when a
// department has vanished from a directory, and a history without it cannot
// answer the question the history exists for.
var workingRuns = []syncRun{
	{actor: "zhangwei", daysAgo: 14, outcome: model.SyncSucceeded, created: 12, skipped: 1,
		skippedDetail: "1 × That username is already in use. (admin)"},
	{daysAgo: 10, outcome: model.SyncSucceeded, updated: 2, skipped: 1,
		skippedDetail: "1 × That username is already in use. (admin)"},
	{daysAgo: 7, outcome: model.SyncSucceeded, updated: 1, skipped: 1,
		skippedDetail: "1 × That username is already in use. (admin)"},
	{daysAgo: 4, outcome: model.SyncSucceeded, updated: 1, deactivated: 6, skipped: 1,
		skippedDetail: "1 × That username is already in use. (admin)"},
	{daysAgo: 2, outcome: model.SyncSucceeded, skipped: 3,
		skippedDetail: "2 × That is not a valid phone number. (mei, arjun); " +
			"1 × That username is already in use. (admin)"},
	{daysAgo: 0, outcome: model.SyncSucceeded, updated: 1, skipped: 1,
		skippedDetail: "1 × That username is already in use. (admin)"},
}

// And the one that cannot be reached. Two kinds of failure, because they send
// an operator to different places: the directory's own wording, left
// untranslated on purpose, and Portico's own refusal, which carries a code the
// console renders in the reader's language.
var brokenRuns = []syncRun{
	{actor: "zhangwei", daysAgo: 21, outcome: model.SyncSucceeded, created: 3},
	{actor: "zhangwei", daysAgo: 12, outcome: model.SyncFailed,
		errText: `LDAP Result Code 200 "Network Error": dial tcp: lookup ldap.acme.invalid: no such host`},
	{daysAgo: 5, outcome: model.SyncFailed,
		errText: `LDAP Result Code 32 "No Such Object": `},
	{actor: "chenjing", daysAgo: 1, outcome: model.SyncFailed,
		errCode: "DIRECTORY_RETURNED_NOTHING",
		errText: "The directory returned no entries, so nothing was changed."},
}

func (s *Seeder) writeSyncRuns(ctx context.Context, t *seededTenant, w *world) error {
	if len(t.directories) == 0 {
		return nil
	}
	now := w.opts.Now
	db := s.db(t.tenant.ID)

	const insert = `
INSERT INTO ldap_sync_runs (
    id, tenant_id, source_id, actor_name, started_at, finished_at, outcome,
    created_count, updated_count, deactivated_count, skipped_count,
    skipped_detail, error_code, error
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	for i, source := range t.directories {
		runs := workingRuns
		if i > 0 {
			runs = brokenRuns
		}
		for _, r := range runs {
			started := now.AddDate(0, 0, -r.daysAgo).Add(3 * time.Hour)
			finished := started.Add(42 * time.Second)

			_, err := db.ExecContext(ctx, insert,
				uuid.NewString(), t.tenant.ID, source.ID, r.actor, started, finished,
				r.outcome, r.created, r.updated, r.deactivated, r.skipped,
				r.skippedDetail, r.errCode, r.errText)
			if err != nil {
				return fmt.Errorf("write sync run for %s: %w", source.Name, err)
			}
			w.summary.SyncRuns++
		}

		// last_synced_at is the last success; last_sync_attempt_at is the last
		// attempt. Setting them apart is the whole reason they are two
		// columns, and on the broken directory they disagree — which is
		// exactly the state an operator has to be able to read.
		if _, err := db.ExecContext(ctx, `
UPDATE ldap_sources SET last_synced_at = (
        SELECT max(finished_at) FROM ldap_sync_runs
        WHERE tenant_id = $1 AND source_id = $2 AND outcome = 'SUCCEEDED'),
    last_sync_attempt_at = (
        SELECT max(started_at) FROM ldap_sync_runs
        WHERE tenant_id = $1 AND source_id = $2)
WHERE tenant_id = $1 AND id = $2`, t.tenant.ID, source.ID); err != nil {
			return fmt.Errorf("date directory %s: %w", source.Name, err)
		}
	}
	return nil
}

// delivery is one row of a subscription's delivery history.
type delivery struct {
	event     string
	status    string
	attempts  int
	hoursAgo  int
	lastCode  int
	lastError string
	// retryIn is how long until the next attempt. Zero means none is
	// scheduled, which is what both DELIVERED and FAILED look like.
	retryIn time.Duration
}

// All four states, because each means something different to whoever is
// looking: delivered is nothing to do, pending is waiting, retrying is a
// receiver having a bad day, and failed is one that has been down long enough
// to have been given up on. A history of successes makes the screen look like
// it has no reason to exist.
var deliveries = []delivery{
	{event: "user.created", status: "DELIVERED", attempts: 1, hoursAgo: 72, lastCode: 200},
	{event: "user.updated", status: "DELIVERED", attempts: 1, hoursAgo: 40, lastCode: 204},
	{event: "user.disabled", status: "DELIVERED", attempts: 2, hoursAgo: 26, lastCode: 200},
	// Waiting for the next pass, which is the ordinary state of an event that
	// has just been raised.
	{event: "organization.updated", status: "PENDING", attempts: 0, hoursAgo: 0,
		retryIn: 10 * time.Second},
	// Retrying, with the reason. A receiver returning 502 is the common case
	// and the one an operator has to tell apart from a signature they got
	// wrong.
	{event: "user.updated", status: "PENDING", attempts: 3, hoursAgo: 5, lastCode: 502,
		lastError: "unexpected status 502", retryIn: 8 * time.Minute},
	// And one that has given up.
	{event: "user.created", status: "FAILED", attempts: 6, hoursAgo: 60, lastCode: 500,
		lastError: "unexpected status 500"},
}

func (s *Seeder) writeDeliveries(ctx context.Context, t *seededTenant, w *world) error {
	if len(t.subscriptions) == 0 {
		return nil
	}
	now := w.opts.Now
	db := s.db(t.tenant.ID)

	const insert = `
INSERT INTO webhook_deliveries (
    id, tenant_id, subscription_id, event_type, payload, status,
    attempts, last_error, last_status, next_attempt_at, created_at, delivered_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	payload, err := json.Marshal(map[string]any{
		"id":       "evt-seeded",
		"type":     "user.disabled",
		"tenant":   t.tenant.Code,
		"occurred": now.Format(time.RFC3339),
		"data":     map[string]any{"username": "left.company"},
	})
	if err != nil {
		return fmt.Errorf("marshal delivery payload: %w", err)
	}

	for i, sub := range t.subscriptions {
		for k, d := range deliveries {
			// The second subscription gets the other half of the list, so the
			// two do not read as copies of each other.
			if i > 0 && k%2 == 0 {
				continue
			}
			created := now.Add(-time.Duration(d.hoursAgo) * time.Hour)

			var delivered, next any
			if d.status == "DELIVERED" {
				delivered = created.Add(time.Duration(d.attempts) * 2 * time.Second)
			}
			if d.retryIn > 0 {
				next = created.Add(d.retryIn)
			}

			_, err := db.ExecContext(ctx, insert,
				uuid.NewString(), t.tenant.ID, sub.ID, d.event, string(payload),
				d.status, d.attempts, d.lastError, d.lastCode, next, created, delivered)
			if err != nil {
				return fmt.Errorf("write delivery for %s: %w", sub.Name, err)
			}
			w.summary.Deliveries++
		}
	}

	// One subscription mid-rotation: an old key still being sent alongside the
	// new one, with hours left on it. The console warns while the window is
	// open, and that warning is what a receiver's operator needs to see — so
	// the seed leaves it open rather than describing it.
	if _, err := db.ExecContext(ctx, `
UPDATE webhook_subscriptions
SET previous_secret = 'whsec_seeded_previous_key',
    previous_secret_expires_at = $2::timestamptz + make_interval(hours => 6)
WHERE tenant_id = $1 AND id = $3`,
		t.tenant.ID, now, t.subscriptions[0].ID); err != nil {
		return fmt.Errorf("open a rotation window: %w", err)
	}
	return nil
}
