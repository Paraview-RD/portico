package service

// Coverage for the transactional half of UserService.Create — the part that
// changed to let an invitation code pre-assign group membership in the same
// transaction as the account it belongs to (see
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md).

import (
	"context"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// newUserTestFixture opens a throwaway database with one tenant, ready for
// UserService.Create.
func newUserTestFixture(t *testing.T) (*UserService, *store.Store, string) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-users"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "users", Name: "Users", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	audit := NewAuditService(st)
	settings := NewSettingsService(st, 0)
	users := NewUserService(st, audit, settings,
		auth.NewTokenService([]byte("0123456789abcdef0123456789abcdef")), metrics.New())
	return users, st, tenantID
}

func createTestGroup(t *testing.T, st *store.Store, tenantID, id, name string) {
	t.Helper()
	err := st.Queries.CreateGroup(context.Background(), sqlcgen.CreateGroupParams{
		ID: id, TenantID: tenantID, DisplayName: name, Source: "ADMIN",
		CreatedAt: store.Now(),
	})
	if err != nil {
		t.Fatalf("create group %s: %v", id, err)
	}
}

func TestCreate_WithGroupIDs_WritesMembership(t *testing.T) {
	users, st, tenantID := newUserTestFixture(t)
	ctx := context.Background()
	createTestGroup(t, st, tenantID, "group-1", "Beta Testers")

	created, err := users.Create(ctx, tenantID, CreateUserInput{
		Username: "newbie", DisplayName: "New Bie", Password: "Str0ng!Passw0rd",
		Role: model.RoleUser, Source: model.SourceRegistration, GroupIDs: []string{"group-1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	members, err := st.ForTenant(tenantID).ListGroupMembers(ctx, "group-1")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.UserID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("new user %s is not a member of group-1", created.ID)
	}
}

// The transaction is the whole point: a group that does not exist must not
// leave a user behind with no membership. A separate Create-then-AddMember
// call could not make this guarantee.
func TestCreate_WithUnknownGroupID_RollsBackUser(t *testing.T) {
	users, st, tenantID := newUserTestFixture(t)
	ctx := context.Background()

	_, err := users.Create(ctx, tenantID, CreateUserInput{
		Username: "newbie", DisplayName: "New Bie", Password: "Str0ng!Passw0rd",
		Role: model.RoleUser, Source: model.SourceRegistration, GroupIDs: []string{"no-such-group"},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown group id")
	}

	_, getErr := st.ForTenant(tenantID).GetUserByUsername(ctx, "newbie")
	if !store.IsNoRows(getErr) {
		t.Errorf("the user was created despite the unknown group: %v", getErr)
	}
}

func TestCreate_WithoutGroupIDs_Unaffected(t *testing.T) {
	users, _, tenantID := newUserTestFixture(t)
	ctx := context.Background()

	created, err := users.Create(ctx, tenantID, CreateUserInput{
		Username: "plain", DisplayName: "Plain User", Password: "Str0ng!Passw0rd",
		Role: model.RoleUser, Source: model.SourceAdmin,
	})
	if err != nil {
		t.Fatalf("create without group ids: %v", err)
	}
	if created.Username != "plain" {
		t.Errorf("username = %q, want plain", created.Username)
	}
}
