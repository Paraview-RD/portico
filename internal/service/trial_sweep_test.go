package service

// What gives the demonstration's quota back.
//
// The failure this exists to prevent is not a flood. The quota counts
// confirmed trial requests, nothing ever decremented it, and created tenants
// lived forever — so fifty ordinary visitors closed the signup permanently,
// with no error anywhere and nothing in the interface to say why. These tests
// are about the two halves of taking one back, and about not taking back the
// wrong thing.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// sweeper returns a trial service on a throwaway database, with the clock
// under the test's control.
func sweeper(t *testing.T, now func() time.Time) (*TrialService, *store.Store) {
	t.Helper()
	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return &TrialService{
		store:   st,
		tenants: NewTenantService(st),
		now:     now,
	}, st
}

// trialTenant creates a tenant with a deadline and the trial_requests row that
// holds its code, its quota slot, and its applicant's mailbox — the shape a
// confirmed trial actually leaves behind.
func trialTenant(t *testing.T, st *store.Store, code string, expiresAt time.Time) {
	t.Helper()
	ctx := context.Background()
	now := store.Now()

	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: "tenant-" + code, Code: code, Name: code, Status: "ACTIVE",
		ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant %s: %v", code, err)
	}

	if _, err := st.Queries.CreateTrialRequest(ctx, sqlcgen.CreateTrialRequestParams{
		ID:          "req-" + code,
		Email:       code + "@example.test",
		EmailKey:    code + "@example.test",
		CompanyName: code,
		TenantCode:  code,
		Industry:    "generic",
		TokenHash:   "hash-" + code,
		ExpiresAt:   now.Add(24 * time.Hour),
		RequestIp:   "203.0.113.7",
	}); err != nil {
		t.Fatalf("create trial request %s: %v", code, err)
	}
	tenantID := "tenant-" + code
	if _, err := st.Queries.MarkTrialRequestConfirmed(ctx, sqlcgen.MarkTrialRequestConfirmedParams{
		ID:       "req-" + code,
		TenantID: &tenantID,
	}); err != nil {
		t.Fatalf("confirm trial request %s: %v", code, err)
	}
}

func tenantStatus(t *testing.T, st *store.Store, code string) (string, bool) {
	t.Helper()
	row, err := st.Queries.GetTenantByCode(context.Background(), code)
	if err != nil {
		if store.IsNoRows(err) {
			return "", false
		}
		t.Fatalf("read tenant %s: %v", code, err)
	}
	return row.Status, true
}

// A deadline that has passed disables the tenant, and does not delete it.
//
// The order matters more than it looks. Disabling is reversible: everything
// the person built is still there, and an operator who hears from them can
// move the deadline and switch it back on. Deleting at the deadline would make
// a fortnight a cliff.
func TestAPassedDeadlineDisablesRatherThanDeletes(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })

	trialTenant(t, st, "overdue", now.Add(-time.Hour))
	trialTenant(t, st, "current", now.Add(TrialTenantTTL))

	disabled, deleted, err := service.SweepTenants(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if disabled != 1 || deleted != 0 {
		t.Errorf("disabled %d and deleted %d, wanted 1 and 0", disabled, deleted)
	}

	if status, exists := tenantStatus(t, st, "overdue"); !exists {
		t.Error("the overdue tenant was deleted at its deadline")
	} else if status != string(model.StatusDisabled) {
		t.Errorf("the overdue tenant is %s", status)
	}
	if status, _ := tenantStatus(t, st, "current"); status != string(model.StatusActive) {
		t.Errorf("a tenant inside its fortnight is %s", status)
	}
}

// Running twice does not write twice.
//
// The disable query has the status in its predicate for this reason: a sweep
// that reports one disabled tenant on every pass forever is a sweep nobody can
// read the logs of.
func TestSweepingTwiceReportsTheWorkOnce(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })
	trialTenant(t, st, "overdue", now.Add(-time.Hour))

	if disabled, _, err := service.SweepTenants(context.Background()); err != nil || disabled != 1 {
		t.Fatalf("first pass: %d disabled, %v", disabled, err)
	}
	disabled, deleted, err := service.SweepTenants(context.Background())
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if disabled != 0 || deleted != 0 {
		t.Errorf("second pass reported %d disabled and %d deleted", disabled, deleted)
	}
}

