package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/service"
)

// The attributes a tenant defines for itself.
//
// What is worth testing at this layer is the part that is a promise to whoever
// integrates: which keys may be used, what a value has to look like, and what
// happens to a value nobody sent. The catalogue's own shape is tested in
// internal/service, where the reasons live.

func defineAttribute(t *testing.T, api *apiTest, token string, body map[string]any) response {
	t.Helper()
	return api.do(http.MethodPost, "/api/v1/user-attributes", token, body)
}

// A definition round-trips, and its key is what everything else refers to.
func TestADefinedAttributeCanBeGivenAValue(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	created := defineAttribute(t, api, admin, map[string]any{
		"key": "badge_number", "label": "门禁卡号", "kind": "TEXT",
	})
	if created.Status != http.StatusOK {
		t.Fatalf("define: %d %s %s", created.Status, created.Code, created.Message)
	}

	userID := api.createUser(admin, "badge-holder", "badge-holder-password-1", "USER")

	set := api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin,
		map[string]string{"badge_number": "A-10293"})
	if set.Status != http.StatusOK {
		t.Fatalf("set value: %d %s %s", set.Status, set.Code, set.Message)
	}

	var values map[string]string
	api.do(http.MethodGet, "/api/v1/users/"+userID+"/attributes", admin, nil).into(t, &values)
	if values["badge_number"] != "A-10293" {
		t.Errorf("stored value = %q, want A-10293", values["badge_number"])
	}
}

// A key may not be one the built-in catalogue already holds.
//
// To the person typing it there is one namespace: a key that already names
// something cannot name a second thing, and whether the first is built in or a
// colleague's makes no difference to what they have to do. A tenant that could
// define `department` would make every mapping naming it ambiguous.
func TestAKeyTheBuiltInCatalogueHoldsIsRefused(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, key := range []string{"department", "role", "tenant_code", "email"} {
		res := defineAttribute(t, api, admin, map[string]any{
			"key": key, "label": "Clashes", "kind": "TEXT",
		})
		if res.Code != "USER_ATTRIBUTE_KEY_TAKEN" {
			t.Errorf("defining %q = %d %s, want USER_ATTRIBUTE_KEY_TAKEN — a mapping "+
				"naming that key would then be ambiguous", key, res.Status, res.Code)
		}
	}

	// And the tenant's own keys are equally taken, the second time.
	first := defineAttribute(t, api, admin, map[string]any{
		"key": "site_code", "label": "Site", "kind": "TEXT",
	})
	if first.Status != http.StatusOK {
		t.Fatalf("first definition: %d %s", first.Status, first.Code)
	}
	if again := defineAttribute(t, api, admin, map[string]any{
		"key": "site_code", "label": "Site again", "kind": "TEXT",
	}); again.Code != "USER_ATTRIBUTE_KEY_TAKEN" {
		t.Errorf("defining the same key twice = %d %s, want USER_ATTRIBUTE_KEY_TAKEN",
			again.Status, again.Code)
	}
}

// A key has to be usable as a claim name, an LDAP mapping target, and a
// sub-attribute name, so what it may contain is narrow.
func TestAnUnusableKeyIsRefused(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, key := range []string{"", "a", "Badge", "badge-number", "badge number", "1badge", "badge_"} {
		res := defineAttribute(t, api, admin, map[string]any{
			"key": key, "label": "Nope", "kind": "TEXT",
		})
		if res.Status == http.StatusOK {
			t.Errorf("key %q was accepted; it has to survive being a claim name "+
				"and an XML attribute name", key)
		}
	}
}

