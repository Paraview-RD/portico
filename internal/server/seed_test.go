package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/seed"
)

// The seed is only worth having if it stays complete, and it will not stay
// complete on its own.
//
// The failure mode is specific and slow: somebody adds a screen, the seed does
// not know about it, and six months later the demonstration database covers
// three quarters of the product while looking like it covers all of it. Nobody
// notices, because an empty list looks the same as a feature nobody uses.
//
// So two things are checked here. Every list the console draws has rows in it
// after seeding — which is the property somebody actually wants — and every
// collection endpoint the router serves is either one of those or is named
// below as allowed to be empty, with a reason. The second is what makes adding
// a screen without seeding it a red build rather than a quiet gap.

// seededAPI builds a server and seeds the database behind it.
//
// The seeder opens its own connection to the same DSN rather than reaching
// into the server, which is what a developer does at a shell too. The
// bootstrap administrator the server creates is what api.adminToken signs in
// as; the seed adds its own people beside it.
func seededAPI(t *testing.T) *apiTest {
	t.Helper()

	cfg := testConfig(t)
	api := newAPITestWithConfig(t, cfg)

	seeder, err := seed.Open(cfg)
	if err != nil {
		t.Fatalf("open seeder: %v", err)
	}
	defer func() { _ = seeder.Close() }()

	if _, err := seeder.Run(context.Background(), seed.Options{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return api
}

// collection is one list the console draws, named by the request that fills it.
type collection struct {
	// path is the request, with any ids substituted from what was seeded.
	path string
	// screen is what a reader of a failure needs to know: which screen is
	// empty, not which URL returned nothing.
	screen string
}

// TestTheSeedFillsEveryListTheConsoleDraws is the property the seed exists for.
func TestTheSeedFillsEveryListTheConsoleDraws(t *testing.T) {
	api := seededAPI(t)
	admin := api.adminToken()

	for _, c := range seededCollections(t, api, admin) {
		res := api.do(http.MethodGet, c.path, admin, nil)
		if res.Status != http.StatusOK {
			t.Errorf("%s: GET %s answered %d %s", c.screen, c.path, res.Status, res.Code)
			continue
		}
		if countRows(t, res.Data) == 0 {
			t.Errorf("%s is empty after seeding (GET %s). Either the seed does "+
				"not cover it, or it covers it under a different tenant — "+
				"either way somebody looking at this screen sees nothing.",
				c.screen, c.path)
		}
	}
}

// seededCollections is every list, with ids filled in from the seeded data.
func seededCollections(t *testing.T, api *apiTest, admin string) []collection {
	t.Helper()

	lists := []collection{
		{path: "/api/v1/users?limit=20", screen: "Users"},
		{path: "/api/v1/organizations", screen: "Organizations"},
		{path: "/api/v1/groups", screen: "Groups"},
		{path: "/api/v1/applications/oauth-clients", screen: "Applications — OIDC"},
		{path: "/api/v1/applications/saml-service-providers", screen: "Applications — SAML"},
		{path: "/api/v1/applications/cas-services", screen: "Applications — CAS"},
		{path: "/api/v1/directories", screen: "Directory integration — LDAP"},
		{path: "/api/v1/scim-credentials", screen: "Directory integration — SCIM"},
		{path: "/api/v1/webhooks", screen: "Webhooks"},
		{path: "/api/v1/external-identity-providers", screen: "Identity providers"},
		// The public half of the same thing, and not the same list: this one
		// is the buttons a sign-in screen draws, so it carries the active
		// provider and not the disabled one. Seeded because a sign-in screen
		// with no button is the feature looking absent.
		{path: "/api/v1/auth/external/providers", screen: "Sign-in screen — provider buttons"},
		{path: "/api/v1/fields", screen: "Field catalogue (the mapping picker)"},
		{path: "/api/v1/user-attributes", screen: "Tenant-defined user attributes"},
		{path: "/api/v1/audit-logs?limit=20", screen: "Audit logs"},
		// The portal, which is what everybody who is not an administrator
		// sees. An empty one there is the whole product looking broken.
		{path: "/api/v1/portal/applications", screen: "Portal — applications"},
	}

	// The two nested lists, which need an id from what was seeded. They are
	// the histories — the screens somebody opens when something has gone
	// wrong — and an empty one there is the worst kind of empty.
	if id := firstID(t, api, admin, "/api/v1/directories"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/directories/" + id + "/runs",
			screen: "Directory integration — run history",
		})
	}
	if id := firstID(t, api, admin, "/api/v1/webhooks"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/webhooks/" + id + "/deliveries",
			screen: "Webhooks — delivery history",
		})
	}
	if id := firstID(t, api, admin, "/api/v1/groups"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/groups/" + id + "/members",
			screen: "Groups — members",
		})
	}
	// The mapping tables, one per recipient kind. Seeded so that an empty one
	// means "nobody has decided anything here" rather than "this screen was
	// never wired up" — which from the console look the same, and which is the
	// distinction this whole feature turns on.
	//
	// The OAuth one is addressed by client id rather than row id, because that
	// is what every other route under that prefix uses.
	if clientID := firstClientID(t, api, admin); clientID != "" {
		lists = append(lists, collection{
			path:   "/api/v1/applications/oauth-clients/" + clientID + "/field-mappings",
			screen: "Application detail — fields (OIDC)",
		})
	}
	if id := firstID(t, api, admin, "/api/v1/applications/saml-service-providers"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/applications/saml-service-providers/" + id + "/field-mappings",
			screen: "Application detail — fields (SAML)",
		})
	}
	if id := firstID(t, api, admin, "/api/v1/applications/cas-services"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/applications/cas-services/" + id + "/field-mappings",
			screen: "Application detail — fields (CAS)",
		})
	}
	if id := firstID(t, api, admin, "/api/v1/webhooks"); id != "" {
		lists = append(lists, collection{
			path:   "/api/v1/webhooks/" + id + "/field-mappings",
			screen: "Webhook detail — fields",
		})
	}
	// A named account rather than the first row, because these two are only
	// interesting for somebody the seed gave a group and a device to. The
	// bootstrap administrator has neither, and asserting against whoever
	// happens to sort first would be a test that passes for the wrong reason.
	if id := userID(t, api, admin, "zhangwei"); id != "" {
		lists = append(lists,
			collection{path: "/api/v1/users/" + id + "/groups", screen: "User detail — groups"},
			collection{path: "/api/v1/users/" + id + "/sessions", screen: "User detail — devices"},
			collection{path: "/api/v1/users/" + id + "/attributes", screen: "User detail — custom attributes"},
		)
	}
	return lists
}

