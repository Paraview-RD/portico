package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/model"
)

// The overlay is pure, so these tests are too. What they are protecting is the
// promise the whole feature rests on — that a payload nobody has configured is
// the payload that was always sent — and the three ways a rule can change one.

// rules builds an Outbound the way the store would have.
func rules(mappings ...model.FieldMapping) Outbound {
	byKey := map[string]model.FieldMapping{}
	for _, m := range mappings {
		byKey[m.SourceKey] = m
	}
	return Outbound{byKey: byKey}
}

// userBody is a payload in the shape model.User marshals to.
func userBody() map[string]any {
	return map[string]any{
		"id":          "user-1",
		"username":    "ada",
		"displayName": "Ada Lovelace",
		"email":       "ada@example.org",
		"source":      "LOCAL",
		"createdAt":   "2026-01-01T00:00:00Z",
		"profile": map[string]any{
			"department": "Analytical Engines",
			"title":      "Engineer",
		},
	}
}

// A subscription that configured nothing gets what it always got.
//
// The publish path short-circuits before this function for an empty rule set,
// so this is the belt to that braces: even reaching the overlay with nothing
// configured must not disturb the body.
func TestARuleSetWithNothingInItChangesNothing(t *testing.T) {
	body := userBody()
	before, _ := json.Marshal(body)

	after, _ := json.Marshal(applyToPayload(body, subjectUser, rules(), nil))

	if string(before) != string(after) {
		t.Errorf("an empty rule set changed the payload\n before: %s\n  after: %s", before, after)
	}
}

// Renaming a nested field lifts it to the top level and takes it out of the
// object it came from.
//
// A mapping target is one name, so this is the only thing it can mean — and a
// value left behind in `profile` as well would be the fact sent twice under two
// names, which is what an integrator is renaming it to avoid.
func TestRenamingANestedFieldLiftsItAndRemovesTheOriginal(t *testing.T) {
	body := applyToPayload(userBody(), subjectUser,
		rules(model.FieldMapping{SourceKey: "department", TargetName: "dept"}), nil)

	if body["dept"] != "Analytical Engines" {
		t.Errorf("dept is %v, want the department value at the top level", body["dept"])
	}
	profile, _ := body["profile"].(map[string]any)
	if _, still := profile["department"]; still {
		t.Error("the value is still in profile as well, so it is now sent twice under two names")
	}
	if profile["title"] != "Engineer" {
		t.Error("lifting one member disturbed the rest of profile")
	}
}

// An object emptied by lifting its last member is removed rather than sent
// as {}. An object that appears and disappears depending on configuration is
// harder to consume than one that is consistently either there or not.
func TestLiftingTheLastProfileMemberRemovesTheEmptyObject(t *testing.T) {
	body := map[string]any{
		"id":      "user-1",
		"profile": map[string]any{"department": "Analytical Engines"},
	}
	body = applyToPayload(body, subjectUser,
		rules(model.FieldMapping{SourceKey: "department", TargetName: "dept"}), nil)

	if _, present := body["profile"]; present {
		t.Errorf("profile is still present and empty: %v", body["profile"])
	}
}

// Suppression removes in place rather than renaming to nothing.
func TestSuppressionRemovesTheFieldAndAddsNothing(t *testing.T) {
	body := applyToPayload(userBody(), subjectUser,
		rules(model.FieldMapping{SourceKey: "email", Suppressed: true}), nil)

	if _, still := body["email"]; still {
		t.Error("a suppressed field is still in the payload")
	}
	if body["username"] != "ada" {
		t.Error("suppressing one field disturbed another")
	}
}

// An addition is a fact the payload never carried, and it lands at the top
// level like every other target.
func TestAnAdditionLandsAtTheTopLevel(t *testing.T) {
	values := map[string]string{"organization_path": "acme/eng/platform"}
	body := applyToPayload(userBody(), subjectUser,
		rules(model.FieldMapping{SourceKey: "organization_path", TargetName: "orgPath"}), values)

	if body["orgPath"] != "acme/eng/platform" {
		t.Errorf("orgPath is %v, want the resolved path", body["orgPath"])
	}
}

