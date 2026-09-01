package service

// Coverage for invitation-gated registration: the administrative surface
// (InvitationService) and the redemption path it feeds
// (UserService.Register). See
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

type invitationFixture struct {
	t           *testing.T
	invitations *InvitationService
	users       *UserService
	settings    *SettingsService
	store       *store.Store
	tenantID    string
	actor       auth.Principal
}

func newInvitationFixture(t *testing.T) *invitationFixture {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-inv"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "inv", Name: "Invitations", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	audit := NewAuditService(st)
	settings := NewSettingsService(st, 0)
	users := NewUserService(st, audit, settings,
		auth.NewTokenService([]byte("0123456789abcdef0123456789abcdef")), metrics.New())
	invitations := NewInvitationService(st, audit)

	// Registration is off by default; every test here needs it on, with
	// verification off so Register does not also try to send a message.
	current, err := settings.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	current.RegistrationEnabled = true
	if _, err := settings.Update(ctx, tenantID, current); err != nil {
		t.Fatalf("enable registration: %v", err)
	}

	return &invitationFixture{
		t: t, invitations: invitations, users: users, settings: settings,
		store: st, tenantID: tenantID,
		actor: auth.Principal{TenantID: tenantID, UserID: "admin-id", Username: "admin", Role: model.RoleSuperAdmin},
	}
}

func (f *invitationFixture) setInvitationOnly(only bool) {
	f.t.Helper()
	ctx := context.Background()
	current, err := f.settings.Get(ctx, f.tenantID)
	if err != nil {
		f.t.Fatalf("get settings: %v", err)
	}
	current.InvitationOnlyRegistration = only
	if _, err := f.settings.Update(ctx, f.tenantID, current); err != nil {
		f.t.Fatalf("set invitation-only: %v", err)
	}
}

func TestInvitationCreate_RequiresPositiveQuota(t *testing.T) {
	f := newInvitationFixture(t)
	_, err := f.invitations.Create(context.Background(), f.actor, CreateInvitationInput{Code: "X", Quota: 0})
	if err == nil {
		t.Fatal("expected an error for a zero quota")
	}
}

func TestInvitationCreate_ValidatesOrganization(t *testing.T) {
	f := newInvitationFixture(t)
	_, err := f.invitations.Create(context.Background(), f.actor,
		CreateInvitationInput{Code: "X", Quota: 1, OrganizationID: "no-such-org"})
	if !errors.Is(err, ErrOrganizationNotFound) {
		t.Errorf("err = %v, want ErrOrganizationNotFound", err)
	}
}

func TestInvitationCreate_ValidatesGroups(t *testing.T) {
	f := newInvitationFixture(t)
	_, err := f.invitations.Create(context.Background(), f.actor,
		CreateInvitationInput{Code: "X", Quota: 1, GroupIDs: []string{"no-such-group"}})
	if err == nil {
		t.Fatal("expected an error for an unknown group")
	}
}

func TestInvitationCreate_DuplicateCodeConflicts(t *testing.T) {
	f := newInvitationFixture(t)
	ctx := context.Background()
	if _, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{Code: "DUP", Quota: 1}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{Code: "DUP", Quota: 1})
	if err == nil {
		t.Fatal("expected a conflict on the duplicate code")
	}
}