// A value is checked against its kind, and stored in one form.
//
// Canonical rather than as typed, because the value leaves in a token: a yes/no
// recorded as "Yes" by one administrator and "true" by another would reach an
// application as two different facts.
func TestAValueIsCheckedAndStoredCanonically(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "typed-values", "typed-values-password-1", "USER")

	for _, def := range []map[string]any{
		{"key": "head_count", "label": "Head count", "kind": "NUMBER"},
		{"key": "on_call", "label": "On call", "kind": "BOOLEAN"},
		{"key": "contract_ends", "label": "Contract ends", "kind": "DATE"},
		{"key": "work_mode", "label": "Work mode", "kind": "SELECT",
			"allowedValues": []string{"ONSITE", "HYBRID", "REMOTE"}},
	} {
		if res := defineAttribute(t, api, admin, def); res.Status != http.StatusOK {
			t.Fatalf("define %v: %d %s %s", def["key"], res.Status, res.Code, res.Message)
		}
	}

	for _, bad := range []map[string]string{
		{"head_count": "not a number"},
		{"on_call": "sometimes"},
		{"contract_ends": "31/12/2026"},
		{"contract_ends": "2026-12-31T09:00:00Z"},
		{"work_mode": "OFFSHORE"},
	} {
		res := api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin, bad)
		if res.Code != "INVALID_USER_ATTRIBUTE_VALUE" {
			t.Errorf("%v = %d %s, want INVALID_USER_ATTRIBUTE_VALUE", bad, res.Status, res.Code)
		}
	}

	set := api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin,
		map[string]string{"on_call": "Yes", "contract_ends": "2026-12-31", "head_count": "12"})
	if set.Status != http.StatusOK {
		t.Fatalf("set values: %d %s %s", set.Status, set.Code, set.Message)
	}

	var values map[string]string
	set.into(t, &values)
	if values["on_call"] != "true" {
		t.Errorf("a yes/no recorded as %q was stored as %q, want \"true\" — two "+
			"administrators must not be able to send an application two different "+
			"facts", "Yes", values["on_call"])
	}
	if values["contract_ends"] != "2026-12-31" {
		t.Errorf("date stored as %q", values["contract_ends"])
	}
}

// Clearing a value removes it rather than storing an empty string, and a key
// nobody sent is left alone.
//
// Both halves matter. Nothing is ever sent empty, so a stored empty string
// would be a value that is configured and silently never arrives; and a form
// showing three of an account's attributes must not blank the ones it did not
// show.
func TestClearingAValueRemovesItAndSilenceLeavesTheRestAlone(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "clears-values", "clears-values-password-1", "USER")

	for _, key := range []string{"badge_number", "site_code"} {
		if res := defineAttribute(t, api, admin, map[string]any{
			"key": key, "label": key, "kind": "TEXT",
		}); res.Status != http.StatusOK {
			t.Fatalf("define %s: %d %s", key, res.Status, res.Code)
		}
	}

	api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin,
		map[string]string{"badge_number": "A-1", "site_code": "SH"})

	// One key, empty. The other is not mentioned at all.
	cleared := api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin,
		map[string]string{"badge_number": ""})

	var values map[string]string
	cleared.into(t, &values)

	if _, present := values["badge_number"]; present {
		t.Errorf("a cleared value is still present as %q; an empty string would be "+
			"a value that is configured and never arrives", values["badge_number"])
	}
	if values["site_code"] != "SH" {
		t.Errorf("site_code = %q after a request that did not mention it, want SH",
			values["site_code"])
	}
}

// Retiring an attribute keeps its values and stops them being served.
func TestARetiredAttributeKeepsItsValuesAndStopsBeingServed(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "retired-attr", "retired-attr-password-1", "USER")

	var definition struct {
		ID string `json:"id"`
	}
	defineAttribute(t, api, admin, map[string]any{
		"key": "badge_number", "label": "Badge", "kind": "TEXT",
	}).into(t, &definition)

	api.do(http.MethodPut, "/api/v1/users/"+userID+"/attributes", admin,
		map[string]string{"badge_number": "A-1"})

	if res := api.do(http.MethodPost, "/api/v1/user-attributes/"+definition.ID+"/disable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Code)
	}

	var values map[string]string
	api.do(http.MethodGet, "/api/v1/users/"+userID+"/attributes", admin, nil).into(t, &values)
	if _, present := values["badge_number"]; present {
		t.Error("a retired attribute's value is still served; it is neither shown " +
			"nor sent, so it is not part of the account as anybody sees it")
	}

	// And it comes back with the value intact, which is the whole reason
	// retiring exists beside deleting.
	if res := api.do(http.MethodPost, "/api/v1/user-attributes/"+definition.ID+"/enable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("enable: %d %s", res.Status, res.Code)
	}
	api.do(http.MethodGet, "/api/v1/users/"+userID+"/attributes", admin, nil).into(t, &values)
	if values["badge_number"] != "A-1" {
		t.Errorf("value after enabling again = %q, want A-1: retiring must not "+
			"discard what deleting discards", values["badge_number"])
	}
}