// Nothing is ever sent empty.
//
// A subscriber that mapped a field and never receives it should conclude that
// the account has no value for it — which is only a safe conclusion if an
// absent value is never rendered as an empty one.
func TestAnAdditionWithNoValueSendsNothing(t *testing.T) {
	body := applyToPayload(userBody(), subjectUser,
		rules(model.FieldMapping{SourceKey: "organization_path", TargetName: "orgPath"}),
		map[string]string{})

	if _, present := body["orgPath"]; present {
		t.Errorf("a field with no value was sent as %#v", body["orgPath"])
	}
}

// A rule that names where the fact already is changes nothing at all.
//
// This is `organization_code → code` on an organization event: an ordinary
// thing to configure, and a no-op. It must not delete and re-add the key,
// because that is how a value ends up moving to the end of an object or
// vanishing when the read fails.
func TestAMappingToItsOwnDefaultNameIsANoOp(t *testing.T) {
	body := map[string]any{"id": "org-1", "code": "eng", "name": "Engineering"}
	before, _ := json.Marshal(body)

	after, _ := json.Marshal(applyToPayload(body, subjectOrganization,
		rules(model.FieldMapping{SourceKey: "organization_code", TargetName: "code"}), nil))

	if string(before) != string(after) {
		t.Errorf("a mapping to its own name changed the payload\n before: %s\n  after: %s", before, after)
	}
}

// A field the payload does not carry is not conjured by a rename.
//
// `phone` is absent from this body because the account has none. A rename must
// leave it absent rather than adding the key with a nil value, which a
// subscriber would read as "the phone number was cleared".
func TestRenamingAnAbsentFieldDoesNotInventIt(t *testing.T) {
	body := applyToPayload(userBody(), subjectUser,
		rules(model.FieldMapping{SourceKey: "phone", TargetName: "mobile"}), nil)

	if value, present := body["mobile"]; present {
		t.Errorf("renaming an absent field produced mobile=%#v", value)
	}
}

// Group events carry a payload with no vocabulary, so they are delivered as
// they always were. Stated here as well as in the documentation, because a
// subscription that has configured mappings and sees an unchanged group body
// needs this to be a decision rather than an oversight.
func TestGroupEventsAreNotSubjectToMappings(t *testing.T) {
	for _, eventType := range []string{
		"group.created", "group.updated", "group.deleted", "group.members_changed",
	} {
		if subject := subjectOf(eventType); subject != subjectNone {
			t.Errorf("%s resolves to subject %q, want none", eventType, subject)
		}
	}

	for eventType, want := range map[string]payloadSubject{
		"user.created":          subjectUser,
		"user.password_changed": subjectUser,
		"organization.updated":  subjectOrganization,
		"organization.disabled": subjectOrganization,
	} {
		if subject := subjectOf(eventType); subject != want {
			t.Errorf("%s resolves to subject %q, want %q", eventType, subject, want)
		}
	}
}

// A name the payload already uses is owned, and by every key that legitimately
// owns it.
//
// `id` is `user_id` in a user event and `organization_id` in an organization
// event; both are mappings to themselves and both must be allowed, while
// anything else landing on `id` would put a value where every subscriber reads
// the identifier.
func TestThePayloadsOwnNamesAreClaimed(t *testing.T) {
	owners, claimed := webhookTopLevelOwners["id"]
	if !claimed {
		t.Fatal("`id` is not claimed, so a mapping could overwrite the identifier")
	}
	if !owners["user_id"] || !owners["organization_id"] {
		t.Errorf("`id` is owned by %v, want both user_id and organization_id — "+
			"otherwise one of the two subjects cannot map a field to where it "+
			"already lives", owners)
	}
	if owners["department"] {
		t.Error("department owns `id`, which would let a department be sent as the identifier")
	}

	// Nested locations cannot collide, because a mapping can only write at the
	// top level. Claiming one would refuse mappings for no reason.
	if _, claimed := webhookTopLevelOwners["profile.department"]; claimed {
		t.Error("a nested location is claimed as a top-level name")
	}
}

// Every location the tables name is a key of the catalogue.
//
// The two are written by hand and drift apart silently: a location under a key
// nobody can map is dead, and worse, a catalogue key whose location is stale
// means a rename that quietly adds a second copy of a fact instead of moving
// it. This catches the first; TestEveryDefaultLocationMatchesTheStruct would be
// the second, and belongs with the payload types.
func TestEveryDefaultLocationNamesACatalogueKey(t *testing.T) {
	known := map[string]bool{}
	for _, f := range BuiltInFields() {
		known[f.Key] = true
	}

	for name, table := range map[string]map[string]string{
		"user": webhookUserDefaults, "organization": webhookOrganizationDefaults,
	} {
		for key := range table {
			if !known[key] {
				t.Errorf("the %s payload table maps %q, which is not in the catalogue; "+
					"nobody can configure it and the entry does nothing", name, key)
			}
		}
	}
}

