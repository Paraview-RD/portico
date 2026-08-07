package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
	"github.com/paraview/portico/internal/testdb"
)

// newTestStore opens a throwaway database on disk. A file (rather than
// :memory:) is used because migrations and the single-connection pool
// behave the same way there as in production.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := newTestStore(t)

	// If migrations ran, every table is queryable.
	for _, table := range []string{"users", "organizations", "audit_logs", "system_settings"} {
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

// Timestamps are stored as text; this verifies they survive the round trip
// as time.Time rather than silently becoming zero values.
func TestTimestampRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := store.Now()

	err := s.Queries.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
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

	got, err := s.Queries.GetOrganizationByID(ctx, "org-1")
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
	ctx := context.Background()
	now := store.Now()

	missing := "no-such-org"
	err := s.Queries.CreateUser(ctx, sqlcgen.CreateUserParams{
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
}

// CHECK constraints are the last line of defense if a service layer bug lets
// an invalid enum through.
func TestStatusCheckConstraint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := store.Now()

	err := s.Queries.CreateUser(ctx, sqlcgen.CreateUserParams{
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
	ctx := context.Background()
	now := store.Now()

	wantErr := errSentinel{}
	err := s.WithTx(func(q *sqlcgen.Queries) error {
		if err := q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
			ID: "org-rollback", Name: "Temp", Code: "TMP",
			Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want the sentinel", err)
	}

	if _, err := s.Queries.GetOrganizationByID(ctx, "org-rollback"); err == nil {
		t.Error("row survived a rolled-back transaction")
	}
}

func TestWithTxCommitsOnSuccess(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := store.Now()

	err := s.WithTx(func(q *sqlcgen.Queries) error {
		return q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
			ID: "org-commit", Name: "Keep", Code: "KEEP",
			Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	if _, err := s.Queries.GetOrganizationByID(ctx, "org-commit"); err != nil {
		t.Errorf("committed row is missing: %v", err)
	}
}

// A timestamp written and read back must compare equal, which is what lets
// service code treat these as ordinary Go times.
func TestTimestampRoundTripPreservesValue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := store.Now()

	if err := s.Queries.UpsertSetting(ctx, sqlcgen.UpsertSettingParams{
		Key: "k", Value: "v", UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.Queries.GetSetting(ctx, "k")
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
