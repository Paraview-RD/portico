package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
	"github.com/paraview/portico/internal/testdb"
)

// newTestStore opens a throwaway database with the migrations applied.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// newTestTenant creates a tenant and returns the query view bound to it.
// Every scoped table has a foreign key to tenants, so a fixture needs one
// before it can insert anything at all.
func newTestTenant(t *testing.T, s *store.Store, code string) *store.Scoped {
	t.Helper()
	ctx := context.Background()
	now := store.Now()
	id := "tenant-" + code

	err := s.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: id, Code: code, Name: code, Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create tenant %s: %v", code, err)
	}
	return s.ForTenant(id)
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := newTestStore(t)

	// If migrations ran, every table is queryable.
	for _, table := range []string{"tenants", "users", "organizations", "audit_logs", "system_settings"} {
		var count int
		if err := s.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Errorf("table %s is not queryable: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dsn := testdb.DSN(t)

	first, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = first.Close()

	// Reopening must not fail by trying to re-apply migrations.
	second, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = second.Close()
}

func TestOpenRejectsUnknownDriver(t *testing.T) {
	if _, err := store.Open("mysql", "whatever"); err == nil {
		t.Fatal("expected an error for an unimplemented driver")
	}
}

// Timestamps must survive the round trip as time.Time rather than silently
// becoming zero values, which is what lets service code treat them as
// ordinary Go times.
func TestTimestampRoundTrip(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	err := q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
		ID:        "org-1",
		Name:      "Engineering",
		Code:      "ENG",
		Status:    "ACTIVE",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	got, err := q.GetOrganizationByID(ctx, "org-1")
	if err != nil {
		t.Fatalf("get organization: %v", err)
	}

	if got.CreatedAt.IsZero() {
		t.Fatal("created_at came back as the zero time")
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, now)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	missing := "no-such-org"
	err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:             "user-1",
		Username:       "alice",
		DisplayName:    "Alice",
		PasswordHash:   "hash",
		Role:           "USER",
		Status:         "ACTIVE",
		OrganizationID: &missing,
		TokenVersion:   1,
		Source:         "ADMIN",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err == nil {
		t.Fatal("expected a foreign key violation for a nonexistent organization")
	}
	if !store.IsForeignKeyViolation(err) {
		t.Errorf("IsForeignKeyViolation = false for %v", err)
	}
}

// The service layer turns a lost insert race into a 409 by asking whether the
// error was a unique violation. That classification is only correct if it
// matches what this driver actually returns, which is what this checks — the
// previous implementation matched SQLite's message text and so answered "no"
// to every PostgreSQL error, quietly downgrading those conflicts to 500s.
func TestUniqueViolationIsRecognized(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	first := sqlcgen.CreateOrganizationParams{
		ID: "org-a", Name: "Engineering", Code: "ENG",
		Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	if err := q.CreateOrganization(ctx, first); err != nil {
		t.Fatalf("create first organization: %v", err)
	}

	duplicate := first
	duplicate.ID = "org-b"
	err := q.CreateOrganization(ctx, duplicate)
	if err == nil {
		t.Fatal("expected a unique violation on the duplicate code")
	}
	if !store.IsUniqueViolation(err) {
		t.Errorf("IsUniqueViolation = false for %v", err)
	}
	if store.IsForeignKeyViolation(err) {
		t.Error("a unique violation was classified as a foreign key violation")
	}
}

// ForTenant does not verify that its tenant exists, on the grounds that
// callers only ever get one from an authenticated principal or a checked
// lookup, and that an empty one fails closed regardless. That second half is
// a claim in a doc comment which a later contributor may well lean on, so it
// is worth holding to it: a read must find nothing rather than everything,
// and a write must be refused rather than land somewhere unreachable.
func TestEmptyTenantFailsClosed(t *testing.T) {
	s := newTestStore(t)
	existing := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	err := existing.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: "user-1", Username: "alice", DisplayName: "Alice",
		PasswordHash: "hash", Role: "USER", Status: "ACTIVE",
		TokenVersion: 1, Source: "ADMIN", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	empty := s.ForTenant("")

	if _, err := empty.GetUserByID(ctx, "user-1"); !store.IsNoRows(err) {
		t.Errorf("read through an empty tenant returned %v, want no rows", err)
	}
	if count, err := empty.CountUsers(ctx); err != nil || count != 0 {
		t.Errorf("count through an empty tenant = %d, %v; want 0, nil", count, err)
	}

	err = empty.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: "user-2", Username: "mallory", DisplayName: "Mallory",
		PasswordHash: "hash", Role: "USER", Status: "ACTIVE",
		TokenVersion: 1, Source: "ADMIN", CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("a write through an empty tenant succeeded")
	}
	if !store.IsForeignKeyViolation(err) {
		t.Errorf("write was refused by %v, want a foreign key violation", err)
	}
}

func TestErrorClassifiersRejectUnrelatedErrors(t *testing.T) {
	for _, err := range []error{nil, errSentinel{}, sql.ErrNoRows} {
		if store.IsUniqueViolation(err) {
			t.Errorf("IsUniqueViolation = true for %v", err)
		}
		if store.IsForeignKeyViolation(err) {
			t.Errorf("IsForeignKeyViolation = true for %v", err)
		}
	}
	if !store.IsNoRows(sql.ErrNoRows) {
		t.Error("IsNoRows = false for sql.ErrNoRows")
	}
	if store.IsNoRows(errSentinel{}) {
		t.Error("IsNoRows = true for an unrelated error")
	}
}

// CHECK constraints are the last line of defense if a service layer bug lets
// an invalid enum through.
func TestStatusCheckConstraint(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:           "user-1",
		Username:     "alice",
		DisplayName:  "Alice",
		PasswordHash: "hash",
		Role:         "USER",
		Status:       "BOGUS",
		TokenVersion: 1,
		Source:       "ADMIN",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err == nil {
		t.Fatal("expected a CHECK constraint violation for an invalid status")
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	s := newTestStore(t)
	scoped := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	wantErr := errSentinel{}
	err := s.WithTx(func(q *sqlcgen.Queries) error {
		if err := q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
			ID: "org-rollback", TenantID: "tenant-acme", Name: "Temp", Code: "TMP",
			Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want the sentinel", err)
	}

	if _, err := scoped.GetOrganizationByID(ctx, "org-rollback"); err == nil {
		t.Error("row survived a rolled-back transaction")
	}
}

func TestWithTxCommitsOnSuccess(t *testing.T) {
	s := newTestStore(t)
	scoped := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	err := s.WithTx(func(q *sqlcgen.Queries) error {
		return q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
			ID: "org-commit", TenantID: "tenant-acme", Name: "Keep", Code: "KEEP",
			Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	if _, err := scoped.GetOrganizationByID(ctx, "org-commit"); err != nil {
		t.Errorf("committed row is missing: %v", err)
	}
}

// A timestamp written and read back must compare equal, which is what lets
// service code treat these as ordinary Go times.
func TestTimestampRoundTripPreservesValue(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := q.UpsertSettings(ctx, map[string]string{"k": "v"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := q.GetSetting(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, now)
	}
}

func TestNowIsUTC(t *testing.T) {
	got := store.Now()
	if got.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", got.Location())
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }
