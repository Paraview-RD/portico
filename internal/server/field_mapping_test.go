package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The mapping API, at the boundary.
//
// The overlay and the rules have their own tests in internal/service. What is
// under test here is the part only a request can reach: that the four kinds of
// recipient are addressed the way the routes say, that a save round-trips, and
// that the refusals arrive as refusals rather than as a constraint violation
// somebody has to read a stack trace to understand.

// mappingRule is one rule as the API renders it.
type mappingRule struct {
	SourceKey    string `json:"sourceKey"`
	TargetName   string `json:"targetName"`
	FriendlyName string `json:"friendlyName"`
	Suppressed   bool   `json:"suppressed"`
}

func mappingsAt(t *testing.T, api *apiTest, token, path string) []mappingRule {
	t.Helper()

	res := api.do(http.MethodGet, path, token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, res.Status, res.Code)
	}
	var rules []mappingRule
	if err := json.Unmarshal(res.Data, &rules); err != nil {
		t.Fatalf("read mappings: %v", err)
	}
	return rules
}

func body(rules ...map[string]any) map[string]any {
	return map[string]any{"mappings": rules}
}

// A save round-trips, and replaces rather than merges.
//
// The second save is the assertion that matters. A form is a table somebody
// edited, so what it sends is the whole set — and a merge would leave the row
// they deleted in place, which is the one outcome nobody expects from a save.
func TestSavingMappingsReplacesTheWholeSet(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()
	path := "/api/v1/applications/oauth-clients/wiki/field-mappings"

	res := api.do(http.MethodPut, path, admin, body(
		map[string]any{"sourceKey": "department", "targetName": "dept"},
		map[string]any{"sourceKey": "title", "targetName": "job_title"},
	))
	if res.Status != http.StatusOK {
		t.Fatalf("save: %d %s", res.Status, res.Code)
	}
	if rules := mappingsAt(t, api, admin, path); len(rules) != 2 {
		t.Fatalf("after saving two rules the set has %d", len(rules))
	}

	// The form now holds one row. The other must be gone, not merged.
	res = api.do(http.MethodPut, path, admin, body(
		map[string]any{"sourceKey": "department", "targetName": "dept"},
	))
	if res.Status != http.StatusOK {
		t.Fatalf("second save: %d %s", res.Status, res.Code)
	}

	rules := mappingsAt(t, api, admin, path)
	if len(rules) != 1 {
		t.Fatalf("after saving one rule the set has %d: %+v", len(rules), rules)
	}
	if rules[0].SourceKey != "department" || rules[0].TargetName != "dept" {
		t.Errorf("the surviving rule is %+v, want department → dept", rules[0])
	}
}

// An empty list restores the defaults, which is how somebody backs out.
func TestSavingAnEmptySetRestoresTheDefaults(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()
	path := "/api/v1/webhooks/" + firstID(t, api, admin, "/api/v1/webhooks") + "/field-mappings"

	if res := api.do(http.MethodPut, path, admin, body()); res.Status != http.StatusOK {
		t.Fatalf("clear: %d %s", res.Status, res.Code)
	}
	if rules := mappingsAt(t, api, admin, path); len(rules) != 0 {
		t.Errorf("after clearing, the set still holds %+v", rules)
	}
}