// firstID reads the id of the first row of a list.
func firstID(t *testing.T, api *apiTest, token, path string) string {
	t.Helper()

	res := api.do(http.MethodGet, path, token, nil)
	if res.Status != http.StatusOK {
		t.Errorf("GET %s: %d %s", path, res.Status, res.Code)
		return ""
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Data, &rows); err != nil || len(rows) == 0 {
		return ""
	}
	return rows[0].ID
}

// firstClientID reads the client id — not the row id — of the first
// registered OAuth client, which is how that prefix's routes address one.
func firstClientID(t *testing.T, api *apiTest, token string) string {
	t.Helper()

	res := api.do(http.MethodGet, "/api/v1/applications/oauth-clients", token, nil)
	if res.Status != http.StatusOK {
		t.Errorf("list oauth clients: %d %s", res.Status, res.Code)
		return ""
	}
	var rows []struct {
		ClientID string `json:"clientId"`
	}
	if err := json.Unmarshal(res.Data, &rows); err != nil || len(rows) == 0 {
		return ""
	}
	return rows[0].ClientID
}

// userID finds a seeded account by the username the seed gave it.
func userID(t *testing.T, api *apiTest, token, username string) string {
	t.Helper()

	res := api.do(http.MethodGet, "/api/v1/users?keyword="+username+"&limit=5", token, nil)
	if res.Status != http.StatusOK {
		t.Errorf("look up %s: %d %s", username, res.Status, res.Code)
		return ""
	}
	var paged struct {
		Items []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res.Data, &paged); err != nil {
		return ""
	}
	for _, u := range paged.Items {
		if u.Username == username {
			return u.ID
		}
	}
	return ""
}

// countRows counts a JSON array, or the items of a paged envelope.
func countRows(t *testing.T, data json.RawMessage) int {
	t.Helper()

	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err == nil {
		return len(rows)
	}
	var paged struct {
		Items []json.RawMessage `json:"items"`
		Data  []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &paged); err == nil && (paged.Items != nil || paged.Data != nil) {
		return len(paged.Items) + len(paged.Data)
	}
	// A map, which is how one account's custom attribute values arrive: keyed
	// by the attribute key, because that is what the rest of the system refers
	// to and an id would make every caller resolve it.
	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(data, &keyed); err == nil {
		return len(keyed)
	}
	t.Errorf("cannot tell how many rows are in %s", string(data))
	return 0
}

