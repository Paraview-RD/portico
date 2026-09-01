package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

func TestCreateAndGetInvitationByCode(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	err := q.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: "inv-1", Code: "WELCOME2026", GroupIds: []string{},
		Quota: 10, Status: "ACTIVE", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	got, err := q.GetInvitationByCode(ctx, "WELCOME2026")
	if err != nil {
		t.Fatalf("get by code: %v", err)
	}
	if got.ID != "inv-1" || got.Quota != 10 || got.UsedCount != 0 || got.Status != "ACTIVE" {
		t.Errorf("got %+v, unexpected fields", got)
	}

	list, err := q.ListInvitations(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list = %d invitations, want 1", len(list))
	}
}

// Codes are unique within a tenant, not globally — two tenants each handing
// out "WELCOME2026" is normal, the same reasoning as oauth_clients.client_id.
func TestInvitationCodesAreUniquePerTenantOnly(t *testing.T) {
	s := newTestStore(t)
	acme := newTestTenant(t, s, "acme")
	beta := newTestTenant(t, s, "beta")
	ctx := context.Background()
	now := store.Now()

	invite := func(id string) sqlcgen.CreateInvitationParams {
		return sqlcgen.CreateInvitationParams{
			ID: id, Code: "WELCOME2026", GroupIds: []string{},
			Quota: 5, Status: "ACTIVE", CreatedAt: now,
		}
	}

	if err := acme.CreateInvitation(ctx, invite("a")); err != nil {
		t.Fatalf("first tenant: %v", err)
	}
	if err := beta.CreateInvitation(ctx, invite("b")); err != nil {
		t.Fatalf("second tenant could not reuse the code: %v", err)
	}
	if err := acme.CreateInvitation(ctx, invite("c")); !store.IsUniqueViolation(err) {
		t.Errorf("duplicate within one tenant gave %v, want a unique violation", err)
	}
}

// RedeemInvitation's UPDATE ... WHERE used_count < quota is what a concurrent
// registration relies on: this checks it directly, without a service layer
// in the way, before the harder concurrent version below.
func TestRedeemInvitation_QuotaExceeded(t *testing.T) {
	s := newTestStore(t)
	tenantID := "tenant-acme"
	newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := s.Queries.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: "inv-1", TenantID: tenantID, Code: "ONE-USE", GroupIds: []string{},
		Quota: 1, Status: "ACTIVE", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	first, err := s.Queries.RedeemInvitation(ctx, sqlcgen.RedeemInvitationParams{
		TenantID: tenantID, ID: "inv-1", UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("first redemption: %v", err)
	}
	if first.UsedCount != 1 {
		t.Errorf("used_count = %d, want 1", first.UsedCount)
	}

	_, err = s.Queries.RedeemInvitation(ctx, sqlcgen.RedeemInvitationParams{
		TenantID: tenantID, ID: "inv-1", UpdatedAt: now,
	})
	if !store.IsNoRows(err) {
		t.Errorf("second redemption = %v, want no rows (quota exhausted)", err)
	}
}

func TestRedeemInvitation_Disabled(t *testing.T) {
	s := newTestStore(t)
	tenantID := "tenant-acme"
	newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := s.Queries.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: "inv-1", TenantID: tenantID, Code: "DISABLED-CODE", GroupIds: []string{},
		Quota: 10, Status: "DISABLED", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	_, err := s.Queries.RedeemInvitation(ctx, sqlcgen.RedeemInvitationParams{
		TenantID: tenantID, ID: "inv-1", UpdatedAt: now,
	})
	if !store.IsNoRows(err) {
		t.Errorf("redeeming a disabled invitation = %v, want no rows", err)
	}
}

// The whole reason RedeemInvitation checks and increments in one statement:
// with quota=1 and many concurrent callers, exactly one may succeed. A
// check-then-update built from two statements would let more than one
// goroutine observe used_count < quota before either commits.
func TestRedeemInvitation_ConcurrentRedemption_RespectsQuota(t *testing.T) {
	s := newTestStore(t)
	tenantID := "tenant-acme"
	newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := s.Queries.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: "inv-1", TenantID: tenantID, Code: "RACE", GroupIds: []string{},
		Quota: 1, Status: "ACTIVE", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	const attempts = 10
	var wg sync.WaitGroup
	successes := make(chan bool, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Queries.RedeemInvitation(ctx, sqlcgen.RedeemInvitationParams{
				TenantID: tenantID, ID: "inv-1", UpdatedAt: store.Now(),
			})
			successes <- err == nil
		}()
	}
	wg.Wait()
	close(successes)

	won := 0
	for ok := range successes {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of %d concurrent redemptions succeeded, want exactly 1", won, attempts)
	}

	got, err := s.Queries.GetInvitation(ctx, sqlcgen.GetInvitationParams{TenantID: tenantID, ID: "inv-1"})
	if err != nil {
		t.Fatalf("get invitation: %v", err)
	}
	if got.UsedCount != 1 {
		t.Errorf("used_count = %d, want 1", got.UsedCount)
	}
}

func TestUpdateInvitationStatus(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := q.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: "inv-1", Code: "TO-DISABLE", GroupIds: []string{},
		Quota: 5, Status: "ACTIVE", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if err := q.UpdateInvitationStatus(ctx, "inv-1", "DISABLED", store.Now()); err != nil {
		t.Fatalf("disable: %v", err)
	}

	got, err := q.GetInvitation(ctx, "inv-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "DISABLED" {
		t.Errorf("status = %q, want DISABLED", got.Status)
	}
}