// The refusals, each with the reason it exists.
//
// These are the reason this API has a normalization step at all. A rule that
// cannot be saved is an integration that fails at configuration time, in front
// of somebody who can fix it — rather than at sign-in, in front of somebody
// who cannot.
func TestTheRulesThatWouldLieAreRefused(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()

	oidc := "/api/v1/applications/oauth-clients/wiki/field-mappings"
	hook := "/api/v1/webhooks/" + firstID(t, api, admin, "/api/v1/webhooks") + "/field-mappings"

	for _, c := range []struct {
		name string
		path string
		want string
		body map[string]any
		why  string
	}{
		{
			name: "a claim OpenID Connect acts on",
			path: oidc, want: "RESERVED_CLAIM_NAME",
			body: body(map[string]any{"sourceKey": "department", "targetName": "sub"}),
			why: "a department arriving as `sub` tells an application that one " +
				"person is another, in a token it has every reason to trust",
		},
		{
			name: "two rules for one field",
			path: oidc, want: "DUPLICATE_MAPPING_SOURCE",
			body: body(
				map[string]any{"sourceKey": "department", "targetName": "dept"},
				map[string]any{"sourceKey": "department", "targetName": "team"},
			),
			why: "which one wins would be decided by whichever was read first",
		},
		{
			name: "two fields under one name",
			path: oidc, want: "DUPLICATE_MAPPING_TARGET",
			body: body(
				map[string]any{"sourceKey": "department", "targetName": "unit"},
				map[string]any{"sourceKey": "cost_center", "targetName": "unit"},
			),
			why: "only one would arrive, and not the one anybody picked",
		},
		{
			name: "a name the event payload already uses",
			path: hook, want: "PAYLOAD_NAME_TAKEN",
			body: body(map[string]any{"sourceKey": "department", "targetName": "id"}),
			why:  "a department would arrive where every subscriber reads the identifier",
		},
		{
			name: "neither a name nor a suppression",
			path: oidc, want: "MAPPING_TARGET_REQUIRED",
			body: body(map[string]any{"sourceKey": "department"}),
			why:  "the rule says nothing about what should happen",
		},
		{
			name: "a field nobody defined",
			path: oidc, want: "UNKNOWN_FIELD",
			body: body(map[string]any{"sourceKey": "no_such_field", "targetName": "x"}),
			why:  "usually a typo, and the only thing that identifies it is the key",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := api.do(http.MethodPut, c.path, admin, c.body)
			if res.Code != c.want {
				t.Errorf("answered %d %s, want %s — %s", res.Status, res.Code, c.want, c.why)
			}
		})
	}
}

// A name OpenID Connect reserves is unremarkable elsewhere.
//
// The refusal is derived from which recipient is being configured rather than
// passed in as a flag, and this is the case that tells the two designs apart:
// a SAML attribute called `sub` is an ordinary attribute, and refusing it
// would be refusing a valid integration to enforce another protocol's rule.
func TestASAMLAttributeMayTakeANameOIDCReserves(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()

	id := firstID(t, api, admin, "/api/v1/applications/saml-service-providers")
	path := "/api/v1/applications/saml-service-providers/" + id + "/field-mappings"

	res := api.do(http.MethodPut, path, admin, body(
		map[string]any{"sourceKey": "username", "targetName": "sub"},
	))
	if res.Status != http.StatusOK {
		t.Fatalf("saving a SAML attribute called `sub` was refused: %d %s", res.Status, res.Code)
	}
}

// A recipient that is not there is a 404, not a constraint violation.
//
// Without the existence check this would reach the foreign key and surface as
// a 500 describing a column — for what is an ordinary wrong-address mistake.
func TestMappingsForARecipientThatDoesNotExistAre404(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()

	for _, path := range []string{
		"/api/v1/applications/oauth-clients/no-such-client/field-mappings",
		"/api/v1/applications/saml-service-providers/no-such-id/field-mappings",
		"/api/v1/applications/cas-services/no-such-id/field-mappings",
		"/api/v1/webhooks/no-such-id/field-mappings",
	} {
		if res := api.do(http.MethodGet, path, admin, nil); res.Status != http.StatusNotFound {
			t.Errorf("GET %s answered %d %s, want 404", path, res.Status, res.Code)
		}
		res := api.do(http.MethodPut, path, admin, body(
			map[string]any{"sourceKey": "department", "targetName": "dept"},
		))
		if res.Status != http.StatusNotFound {
			t.Errorf("PUT %s answered %d %s, want 404", path, res.Status, res.Code)
		}
	}
}