// notSeeded is every remaining GET the router serves, with the reason the seed
// leaves it alone. Adding an endpoint means adding it to the list above or to
// this one — which is the point of the test below.
//
// Each entry is a decision rather than an exemption granted to make a test
// pass, and they fall into three kinds: one object rather than a list, a
// constant the console reads rather than data, and a list that belongs to
// whoever is asking.
var notSeeded = map[string]string{
	"/api/v1/health":                       "liveness, not a list",
	"/api/v1/ready":                        "readiness, not a list",
	"/api/v1/settings":                     "one settings object per tenant, and both tenants have one",
	"/api/v1/users/me":                     "the caller, not a list",
	"/api/v1/auth/external/callback":       "not a list; it spends a state a browser brought back from somewhere else",
	"/api/v1/users/me/external-identities": "what the caller has linked, and the seed links two people who are deliberately not this one — an administrator whose way in is a provider nobody can reach is a locked door",

	"/api/v1/webhooks/{id}/snapshot": "what a full sync would send, counted on demand — an answer about the tenant rather than a list, and it is right for a seeded tenant without the seed doing anything",

	"/api/v1/auth/permission-check":              "a yes-or-no answer about the caller",
	"/api/v1/auth/recovery-channels":             "what this deployment can send, from configuration",
	"/api/v1/auth/registration-status":           "whether registration is open, from settings",
	"/api/v1/webhooks/events":                    "the catalogue of event types, a constant",
	"/api/v1/applications/integration-endpoints": "the URLs this deployment publishes, a constant",
	"/api/v1/users/export":                       "a spreadsheet of the list that is already asserted",
	"/api/v1/users/import/template":              "a blank spreadsheet, the same one every time",

	// Sessions and group membership belong to whoever is asking, and the
	// account these tests sign in as is the bootstrap administrator rather
	// than a seeded person. The seeded accounts do have both; see
	// internal/seed/history.go and the groups list above.
	"/api/v1/users/me/sessions": "belongs to the caller, who here is the bootstrap admin",
	"/api/v1/users/me/groups":   "belongs to the caller, who here is the bootstrap admin",
}

// TestEverySeededCollectionIsAccountedFor is the anti-rot half.
//
// It calls nothing and asserts on route strings alone, so it is cheap and it
// cannot flake. What it catches is the omission the test above cannot: a new
// list endpoint that nobody remembered to seed, which would otherwise show an
// empty screen and no failure anywhere.
func TestEverySeededCollectionIsAccountedFor(t *testing.T) {
	api := newAPITest(t)

	router, ok := api.srv.Handler().(chi.Routes)
	if !ok {
		t.Fatal("the server's handler is not a chi router; this test walks its route tree")
	}

	covered := map[string]bool{}
	for _, c := range []string{
		"/api/v1/users", "/api/v1/organizations", "/api/v1/groups",
		"/api/v1/applications/oauth-clients", "/api/v1/applications/saml-service-providers",
		"/api/v1/applications/cas-services", "/api/v1/directories",
		"/api/v1/directories/{id}/runs", "/api/v1/scim-credentials",
		"/api/v1/webhooks", "/api/v1/webhooks/{id}/deliveries",
		"/api/v1/external-identity-providers", "/api/v1/auth/external/providers",
		"/api/v1/audit-logs", "/api/v1/portal/applications",
		"/api/v1/groups/{id}/members", "/api/v1/users/{id}/groups",
		"/api/v1/users/{id}/sessions", "/api/v1/fields",
		"/api/v1/user-attributes", "/api/v1/users/{id}/attributes",
		"/api/v1/applications/oauth-clients/{clientID}/field-mappings",
		"/api/v1/applications/saml-service-providers/{id}/field-mappings",
		"/api/v1/applications/cas-services/{id}/field-mappings",
		"/api/v1/webhooks/{id}/field-mappings",
		"/api/v1/organizations/{id}/administrators",
		"/api/v1/users/{id}/administered-organizations",
	} {
		covered[c] = true
	}

	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet || !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		path := normalize(route)
		// Only the collections. A route ending in a parameter fetches one
		// thing, and one thing is not a list.
		// A route whose last segment is a parameter fetches one thing, and one
		// thing is not a list.
		if !looksLikeCollection(path) {
			return nil
		}
		if covered[path] || notSeeded[path] != "" {
			return nil
		}
		t.Errorf("GET %s is served and the seed neither fills it nor says why "+
			"not. Add it to the seeded list, or to notSeeded with the reason — "+
			"a screen nobody seeded looks identical to a feature nobody uses.", path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
}

// looksLikeCollection reports whether a route serves a list rather than one
// item. A path whose last segment is a parameter names a single thing.
func looksLikeCollection(path string) bool {
	last := path[strings.LastIndex(path, "/")+1:]
	return !strings.HasPrefix(last, "{")
}