// The user table covers the profile block completely.
//
// Twenty-five attributes arrive over SCIM and sit in `profile`, and the point
// of this feature for most integrations is getting them out under the name a
// downstream system expects. One missing from the table is one that silently
// cannot be renamed — it would be treated as an addition and sent twice.
func TestEveryProfileFieldHasADefaultLocation(t *testing.T) {
	var missing []string
	for _, f := range BuiltInFields() {
		if f.Group != FieldGroupProfile {
			continue
		}
		if _, mapped := webhookUserDefaults[f.Key]; !mapped {
			missing = append(missing, f.Key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("profile fields with no default location in the user payload: %v.\n"+
			"Each would be treated as an addition, so renaming it would send the "+
			"fact twice — once in profile and once at the top level.", missing)
	}
}

// Every name the payloads actually carry is accounted for.
//
// This is the guard on the guard. `webhookTopLevelOwners` refuses a rename that
// would land on a name the payload already uses — but it is built from the
// location tables, so a name the payload carries and the tables have never
// heard of is a name the refusal does not know to protect. Add a field to
// `model.User` or `model.Organization` and, without this, a mapping could
// quietly overwrite it.
//
// Each key is therefore either mappable, or listed below with the reason it is
// not. The list is the point: "nobody maps it" has to be a decision somebody
// wrote down, because the alternative is that it is an oversight nobody can
// distinguish from one.
func TestEveryPayloadNameIsEitherMappableOrKnowinglyNot(t *testing.T) {
	notMappable := map[string]string{
		// Facts about the row rather than about the person or the place.
		"createdAt": "when the row appeared, which only this database knows",
		"source":    "which system created the account, not an attribute of it",
		// Containers and collections. A mapping target is one name holding one
		// value, so an object or a list has nothing to be renamed to.
		"profile":     "the container; its members are mapped individually",
		"attachments": "a list of organizations, not a single fact",
		// Lifecycle timestamps, which mean something only alongside status.
		"closedAt":    "meaningful only with status, and status is mapped",
		"lockedUntil": "the same",
		// Organization facts nobody maps out.
		"remark":    "free text for administrators, not an integration field",
		"parentId":  "an internal id; organization_parent_code is the mappable form",
		"sortOrder": "how the console orders the tree",
		"managerId": "an internal id; organization_manager_name is the mappable form",
		"userCount": "computed for the console, and stale the moment it is sent",
	}

	check := func(name string, payload any, table map[string]string) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}

		mapped := map[string]bool{}
		for _, location := range table {
			if !strings.Contains(location, ".") {
				mapped[location] = true
			}
		}

		for key := range body {
			if mapped[key] || notMappable[key] != "" {
				continue
			}
			t.Errorf("the %s payload carries %q, which is neither in its location "+
				"table nor listed as knowingly unmappable. A rename could land on "+
				"it and overwrite it, because the collision guard is built from "+
				"that table.", name, key)
		}
	}

	// Zero values: every field without omitempty, which is every field a
	// subscriber can rely on being present.
	check("user", model.User{}, webhookUserDefaults)
	check("organization", model.Organization{}, webhookOrganizationDefaults)
}

// Two subscriptions to one event cannot see each other's renames.
//
// The overlay decodes a fresh tree per subscription for exactly this reason. A
// shared one would mean the first subscriber's rename removed the field before
// the second was assembled — and the second would then silently receive a
// payload missing a field it never configured anything about.
func TestOneSubscriptionsRenameDoesNotReachAnother(t *testing.T) {
	raw, err := json.Marshal(userBody())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decode := func() map[string]any {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return body
	}

	applyToPayload(decode(), subjectUser,
		rules(model.FieldMapping{SourceKey: "department", TargetName: "dept"}), nil)

	second := applyToPayload(decode(), subjectUser, rules(), nil)
	if !reflect.DeepEqual(second, userBody()) {
		t.Errorf("the second subscription received %#v, want the untouched payload", second)
	}
}