// The grace period expiring deletes the tenant and releases what it held.
//
// The released part is the point. The quota counts confirmed requests, the
// tenant code is reserved by one, and one-tenant-per-mailbox is an index on
// one — so a tenant deleted without its request row leaves all three held by
// something that no longer exists, and the quota never recovers.
func TestTheGracePeriodExpiringReleasesTheQuotaAndTheMailbox(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })

	// Expired well beyond the grace period.
	trialTenant(t, st, "ancient", now.Add(-TrialTenantGrace-time.Hour))
	ctx := context.Background()

	before, err := st.Queries.CountConfirmedTrials(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if before == 0 {
		t.Fatal("the fixture did not create a confirmed request, so this test " +
			"cannot show one being released")
	}

	if _, deleted, err := service.SweepTenants(ctx); err != nil || deleted != 1 {
		t.Fatalf("%d deleted, %v", deleted, err)
	}

	if _, exists := tenantStatus(t, st, "ancient"); exists {
		t.Error("the tenant outlived its grace period")
	}
	after, err := st.Queries.CountConfirmedTrials(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if after != before-1 {
		t.Errorf("the quota went from %d to %d — the reservation was not released, "+
			"so this slot is held by a tenant that no longer exists", before, after)
	}
}

// The default tenant is never touched, whatever its row says.
//
// It should never carry a deadline at all. But a bug that gave it one — a
// migration with a DEFAULT, a console action pointed at the wrong code — would
// otherwise take the whole deployment offline on a timer, and then delete it a
// week later. This is the guard for the worst thing this sweep could do.
func TestTheDefaultTenantIsNeverSweptEvenWithADeadline(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })
	ctx := context.Background()

	past := now.Add(-TrialTenantGrace - 24*time.Hour)
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: "tenant-default", Code: model.DefaultTenantCode, Name: "Default",
		Status: "ACTIVE", ExpiresAt: &past,
		CreatedAt: store.Now(), UpdatedAt: store.Now(),
	}); err != nil {
		t.Fatalf("create the default tenant: %v", err)
	}

	if _, _, err := service.SweepTenants(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	status, exists := tenantStatus(t, st, model.DefaultTenantCode)
	if !exists {
		t.Fatal("the sweep deleted the default tenant")
	}
	if status != string(model.StatusActive) {
		t.Errorf("the sweep disabled the default tenant: %s", status)
	}
}

// A tenant with no deadline is not swept, which is every tenant on an ordinary
// deployment.
func TestATenantWithNoDeadlineIsLeftAlone(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })
	ctx := context.Background()

	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: "tenant-forever", Code: "forever", Name: "Forever", Status: "ACTIVE",
		ExpiresAt: nil, CreatedAt: store.Now(), UpdatedAt: store.Now(),
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	disabled, deleted, err := service.SweepTenants(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if disabled != 0 || deleted != 0 {
		t.Errorf("a tenant with no deadline was swept: %d disabled, %d deleted",
			disabled, deleted)
	}
	if status, _ := tenantStatus(t, st, "forever"); status != string(model.StatusActive) {
		t.Errorf("status is %s", status)
	}
}

// The quota is published on every sweep, including one that changed nothing.
//
// A gauge that appears only when something happened cannot be alerted on: an
// operator asking "how close is the demonstration to closing itself" needs an
// answer on a quiet day too.
func TestTheQuotaIsPublishedEvenWhenNothingWasSwept(t *testing.T) {
	now := time.Now().UTC()
	service, st := sweeper(t, func() time.Time { return now })
	registry := metrics.New()
	service.metrics = registry
	service.maxTenants = 50

	trialTenant(t, st, "current", now.Add(TrialTenantTTL))

	disabled, deleted, err := service.SweepTenants(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if disabled != 0 || deleted != 0 {
		t.Fatalf("the fixture was swept: %d disabled, %d deleted", disabled, deleted)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	seen := map[string]float64{}
	for _, family := range families {
		switch family.GetName() {
		case "portico_trial_tenants", "portico_trial_tenants_max":
			seen[family.GetName()] = family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	if seen["portico_trial_tenants"] != 1 {
		t.Errorf("portico_trial_tenants is %v, want 1", seen["portico_trial_tenants"])
	}
	if seen["portico_trial_tenants_max"] != 50 {
		t.Errorf("portico_trial_tenants_max is %v, want 50", seen["portico_trial_tenants_max"])
	}
}

// The fill gate lets one through at a time and does not lose the others.
//
// The risk it guards is the process being killed mid-fill on a 512MB instance,
// so what matters is that concurrency is bounded and that nothing is dropped —
// a gate that refused the second caller would trade a restart for a tenant
// that is permanently empty.
func TestTheFillGateAdmitsOneAtATimeWithoutDroppingAny(t *testing.T) {
	service := (&TrialService{}).WithFillLimit(1)

	var mu sync.Mutex
	concurrent, peak, completed := 0, 0, 0

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service.filling <- struct{}{}
			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			concurrent--
			completed++
			mu.Unlock()
			<-service.filling
		}()
	}
	wg.Wait()

	if peak != 1 {
		t.Errorf("%d fills ran at once; the gate is meant to allow 1", peak)
	}
	if completed != 8 {
		t.Errorf("%d of 8 fills completed — the gate dropped some", completed)
	}
}
