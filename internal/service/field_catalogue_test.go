package service

import (
	"context"
	"strings"
	"testing"
)

// The catalogue is a security boundary as much as a vocabulary, so the tests
// here are about its shape rather than about any mapping that uses it. A
// mapping can only be as safe as the list of things it may name.

// Every field that a directory may not write says why.
//
// This is the test that keeps the boundary legible. Four of the outbound-only
// entries are refusals with teeth — role, status, the two verification flags —
// and the rest are merely facts a directory has no business asserting. A
// reader deciding whether to relax one has to be able to tell those apart, and
// an entry with no reason is one where somebody will guess.
func TestEveryOutboundOnlyFieldSaysWhy(t *testing.T) {
	for _, f := range BuiltInFields() {
		if f.Inbound {
			if f.OutboundOnlyBecause != "" {
				t.Errorf("%s allows inbound mapping and also carries a reason it "+
					"does not; one of the two is stale", f.Key)
			}
			continue
		}
		if strings.TrimSpace(f.OutboundOnlyBecause) == "" {
			t.Errorf("%s cannot be written by a directory and does not say why. "+
				"Some of these are privilege boundaries and some are just facts "+
				"Portico owns — without the reason, nobody can tell which this is.",
				f.Key)
		}
	}
}

// The four that must never be writable from a directory.
//
// Named individually rather than counted, because "the list has four
// outbound-only security entries" is a fact about today and "a directory may
// not set a role" is a fact about the system. If somebody makes role inbound,
// this fails with the sentence explaining what that would mean.
func TestADirectoryCannotBeGivenControlOfPrivilege(t *testing.T) {
	forbidden := map[string]string{
		"role":           "a directory attribute granting administrator is privilege escalation in a system Portico does not own",
		"status":         "an entry disappearing is already how a directory deactivates; a second channel would fight with it",
		"email_verified": "it records a check Portico made, and a directory asserting it is not that check",
		"phone_verified": "the same",
		"user_id":        "a directory that could set it could take over an existing account",
		"tenant_id":      "no account may change its own tenant",
		"tenant_code":    "the same",
	}

	for _, f := range BuiltInFields() {
		if why, listed := forbidden[f.Key]; listed && f.Inbound {
			t.Errorf("%s is mappable inbound, which it must not be: %s", f.Key, why)
		}
	}

	// And the reverse, so the list above cannot rot into naming fields that no
	// longer exist — a guard nobody can trip is worse than none.
	present := map[string]bool{}
	for _, f := range BuiltInFields() {
		present[f.Key] = true
	}
	for key := range forbidden {
		if !present[key] {
			t.Errorf("this test forbids inbound mapping of %q, which is no longer "+
				"in the catalogue; the guard is guarding nothing", key)
		}
	}
}

// Keys are unique, which the mapping storage depends on: a mapping stores a
// key, so two entries under one key would make it ambiguous — and the ambiguity
// would be resolved by whichever happened to be found first.
func TestFieldKeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range BuiltInFields() {
		if seen[f.Key] {
			t.Errorf("%s appears twice in the catalogue", f.Key)
		}
		seen[f.Key] = true
	}
}

// Every entry has a kind the five-kind vocabulary knows.
//
// A kind is what a form draws and what validation refuses a bad value with, so
// an entry with a kind nothing handles is an entry that renders as nothing and
// validates against nothing.
func TestEveryFieldHasAKnownKind(t *testing.T) {
	known := map[string]bool{
		FieldKindText: true, FieldKindNumber: true, FieldKindBoolean: true,
		FieldKindDate: true, FieldKindSelect: true,
	}
	groups := map[string]bool{
		FieldGroupIdentity: true, FieldGroupProfile: true,
		FieldGroupOrganization: true, FieldGroupTenant: true, FieldGroupCustom: true,
	}

	for _, f := range BuiltInFields() {
		if !known[f.Kind] {
			t.Errorf("%s has kind %q, which no form can draw", f.Key, f.Kind)
		}
		if !groups[f.Group] {
			t.Errorf("%s is in group %q, which the picker does not have a section for",
				f.Key, f.Group)
		}
		// A SELECT with nothing to select from is a control with no options.
		if f.Kind == FieldKindSelect && len(f.AllowedValues) == 0 {
			t.Errorf("%s is a SELECT with no allowed values", f.Key)
		}
	}
}

// A returned catalogue cannot be sorted out from under the next caller.
//
// BuiltInFields hands out a copy for exactly this reason: the list is drawn as
// a picker in a fixed order, and a caller that sorted the shared slice in place
// would reorder it for everybody, in a way that looks like a UI bug and is not.
func TestTheBuiltInListIsHandedOutAsACopy(t *testing.T) {
	first := BuiltInFields()
	if len(first) == 0 {
		t.Fatal("the catalogue is empty")
	}
	original := first[0].Key
	first[0] = Field{Key: "tampered"}

	if again := BuiltInFields(); again[0].Key != original {
		t.Errorf("mutating the returned slice changed the catalogue: first key is "+
			"now %q, was %q", again[0].Key, original)
	}
}

// A tenant may not define an attribute under a built-in key.
func TestABuiltInKeyIsRecognisedAsTaken(t *testing.T) {
	for _, key := range []string{"department", "role", "tenant_code"} {
		if !IsBuiltInFieldKey(key) {
			t.Errorf("%q is in the built-in catalogue and is not reported as taken; "+
				"a tenant could define a second field under it and the mapping "+
				"would then be ambiguous", key)
		}
	}
	if IsBuiltInFieldKey("badge_number") {
		t.Error("badge_number is not a built-in and is reported as taken")
	}
}

// The catalogue is reachable without a tenant's definitions failing the whole
// list. A tenant that has defined nothing gets the built-in half.
func TestATenantWithNoDefinitionsGetsTheBuiltInHalf(t *testing.T) {
	f := newSyncFixture(t)
	catalogue := NewFieldCatalogue(f.store)

	fields, err := catalogue.Fields(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("read catalogue: %v", err)
	}
	if len(fields) != len(BuiltInFields()) {
		t.Errorf("a tenant with no definitions has %d fields, want the %d built-in ones",
			len(fields), len(BuiltInFields()))
	}

	if _, err := catalogue.Field(context.Background(), f.tenantID, "department"); err != nil {
		t.Errorf("looking up a built-in field: %v", err)
	}
	if _, err := catalogue.Field(context.Background(), f.tenantID, "no_such_field"); err == nil {
		t.Error("looking up a field nobody defined succeeded")
	}
}
