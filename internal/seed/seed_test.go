package seed_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/seed"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// A seeded database, built once for the whole file.
//
// Seeding is not cheap — fifty-five accounts each cost a password hash on
// purpose — and every test here asks a different question of the same result,
// so building it per test would multiply the slowest part of it by the number
// of questions.
func seedOnce(t *testing.T) (*store.Store, seed.Summary, time.Time) {
	t.Helper()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := &config.Config{
		DatabaseDriver: "postgres",
		JWTSecret:      []byte("0123456789abcdef0123456789abcdef"),
		TokenTTL:       2 * time.Hour,
		// A key, so the paths that store a credential are the ones exercised.
		// Without it the seed still runs and registers directories with an
		// anonymous bind, which is a different and less interesting shape.
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	now := store.Now()
	summary, err := seed.New(st, cfg).Run(context.Background(), seed.Options{Now: now})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st, summary, now
}

// The seed produces enough accounts to page through.
//
// Fifty-five is not a round number chosen for looks: the console pages at
// twenty, and a list that fits on one page has never had its paging exercised
// by anybody looking at it.
func TestTheSeedFillsMoreThanOnePageOfAccounts(t *testing.T) {
	st, summary, _ := seedOnce(t)

	// Counted in the database rather than taken from the summary: the summary
	// is what the seeder believes it did, and this is the question of whether
	// it did it.
	//
	// consolePageSize is web/src/pages/UsersPage.tsx's PAGE_SIZE. Duplicated
	// rather than shared, because it is a fact about a screen in another
	// language; if it changes, this test asks for fewer pages than it thinks
	// and says so in its own message.
	const consolePageSize = 20

	var inMain int
	row := st.ForTenant(seed.TenantMain).DB().QueryRow(
		`SELECT count(*) FROM users u JOIN tenants t ON t.id = u.tenant_id WHERE t.code = $1`,
		seed.TenantMain)
	if err := row.Scan(&inMain); err != nil {
		t.Fatalf("count accounts: %v", err)
	}

	if inMain <= 2*consolePageSize {
		t.Errorf("the default tenant holds %d accounts; the console pages at %d, "+
			"so a third page — which is what makes paging visible — needs more than %d",
			inMain, consolePageSize, 2*consolePageSize)
	}
	if summary.Tenants < 2 {
		t.Errorf("seeded %d tenants; isolation cannot be seen with one", summary.Tenants)
	}
}

// Seeding twice over the same database is refused rather than doubled.
//
// The mistake this guards is a development tool pointed at the wrong DSN. The
// cost of that mistake is somebody's real user list with fifty-five invented
// colleagues in it, and a tool that appends silently is a tool that makes it
// unrecoverable.
func TestSeedingAPopulatedDatabaseIsRefused(t *testing.T) {
	st, _, _ := seedOnce(t)

	cfg := &config.Config{
		DatabaseDriver: "postgres",
		JWTSecret:      []byte("0123456789abcdef0123456789abcdef"),
		TokenTTL:       2 * time.Hour,
	}

	if _, err := seed.New(st, cfg).Run(context.Background(), seed.Options{}); err == nil {
		t.Error("seeding a database that already holds accounts succeeded; " +
			"pointing this at the wrong DSN would be unrecoverable")
	}
}

// Two runs with the same clock produce the same data.
//
// Not the same rows: identifiers are UUIDs and are random by construction, so
// nothing here compares them. What is compared is everything a person would
// look at — who exists, when they were created, how old their password is —
// because the value of determinism is being able to answer "is this the same
// data as yesterday", and an answer of "the names match but the dates have
// drifted" is no answer.
//
// The clock is passed in for the same reason. A seed that read the time itself
// could not be asked this question at all.
func TestTwoRunsWithTheSameClockProduceTheSameData(t *testing.T) {
	now := store.Now()

	fingerprints := make([]string, 2)
	for i := range fingerprints {
		st, err := store.Open("postgres", testdb.DSN(t))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })

		cfg := &config.Config{
			DatabaseDriver: "postgres",
			JWTSecret:      []byte("0123456789abcdef0123456789abcdef"),
			TokenTTL:       2 * time.Hour,
			EncryptionKey:  []byte("0123456789abcdef0123456789abcdef"),
		}
		if _, err := seed.New(st, cfg).Run(context.Background(), seed.Options{Now: now}); err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}

		rows, err := st.ForTenant(seed.TenantMain).DB().Query(`
SELECT t.code, u.username, u.display_name, u.status, u.source,
       u.created_at, u.password_changed_at
FROM users u JOIN tenants t ON t.id = u.tenant_id
ORDER BY t.code, u.username`)
		if err != nil {
			t.Fatalf("read accounts: %v", err)
		}
		var b strings.Builder
		for rows.Next() {
			var code, username, display, status, source string
			var created time.Time
			var changed *time.Time
			if err := rows.Scan(&code, &username, &display, &status, &source, &created, &changed); err != nil {
				t.Fatalf("scan: %v", err)
			}
			fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%v\n",
				code, username, display, status, source, created.UTC(), changed)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read accounts: %v", err)
		}
		_ = rows.Close()
		fingerprints[i] = b.String()
	}

	if fingerprints[0] != fingerprints[1] {
		t.Error("two runs with the same clock produced different data; " +
			"'is this the same database as yesterday' is then unanswerable")
	}
	if fingerprints[0] == "" {
		t.Error("the fingerprint is empty, so this test compared nothing")
	}
}
