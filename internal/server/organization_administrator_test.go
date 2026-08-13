package server_test

import (
	"net/http"
	"testing"
)

// Recording who would administer an organization is data collection, not a
// permission, and this file is what keeps those two apart.
//
// Delegated administration is planned. What a planned feature cannot do is
// invent, on the day it ships, the facts it needed to have been collecting —
// an organization chart is entered by people over months, so the place to
// write "Zhang administers Engineering" has to exist first. That is why
// these rows are written now.
//
// The hazard of collecting them early is precise: a table that looks like a
// role invites somebody to make it one, a line at a time, without anybody
// designing a permission model. So the first test below is the important
// one. When delegated administration is built, it is the test to come and
// edit deliberately — and having to edit it is the point.

// findUserID resolves a username to its id through the API, which is what a
// console does and therefore what these tests should do.
func findUserID(t *testing.T, api *apiTest, adminToken, username string) string {
	t.Helper()

	var page struct {
		Items []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	api.do(http.MethodGet, "/api/v1/users?pageSize=100", adminToken, nil).into(t, &page)

	for _, u := range page.Items {
		if u.Username == username {
			return u.ID
		}
	}
	t.Fatalf("no account named %q", username)
	return ""
}

func TestOrganizationAdministratorGrantsNothing(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Engineering", "ENG", "")
	childID := api.createOrg(admin, "Platform", "PLATFORM", orgID)
	api.createUser(admin, "org-admin", "org-admin-password-1", "USER")
	nomineeID := findUserID(t, api, admin, "org-admin")

	// The widest scope, so nothing here passes because the assignment was a
	// narrow one.
	res := api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin,
		map[string]any{"userId": nomineeID, "scope": "SUBTREE"})
	if res.Status != http.StatusOK {
		t.Fatalf("assign: %d %s %s", res.Status, res.Code, res.Message)
	}

	token := api.login("org-admin", "org-admin-password-1")

	// Tenant-wide administration, which being named here must not become a
	// way into. The organization list is deliberately absent: it is open to
	// every signed-in account and always has been, because the profile
	// screen needs it.
	for _, path := range []string{
		"/api/v1/users",
		"/api/v1/settings",
		"/api/v1/audit-logs",
		"/api/v1/organizations/" + orgID + "/administrators",
	} {
		if res := api.do(http.MethodGet, path, token, nil); res.Status != http.StatusForbidden {
			t.Errorf("being recorded as an organization's administrator granted "+
				"access to %s (%d %s). It must grant nothing: the rows exist for "+
				"a feature that does not exist yet, and this is the test that "+
				"says so.", path, res.Status, res.Code)
		}
	}

	// Administration of the organization named, and of the one below it —
	// the two things a reader of the table would most expect to work, and
	// exactly what must not.
	for _, target := range []struct{ what, id string }{
		{"the organization they are named on", orgID},
		{"an organization below it", childID},
	} {
		if res := api.do(http.MethodPut, "/api/v1/organizations/"+target.id, token,
			map[string]any{"name": "Renamed", "code": "RENAMED"}); res.Status != http.StatusForbidden {
			t.Errorf("a recorded administrator could edit %s (%d %s)",
				target.what, res.Status, res.Code)
		}
		if res := api.do(http.MethodPost, "/api/v1/organizations/"+target.id+"/disable", token, nil); res.Status != http.StatusForbidden {
			t.Errorf("a recorded administrator could disable %s (%d %s)",
				target.what, res.Status, res.Code)
		}
	}

	// Nor may they create an account, which is the first thing a delegated
	// administrator will be for and the last thing this may do today.
	if res := api.do(http.MethodPost, "/api/v1/users", token, map[string]any{
		"username": "hired-by-nobody", "displayName": "Hired", "password": "hired-password-1", "role": "USER",
	}); res.Status != http.StatusForbidden {
		t.Errorf("a recorded administrator could create an account (%d %s)",
			res.Status, res.Code)
	}

	// Nor may they name somebody else, which would be the table
	// bootstrapping itself into a role.
	if res := api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", token,
		map[string]any{"userId": nomineeID, "scope": "SELF"}); res.Status != http.StatusForbidden {
		t.Errorf("a recorded administrator could record another one (%d %s)",
			res.Status, res.Code)
	}
}

// The scope has to be stated. A row that does not say how far it reaches is
// one nobody can interpret when the feature that reads it arrives, and the
// only moment the answer is known is when somebody is filling in the form.
func TestAnAdministratorAssignmentMustNameItsScope(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Engineering", "ENG", "")
	api.createUser(admin, "no-scope", "no-scope-password-1", "USER")
	userID := findUserID(t, api, admin, "no-scope")

	for _, scope := range []any{"", "EVERYTHING", "self", nil} {
		body := map[string]any{"userId": userID}
		if scope != nil {
			body["scope"] = scope
		}
		res := api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin, body)
		if res.Status != http.StatusBadRequest || res.Code != "INVALID_ADMIN_SCOPE" {
			t.Errorf("scope %v was accepted as %d %s; it must be SELF or SUBTREE, "+
				"stated explicitly", scope, res.Status, res.Code)
		}
	}
}

