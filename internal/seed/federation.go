package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// The fourth way another system connects to this one, and the only one that
// runs inward: somebody else's OpenID Provider, trusted to say who a person
// is. The other three — a directory pushing accounts in over SCIM, Portico
// pulling out of LDAP, webhooks pushing events onward — are in
// integrations.go, and they all move accounts or events. This moves an
// assertion about identity, which is a different thing arriving from
// outside.
//
// Written through the store rather than through ExternalIDPService, and that
// is the one place this file departs from the rest of the seed.
//
// Create contacts the issuer before writing the row, deliberately: a
// configuration nobody can discover should fail at the form rather than at
// somebody's sign-in three days later. That is right for an administrator
// and wrong for a seed, which must fill a database on a laptop with no route
// to accounts.google.com — and would otherwise fail wholesale, on a machine
// on a train, because of a name lookup.
//
// What that costs is worth stating plainly, because it is what somebody
// looking at the seeded screens will meet: the buttons are real, the
// configuration is real, and pressing one gets as far as a redirect that
// Google refuses, because the client id below is not registered to anybody.
// The seed demonstrates that configuring a provider puts a button on the
// sign-in screen, and that an account can hold links. It does not
// demonstrate a completed round trip, and no seed can — that needs a client
// somebody registered at a real provider.

// seedFederation registers external identity providers and links a couple of
// accounts to one.
func (s *Seeder) seedFederation(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	q := s.store.ForTenant(t.tenant.ID)
	now := store.Now()

	providers := []struct {
		key         string
		name        string
		buttonLabel string
		issuer      string
		clientID    string
		trustEmail  bool
		disabled    bool
	}{
		{
			// The one on the sign-in screen. Google because it is the issuer
			// most people recognise, and because its discovery document is
			// the one an operator is most likely to have met.
			key: "google", name: "公司 Google", buttonLabel: "Google",
			issuer:   "https://accounts.google.com",
			clientID: "portico-demo.apps.googleusercontent.com",
			// Off, like every provider unless somebody decides otherwise.
			// The seed showing it on would be the seed making the one
			// decision here that can hand an account to a stranger.
			trustEmail: false,
		},
		{
			// Disabled, and that is the point of it. A tenant with one
			// provider cannot show that the sign-in screen lists the active
			// ones rather than all of them — with two, the administrative
			// list has two rows and the sign-in screen has one button, and
			// the difference between the screens is visible rather than
			// asserted.
			key: "entra", name: "合作方 Entra", buttonLabel: "",
			issuer:   "https://login.microsoftonline.com/common/v2.0",
			clientID: "0000-partner-directory-0000",
			// On, so the console shows both states of the setting that
			// decides whether an address may reach an existing account —
			// including the badge that calls it out.
			trustEmail: true,
			disabled:   true,
		},
	}

	seeded := map[string]string{}
	for _, p := range providers {
		id := uuid.NewString()
		err := q.CreateExternalIdentityProvider(ctx, sqlcgen.CreateExternalIdentityProviderParams{
			ID: id, TenantID: t.tenant.ID,
			Name: p.name, ButtonLabel: p.buttonLabel,
			Issuer: p.issuer, ClientID: p.clientID,
			// Empty: a secret would have to be sealed, and the seed has
			// nothing true to put there anyway. An empty one is a public
			// client, which is a state the console has a sentence for.
			ClientSecret:       "",
			Scopes:             "openid profile email",
			TrustVerifiedEmail: p.trustEmail,
			CreatedAt:          now,
		})
		if err != nil {
			return fmt.Errorf("seed identity provider %s: %w", p.name, err)
		}
		if p.disabled {
			if err := q.SetExternalIdentityProviderStatus(ctx, id,
				string(model.StatusDisabled), now); err != nil {
				return fmt.Errorf("disable identity provider %s: %w", p.name, err)
			}
		}
		seeded[p.key] = id
		w.summary.IdentityProviders++
	}

	return s.seedExternalIdentities(ctx, w, t, seeded["google"])
}

// Two accounts that have already linked the provider.
//
// Without these, the profile screen's "other ways to sign in" is an empty
// section for every seeded account, and removing a provider warns about
// links that do not exist. Two rather than one, because the count is what
// the removal warning is about.
//
// Not the administrator. An administrator who signs in through a provider
// nobody can reach is the seed handing somebody a locked door — and the
// account the demo tells people to use is the one that must always open
// with a password.
func (s *Seeder) seedExternalIdentities(ctx context.Context, w *world, t *seededTenant, providerID string) error {
	if providerID == "" {
		return nil
	}

	q := s.store.ForTenant(t.tenant.ID)
	now := store.Now()

	linked := 0
	for _, user := range t.users {
		if user.Role == "SUPER_ADMIN" || user.Email == "" {
			continue
		}
		if linked >= 2 {
			break
		}

		err := q.CreateExternalIdentity(ctx, sqlcgen.CreateExternalIdentityParams{
			ID: uuid.NewString(), TenantID: t.tenant.ID,
			ProviderID: providerID, UserID: user.ID,
			// A subject that looks like what Google issues — a long decimal
			// string — rather than the username. The pair (issuer, subject)
			// is the identity, and a seed that put a username there would
			// suggest the address or the name is what finds an account.
			Subject:   fmt.Sprintf("1075%013d", linked+1),
			Email:     user.Email,
			CreatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("link %s to the seeded provider: %w", user.Username, err)
		}
		linked++
		w.summary.ExternalIdentities++
	}
	return nil
}
