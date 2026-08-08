package server_test

import (
	"net/http"
	"testing"
)

// Organizations form a tree, and the interesting part is what the database
// cannot enforce.
//
// A foreign key is satisfied by every row in a cycle individually: if A's
// parent is B and B's parent is A, both rows point at something that exists.
// So the check has to be in the service layer, and these are what hold it
// there.

func (a *apiTest) createOrg(token, name, code, parentID string) string {
	a.t.Helper()

	res := a.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": name, "code": code, "parentId": parentID,
	})
	if res.Status != http.StatusOK {
		a.t.Fatalf("create organization %s: %d %s %s", code, res.Status, res.Code, res.Message)
	}

	var org struct {
		ID string `json:"id"`
	}
	res.into(a.t, &org)
	return org.ID
}

func (a *apiTest) moveOrg(token, id, parentID string) response {
	a.t.Helper()
	return a.do(http.MethodPut, "/api/v1/organizations/"+id, token, map[string]any{
		"name": "Moved", "parentId": parentID,
	})
}

func TestOrganizationsNest(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	root := api.createOrg(token, "Head Office", "HQ", "")
	child := api.createOrg(token, "Engineering", "ENG", root)
	grandchild := api.createOrg(token, "Platform", "PLAT", child)

	res := api.do(http.MethodGet, "/api/v1/organizations", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list: %d %s", res.Status, res.Code)
	}

	var orgs []struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		ParentID string `json:"parentId"`
	}
	res.into(t, &orgs)

	parents := map[string]string{}
	for _, o := range orgs {
		parents[o.Code] = o.ParentID
	}
	if parents["HQ"] != "" {
		t.Errorf("HQ has parent %q, want none", parents["HQ"])
	}
	if parents["ENG"] != root {
		t.Errorf("ENG parent = %q, want %q", parents["ENG"], root)
	}
	if parents["PLAT"] != child {
		t.Errorf("PLAT parent = %q, want %q", parents["PLAT"], child)
	}
	_ = grandchild
}

// The case a foreign key cannot catch.
func TestAnOrganizationCannotBecomeItsOwnAncestor(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	root := api.createOrg(token, "Head Office", "HQ", "")
	child := api.createOrg(token, "Engineering", "ENG", root)
	grandchild := api.createOrg(token, "Platform", "PLAT", child)

	cases := map[string]struct{ move, under string }{
		"under itself":       {root, root},
		"under its child":    {root, child},
		"under a descendant": {root, grandchild},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := api.moveOrg(token, c.move, c.under)
			if res.Status != http.StatusBadRequest || res.Code != "ORGANIZATION_CYCLE" {
				t.Errorf("move = %d %s, want 400 ORGANIZATION_CYCLE — every row in "+
					"a cycle satisfies the foreign key, so nothing else refuses this",
					res.Status, res.Code)
			}
		})
	}

	// And the tree is unchanged: a refused move must not half-apply.
	res := api.do(http.MethodGet, "/api/v1/organizations/"+root, token, nil)
	var org struct {
		ParentID string `json:"parentId"`
	}
	res.into(t, &org)
	if org.ParentID != "" {
		t.Errorf("the root acquired a parent %q from a refused move", org.ParentID)
	}
}

func TestOrganizationsCanBeRearranged(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	root := api.createOrg(token, "Head Office", "HQ", "")
	other := api.createOrg(token, "Second Office", "HQ2", "")
	child := api.createOrg(token, "Engineering", "ENG", root)

	// A move to a sibling's subtree is legal — rearranging is the whole
	// reason the parent is mutable while the code is not.
	if res := api.moveOrg(token, child, other); res.Status != http.StatusOK {
		t.Fatalf("move to another branch: %d %s %s", res.Status, res.Code, res.Message)
	}

	// And promoting back to a root.
	if res := api.moveOrg(token, child, ""); res.Status != http.StatusOK {
		t.Fatalf("promote to root: %d %s %s", res.Status, res.Code, res.Message)
	}

	res := api.do(http.MethodGet, "/api/v1/organizations/"+child, token, nil)
	var org struct {
		ParentID string `json:"parentId"`
	}
	res.into(t, &org)
	if org.ParentID != "" {
		t.Errorf("parentId = %q after promoting to a root, want empty", org.ParentID)
	}
}

func TestOrganizationDepthIsBounded(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Build a chain as deep as the limit allows, then one more.
	parent := ""
	for i := 0; i < 10; i++ {
		parent = api.createOrg(token, "Level", "L"+string(rune('a'+i)), parent)
	}

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Too deep", "code": "TOODEEP", "parentId": parent,
	})
	if res.Status != http.StatusBadRequest || res.Code != "ORGANIZATION_TOO_DEEP" {
		t.Errorf("creating past the depth limit = %d %s, want 400 ORGANIZATION_TOO_DEEP",
			res.Status, res.Code)
	}
}

// A parent in another tenant must be refused, and refused as "no such
// organization" rather than by the database.
func TestParentCannotCrossATenant(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("other", "Other")

	defaultToken := f.api.adminToken()
	otherToken := f.api.loginTo("other", adminUsername, adminPassword)

	theirs := f.api.createOrg(otherToken, "Their Office", "THEIRS", "")

	res := f.api.do(http.MethodPost, "/api/v1/organizations", defaultToken, map[string]any{
		"name": "Mine", "code": "MINE", "parentId": theirs,
	})
	if res.Status != http.StatusNotFound {
		t.Errorf("a parent from another tenant = %d %s, want 404", res.Status, res.Code)
	}
}