func TestInvitationDisable_IsTerminal(t *testing.T) {
	f := newInvitationFixture(t)
	ctx := context.Background()
	created, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{Code: "ONCE", Quota: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	disabled, err := f.invitations.Disable(ctx, f.actor, created.ID)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Status != "DISABLED" {
		t.Errorf("status = %q, want DISABLED", disabled.Status)
	}

	// There is deliberately no Enable method — a disabled code stays
	// disabled. Confirm the only mutation available (Disable again) does
	// not do anything surprising like flipping it back.
	disabledAgain, err := f.invitations.Disable(ctx, f.actor, created.ID)
	if err != nil {
		t.Fatalf("disable again: %v", err)
	}
	if disabledAgain.Status != "DISABLED" {
		t.Errorf("status after re-disable = %q, want DISABLED", disabledAgain.Status)
	}
}

func TestRegister_InvitationRequired_RejectsWithoutCode(t *testing.T) {
	f := newInvitationFixture(t)
	f.setInvitationOnly(true)

	_, err := f.users.Register(context.Background(), f.tenantID, RegisterInput{
		Username: "nocode", DisplayName: "No Code", Password: "Str0ng!Passw0rd",
	}, "127.0.0.1")
	if !errors.Is(err, ErrInvitationRequired) {
		t.Errorf("err = %v, want ErrInvitationRequired", err)
	}
}

func TestRegister_InvitationCode_AssignsOrganizationAndGroups(t *testing.T) {
	f := newInvitationFixture(t)
	ctx := context.Background()

	if err := f.store.Queries.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
		ID: "org-1", TenantID: f.tenantID, Name: "Engineering", Code: "ENG",
		Status: "ACTIVE", CreatedAt: store.Now(), UpdatedAt: store.Now(),
	}); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	createTestGroup(t, f.store, f.tenantID, "group-1", "Beta Testers")

	_, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{
		Code: "ENG-CODE", Quota: 1, OrganizationID: "org-1", GroupIDs: []string{"group-1"},
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	user, err := f.users.Register(ctx, f.tenantID, RegisterInput{
		Username: "invitee", DisplayName: "Invitee", Password: "Str0ng!Passw0rd",
		InvitationCode: "ENG-CODE",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.OrganizationID != "org-1" {
		t.Errorf("organization = %q, want org-1", user.OrganizationID)
	}

	members, err := f.store.ForTenant(f.tenantID).ListGroupMembers(ctx, "group-1")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.UserID == user.ID {
			found = true
		}
	}
	if !found {
		t.Error("invitee was not added to group-1")
	}

	invitations, err := f.invitations.List(ctx, f.tenantID)
	if err != nil {
		t.Fatalf("list invitations: %v", err)
	}
	if len(invitations) != 1 || invitations[0].UsedCount != 1 {
		t.Errorf("invitations = %+v, want one with used_count 1", invitations)
	}
}

func TestRegister_WithWrongInvitationCode_Fails(t *testing.T) {
	f := newInvitationFixture(t)
	_, err := f.users.Register(context.Background(), f.tenantID, RegisterInput{
		Username: "ghost", DisplayName: "Ghost", Password: "Str0ng!Passw0rd",
		InvitationCode: "NO-SUCH-CODE",
	}, "127.0.0.1")
	if !errors.Is(err, ErrInvitationNotUsable) {
		t.Errorf("err = %v, want ErrInvitationNotUsable", err)
	}
}

func TestRegister_WithDisabledInvitationCode_Fails(t *testing.T) {
	f := newInvitationFixture(t)
	ctx := context.Background()
	created, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{Code: "DISABLED-ME", Quota: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.invitations.Disable(ctx, f.actor, created.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	_, err = f.users.Register(ctx, f.tenantID, RegisterInput{
		Username: "toolate", DisplayName: "Too Late", Password: "Str0ng!Passw0rd",
		InvitationCode: "DISABLED-ME",
	}, "127.0.0.1")
	if !errors.Is(err, ErrInvitationNotUsable) {
		t.Errorf("err = %v, want ErrInvitationNotUsable", err)
	}
}

// The whole reason RedeemInvitation checks and increments atomically: with
// quota=1 and many concurrent registrations sharing the same code, exactly
// one may create an account. This exercises it through the full public
// Register path, not just the raw SQL (see the store-level equivalent in
// internal/store/invitations_test.go).
func TestRegister_WithInvitation_ConcurrentRedemption_RespectsQuota(t *testing.T) {
	f := newInvitationFixture(t)
	ctx := context.Background()

	if _, err := f.invitations.Create(ctx, f.actor, CreateInvitationInput{Code: "RACE", Quota: 1}); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	const attempts = 10
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.users.Register(ctx, f.tenantID, RegisterInput{
				Username: fmt.Sprintf("racer%02d", i), DisplayName: "Racer", Password: "Str0ng!Passw0rd",
				InvitationCode: "RACE",
			}, "127.0.0.1")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInvitationNotUsable) {
			t.Errorf("unexpected error from a losing registration: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("%d of %d concurrent registrations succeeded, want exactly 1", successes, attempts)
	}

	invitations, err := f.invitations.List(ctx, f.tenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(invitations) != 1 || invitations[0].UsedCount != 1 {
		t.Errorf("invitations = %+v, want used_count 1", invitations)
	}
}
