package service

// Coverage for SMSLoginEnabled: turning it on is refused unless this
// deployment can actually reach somebody by SMS specifically -- not merely
// "can deliver something" (see CanDeliverSMS vs CanDeliver).
//
// There was no existing settings_test.go in this package to extend; this
// file's fixture mirrors newInvitationFixture in invitation_test.go (same
// store.Open/testdb.DSN/CreateTenant/NewSettingsService shape), which is the
// closest existing round-trip-through-settings pattern in this codebase.

import (
	"context"
	"testing"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// newSettingsServiceForTest returns a SettingsService backed by a real test
// database, with one tenant already created, so a test can round-trip
// Get/Update without a database of its own.
func newSettingsServiceForTest(t *testing.T) (*SettingsService, string) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-settings"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "set", Name: "Settings", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	return NewSettingsService(st, 0), tenantID
}

func TestSMSLoginRequiresADeliverableSMSChannel(t *testing.T) {
	svc, tenantID := newSettingsServiceForTest(t)
	svc.WithDeliveryChannels(func() []model.RecoveryChannel { return nil }) // no channel configured

	current, err := svc.Get(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	current.SMSLoginEnabled = true
	if _, err := svc.Update(context.Background(), tenantID, current); err == nil {
		t.Fatal("want an error: no channel is configured")
	}

	// Discriminating case: email is deliverable, but SMS specifically is
	// not. A guard that reused CanDeliver() (the existing
	// RegistrationVerification check) would wrongly accept this -- it only
	// asks "can it send anything," not "can it send SMS."
	svc.WithDeliveryChannels(func() []model.RecoveryChannel { return []model.RecoveryChannel{model.RecoveryEmail} })
	if _, err := svc.Update(context.Background(), tenantID, current); err == nil {
		t.Fatal("want an error: email is configured but SMS is not")
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