func TestAnAdministratorIsRecordedOnce(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Engineering", "ENG", "")
	api.createUser(admin, "twice", "twice-password-1", "USER")
	userID := findUserID(t, api, admin, "twice")

	assign := func(scope string) response {
		return api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin,
			map[string]any{"userId": userID, "scope": scope})
	}

	if res := assign("SELF"); res.Status != http.StatusOK {
		t.Fatalf("first assignment: %d %s", res.Status, res.Code)
	}
	if res := assign("SUBTREE"); res.Status != http.StatusConflict || res.Code != "ALREADY_ORGANIZATION_ADMIN" {
		t.Errorf("second assignment = %d %s, want 409 ALREADY_ORGANIZATION_ADMIN. "+
			"Widening a scope has to be a removal and an addition, so both "+
			"appear in the trail as the decisions they are.", res.Status, res.Code)
	}

	// Removed and re-recorded, which is how a scope changes.
	if res := api.do(http.MethodDelete,
		"/api/v1/organizations/"+orgID+"/administrators/"+userID, admin, nil); res.Status != http.StatusOK {
		t.Fatalf("revoke: %d %s", res.Status, res.Code)
	}
	if res := assign("SUBTREE"); res.Status != http.StatusOK {
		t.Errorf("re-recording after removal = %d %s", res.Status, res.Code)
	}
}

// What the assignment records, and what a screen can show. Provenance is
// here because it can only be written as it happens: an audit that cannot
// say who conferred an authority is not an audit, and this becomes an
// authority later.
func TestAnAssignmentRecordsItsScopeAndWhoMadeIt(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Engineering", "ENG", "")
	api.createUser(admin, "recorded", "recorded-password-1", "USER")
	userID := findUserID(t, api, admin, "recorded")
	adminID := findUserID(t, api, admin, adminUsername)

	api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin,
		map[string]any{"userId": userID, "scope": "SUBTREE"})

	var admins []struct {
		UserID        string `json:"userId"`
		Username      string `json:"username"`
		DisplayName   string `json:"displayName"`
		Status        string `json:"status"`
		Scope         string `json:"scope"`
		GrantedBy     string `json:"grantedBy"`
		GrantedByName string `json:"grantedByName"`
		GrantedAt     string `json:"grantedAt"`
	}
	api.do(http.MethodGet, "/api/v1/organizations/"+orgID+"/administrators", admin, nil).into(t, &admins)

	if len(admins) != 1 {
		t.Fatalf("got %d administrators, want 1", len(admins))
	}
	got := admins[0]
	switch {
	case got.UserID != userID:
		t.Errorf("userId = %q, want %q", got.UserID, userID)
	case got.Scope != "SUBTREE":
		t.Errorf("scope = %q, want SUBTREE", got.Scope)
	case got.GrantedBy != adminID:
		t.Errorf("grantedBy = %q, want the account that assigned it (%q)", got.GrantedBy, adminID)
	case got.GrantedByName == "":
		t.Error("grantedByName is empty, so a screen has to show a bare id")
	case got.GrantedAt == "":
		t.Error("grantedAt is empty; when an authority was conferred is not optional")
	case got.Username == "" || got.DisplayName == "":
		t.Error("the assignment does not carry a name to show")
	case got.Status == "":
		t.Error("the assignment does not say whether the account is still usable")
	}

	// And the other direction, which is the query delegated administration
	// will make on every request.
	var administered []struct {
		ID    string `json:"id"`
		Code  string `json:"code"`
		Scope string `json:"scope"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+userID+"/administered-organizations", admin, nil).
		into(t, &administered)

	if len(administered) != 1 || administered[0].ID != orgID || administered[0].Scope != "SUBTREE" {
		t.Errorf("administered organizations = %+v, want the one just assigned", administered)
	}
}

// Disabling the account does not quietly drop the assignment.
//
// An assignment that vanished when somebody was suspended would come back on
// its own when they were reinstated, and nobody would have decided either.
// It is listed with the account's status instead, so a screen can say that
// the person named here cannot currently sign in.
func TestDisablingAnAccountKeepsItsAssignments(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Engineering", "ENG", "")
	api.createUser(admin, "suspended", "suspended-password-1", "USER")
	userID := findUserID(t, api, admin, "suspended")

	api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin,
		map[string]any{"userId": userID, "scope": "SELF"})

	if res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Code)
	}

	var admins []struct {
		UserID string `json:"userId"`
		Status string `json:"status"`
	}
	api.do(http.MethodGet, "/api/v1/organizations/"+orgID+"/administrators", admin, nil).into(t, &admins)

	if len(admins) != 1 {
		t.Fatalf("got %d administrators after disabling the account, want 1 — "+
			"an assignment that disappears on a suspension reappears on a "+
			"reinstatement, and nobody decided either", len(admins))
	}
	if admins[0].Status != "DISABLED" {
		t.Errorf("status = %q, want DISABLED so a screen can say the person "+
			"named here cannot sign in", admins[0].Status)
	}
}

// An assignment cannot reach across tenants. The database refuses it by
// composite foreign key rather than the application remembering to check,
// which is the arrangement everything else here uses.
func TestAnAdministratorCannotBeAssignedAcrossTenants(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("acme", "Acme")

	admin := f.api.adminToken()
	orgID := f.api.createOrg(admin, "Engineering", "ENG", "")

	// Somebody who exists only in the other tenant.
	acmeAdmin := f.api.loginTo("acme", adminUsername, adminPassword)
	var them struct {
		ID string `json:"id"`
	}
	f.api.do(http.MethodGet, "/api/v1/users/me", acmeAdmin, nil).into(t, &them)

	res := f.api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/administrators", admin,
		map[string]any{"userId": them.ID, "scope": "SELF"})
	if res.Status == http.StatusOK {
		t.Error("an account from another tenant was recorded as an administrator here")
	}
}