// The count is bounded, and the message says why it is bounded.
func TestTheNumberOfDefinedAttributesIsBounded(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for i := 0; i < service.MaxCustomFieldsPerTenant; i++ {
		res := defineAttribute(t, api, admin, map[string]any{
			"key": fmt.Sprintf("field_%02d", i), "label": "Filler", "kind": "TEXT",
		})
		if res.Status != http.StatusOK {
			t.Fatalf("definition %d of %d: %d %s %s",
				i, service.MaxCustomFieldsPerTenant, res.Status, res.Code, res.Message)
		}
	}

	over := defineAttribute(t, api, admin, map[string]any{
		"key": "one_too_many", "label": "Over", "kind": "TEXT",
	})
	if over.Code != "TOO_MANY_USER_ATTRIBUTES" {
		t.Errorf("definition %d = %d %s, want TOO_MANY_USER_ATTRIBUTES",
			service.MaxCustomFieldsPerTenant+1, over.Status, over.Code)
	}
	if !strings.Contains(over.Message, "token") {
		t.Errorf("the refusal says %q, which does not mention why the bound "+
			"exists — it is about token size, not storage", over.Message)
	}
}

// Two tenants may define the same key, and neither sees the other's.
func TestAttributeDefinitionsAreIsolatedBetweenTenants(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	for _, tenant := range []struct {
		token, label string
	}{{first.token, "Ours"}, {second.token, "Theirs"}} {
		res := defineAttribute(t, api, tenant.token, map[string]any{
			"key": "badge_number", "label": tenant.label, "kind": "TEXT",
		})
		if res.Status != http.StatusOK {
			t.Fatalf("define for %s: %d %s %s", tenant.label, res.Status, res.Code, res.Message)
		}
	}

	listed := api.do(http.MethodGet, "/api/v1/user-attributes", second.token, nil)
	if strings.Contains(string(listed.Data), "Ours") {
		t.Error("one tenant's attribute definition appears in the other's list")
	}
}

// The catalogue carries the tenant's own alongside the built-in half, and says
// which is which.
func TestTheCatalogueCarriesBothHalves(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	defineAttribute(t, api, admin, map[string]any{
		"key": "badge_number", "label": "门禁卡号", "kind": "TEXT",
	})

	var fields []struct {
		Key                 string `json:"key"`
		Custom              bool   `json:"custom"`
		Inbound             bool   `json:"inbound"`
		OutboundOnlyBecause string `json:"outboundOnlyBecause"`
		Label               string `json:"label"`
	}
	api.do(http.MethodGet, "/api/v1/fields", admin, nil).into(t, &fields)

	seen := map[string]int{}
	for i, f := range fields {
		seen[f.Key] = i
	}

	i, present := seen["department"]
	if !present {
		t.Fatal("the catalogue has no built-in department field")
	}
	if fields[i].Custom || fields[i].Label != "" {
		t.Error("a built-in field is marked custom or carries a stored label; its " +
			"label belongs in the console's message catalogue, because it has to " +
			"read the same in both languages")
	}

	i, present = seen["badge_number"]
	if !present {
		t.Fatal("the catalogue does not carry the tenant's own attribute")
	}
	if !fields[i].Custom || fields[i].Label != "门禁卡号" {
		t.Errorf("the tenant's attribute is custom=%v label=%q, want true and its "+
			"own label", fields[i].Custom, fields[i].Label)
	}

	// And the boundary is visible from out here: role is outbound-only and says
	// why, which is what a reader deciding whether to map it needs.
	i, present = seen["role"]
	if !present {
		t.Fatal("the catalogue has no role field")
	}
	if fields[i].Inbound || fields[i].OutboundOnlyBecause == "" {
		t.Error("role is offered for inbound mapping, or offers no reason it is " +
			"not: a directory attribute granting administrator is escalation in a " +
			"system Portico does not own")
	}
}

func TestUserAttributeManagementIsAdministratorOnly(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "attr-bystander", "attr-bystander-password-1", "USER")
	user := api.login("attr-bystander", "attr-bystander-password-1")

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/fields", nil},
		{http.MethodGet, "/api/v1/user-attributes", nil},
		{http.MethodPost, "/api/v1/user-attributes", map[string]any{"key": "x_y", "label": "X", "kind": "TEXT"}},
	} {
		if res := api.do(call.method, call.path, user, call.body); res.Status != http.StatusForbidden {
			t.Errorf("%s %s as a normal user = %d %s, want 403",
				call.method, call.path, res.Status, res.Code)
		}
	}
}
