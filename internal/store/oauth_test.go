package store_test

import (
	"context"
	"testing"

	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// A nil slice is encoded by the driver as SQL NULL rather than as an empty
// array, so a column declared NOT NULL DEFAULT '{}' rejects it — and the
// failure names a column the caller never mentioned, which reads as a schema
// bug rather than an unset optional field. Registering a client with no
// post-logout URIs is the ordinary case, and it hit exactly that.
func TestArrayColumnsRejectNilAndAcceptEmpty(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	base := sqlcgen.CreateOAuthClientParams{
		Name:            "App",
		ApplicationType: "WEB",
		AuthMethod:      "none",
		RedirectUris:    []string{"https://app.example.com/cb"},
		GrantTypes:      []string{"authorization_code"},
		Scopes:          []string{"openid"},
		Status:          "ACTIVE",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	withNil := base
	withNil.ID, withNil.ClientID = "client-nil", "nil-uris"
	withNil.PostLogoutRedirectUris = nil
	if err := q.CreateOAuthClient(ctx, withNil); err == nil {
		t.Error("a nil array was accepted; the service must not rely on that")
	}

	withEmpty := base
	withEmpty.ID, withEmpty.ClientID = "client-empty", "empty-uris"
	withEmpty.PostLogoutRedirectUris = []string{}
	if err := q.CreateOAuthClient(ctx, withEmpty); err != nil {
		t.Fatalf("an empty array was rejected: %v", err)
	}

	got, err := q.GetOAuthClient(ctx, "empty-uris")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got.PostLogoutRedirectUris) != 0 {
		t.Errorf("post-logout URIs = %v, want none", got.PostLogoutRedirectUris)
	}
	if len(got.RedirectUris) != 1 {
		t.Errorf("redirect URIs = %v, want one", got.RedirectUris)
	}
}

// Client ids are unique per tenant, like every other identifier here: two
// tenants each registering a client called "web-app" is the expected case.
func TestClientIDsAreUniquePerTenantOnly(t *testing.T) {
	s := newTestStore(t)
	acme := newTestTenant(t, s, "acme")
	beta := newTestTenant(t, s, "beta")
	ctx := context.Background()
	now := store.Now()

	client := func(id string) sqlcgen.CreateOAuthClientParams {
		return sqlcgen.CreateOAuthClientParams{
			ID: id, ClientID: "web-app", Name: "Web App",
			ApplicationType: "WEB", AuthMethod: "none",
			RedirectUris:           []string{"https://app.example.com/cb"},
			PostLogoutRedirectUris: []string{},
			GrantTypes:             []string{"authorization_code"},
			Scopes:                 []string{"openid"},
			Status:                 "ACTIVE", CreatedAt: now, UpdatedAt: now,
		}
	}

	if err := acme.CreateOAuthClient(ctx, client("a")); err != nil {
		t.Fatalf("first tenant: %v", err)
	}
	if err := beta.CreateOAuthClient(ctx, client("b")); err != nil {
		t.Fatalf("second tenant could not reuse the client id: %v", err)
	}

	err := acme.CreateOAuthClient(ctx, client("c"))
	if !store.IsUniqueViolation(err) {
		t.Errorf("a duplicate within one tenant gave %v, want a unique violation", err)
	}

	// And neither tenant can read the other's.
	if _, err := acme.GetOAuthClient(ctx, "web-app"); err != nil {
		t.Errorf("tenant cannot read its own client: %v", err)
	}
	clients, err := acme.ListOAuthClients(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(clients) != 1 {
		t.Errorf("tenant sees %d clients, want 1", len(clients))
	}
}

// A refresh token chain: presenting a spent token means a copy leaked, and
// the response is to revoke everything descended from it rather than fail
// the one call. The recursive query is the part worth testing — it is the
// only place in the schema where a row references its own table.
func TestRefreshTokenChainRevocation(t *testing.T) {
	s := newTestStore(t)
	q := newTestTenant(t, s, "acme")
	ctx := context.Background()
	now := store.Now()

	if err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: "user-1", Username: "alice", DisplayName: "Alice",
		PasswordHash: "hash", Role: "USER", Status: "ACTIVE",
		TokenVersion: 1, Source: "ADMIN", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Three generations, each replacing the one before.
	ids := []string{"rt-1", "rt-2", "rt-3"}
	for i, id := range ids {
		err := q.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
			ID: id, ClientID: "web-app", Subject: "user-1",
			TokenHash: "hash-" + id,
			Scopes:    []string{"openid"}, Audience: []string{}, Amr: []string{"pwd"},
			AuthTime: now, CreatedAt: now, ExpiresAt: now.Add(720 * 3600e9),
		})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if i > 0 {
			if err := q.SpendRefreshToken(ctx, ids[i-1], id, now); err != nil {
				t.Fatalf("spend %s: %v", ids[i-1], err)
			}
		}
	}

	// Revoking from the head takes the whole chain with it.
	if err := q.RevokeRefreshTokenChain(ctx, "rt-1", now); err != nil {
		t.Fatalf("revoke chain: %v", err)
	}

	for _, id := range ids {
		row, err := q.GetRefreshToken(ctx, "hash-"+id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if row.RevokedAt == nil {
			t.Errorf("%s survived the chain revocation", id)
		}
	}
}
