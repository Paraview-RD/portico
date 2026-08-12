package service

import (
	"context"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/model"
)

// The catalogue promises that a field never received means the account has no
// value for it. That promise only holds if every key the catalogue offers can
// actually be resolved — a key with no resolver is a field somebody maps, never
// receives, and reasonably concludes is empty for everyone.
//
// `tenant_code` was exactly that: offered, mappable, and never filled in.

// fullAccount has something in every place FieldValues reads from the account
// itself. The organization keys are separate, below.
func fullAccount() model.User {
	return model.User{
		ID: "user-1", Username: "ada", DisplayName: "Ada Lovelace",
		Email: "ada@example.org", Phone: "+442079460100",
		Role: model.RoleUser, Status: model.StatusActive,
		ExternalID: "ext-1", UpdatedAt: time.Now(),
		Profile: model.UserProfile{
			NameFormatted: "Ada Lovelace", FamilyName: "Lovelace", GivenName: "Ada",
			MiddleName: "Augusta", HonorificPrefix: "Ms", HonorificSuffix: "FRS",
			NickName: "Ada", ProfileURL: "https://example.org/ada",
			PhotoURL: "https://example.org/ada.png", Title: "Engineer",
			UserType: "Employee", PreferredLanguage: "en-GB", Locale: "en-GB",
			Timezone: "Europe/London", AddressFormatted: "12 Analytical Way",
			StreetAddress: "12 Analytical Way", Locality: "London",
			Region: "Greater London", PostalCode: "SW1A 1AA", Country: "GB",
			EmployeeNumber: "E-1", CostCenter: "CC-1", Department: "Analytical Engines",
			ManagerID: "user-2", ManagerName: "Charles Babbage",
		},
	}
}

// Every built-in field the catalogue offers can be resolved.
//
// The organization keys need a tree and are excluded here rather than left to
// fail: an account belonging to nothing genuinely has no organization code,
// which is the absence this feature is supposed to express.
func TestEveryBuiltInFieldCanBeResolved(t *testing.T) {
	f := newSyncFixture(t)
	catalogue := NewFieldCatalogue(f.store)

	values, err := catalogue.FieldValues(context.Background(), f.tenantID, fullAccount())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	needsAnOrganization := map[string]bool{
		"organization_id": true, "organization_name": true,
		"organization_code": true, "organization_parent_code": true,
		"organization_path": true, "organization_manager_name": true,
	}

	for _, field := range BuiltInFields() {
		if needsAnOrganization[field.Key] {
			continue
		}
		if _, resolved := values[field.Key]; !resolved {
			t.Errorf("%s is in the catalogue and FieldValues never produces a value "+
				"for it. Anybody who maps it will never receive it, and this "+
				"feature tells them that means the account has no value — so the "+
				"field is not merely useless, it lies.", field.Key)
		}
	}
}

// The tenant's own two do not depend on the account at all, so they are
// present even for one with nothing filled in. This is the narrow case the
// bug above actually shipped as.
func TestTheTenantsOwnFieldsAreAlwaysResolved(t *testing.T) {
	f := newSyncFixture(t)
	catalogue := NewFieldCatalogue(f.store)

	values, err := catalogue.FieldValues(context.Background(), f.tenantID,
		model.User{ID: "user-1", Username: "someone"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if values["tenant_id"] != f.tenantID {
		t.Errorf("tenant_id is %q, want %q", values["tenant_id"], f.tenantID)
	}
	if values["tenant_code"] == "" {
		t.Error("tenant_code is empty, so a mapping for it would send nothing — " +
			"and an integrator would read that as the tenant having no code")
	}
}
