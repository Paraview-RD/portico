package server_test

import (
	"net/http"
	"testing"
)

// The organization upgrades, and the two properties that keep them from
// becoming something else.
//
// A manager grants nothing, and an attachment does not move anybody's
// primary membership. Both are easy to add and easy to have quietly become a
// permission model or a second, contradictory membership — so both are
// asserted rather than assumed.

func TestOrganizationManagerGrantsNothing(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Platform", "PLATFORM", "")
	api.createUser(admin, "in-charge", "in-charge-password-1", "USER")

	var nominee struct {
		Items []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	api.do(http.MethodGet, "/api/v1/users?pageSize=50", admin, nil).into(t, &nominee)
	var nomineeID string
	for _, u := range nominee.Items {
		if u.Username == "in-charge" {
			nomineeID = u.ID
		}
	}

	res := api.do(http.MethodPut, "/api/v1/organizations/"+orgID+"/manager", admin,
		map[string]any{"managerId": nomineeID})
	if res.Status != http.StatusOK {
		t.Fatalf("nominate: %d %s %s", res.Status, res.Code, res.Message)
	}

	var org struct {
		ManagerID   string `json:"managerId"`
		ManagerName string `json:"managerName"`
	}
	res.into(t, &org)
	if org.ManagerID != nomineeID {
		t.Errorf("managerId = %q, want %q", org.ManagerID, nomineeID)
	}
	if org.ManagerName == "" {
		t.Error("managerName is empty, so a client has to show a bare id")
	}

	// And the nominee is still an ordinary account. This is the assertion
	// that matters: a field like this becoming a third role is the worst way
	// to acquire a permission model, because nothing declares it.
	token := api.login("in-charge", "in-charge-password-1")

	// Reading the organization list is open to every signed-in account and
	// always has been — the profile screen needs it — so it is not in this
	// list. What is: everything that is administrator-only, which being
	// named a manager must not become a way into.
	for _, path := range []string{
		"/api/v1/users",
		"/api/v1/settings",
		"/api/v1/audit-logs",
	} {
		if res := api.do(http.MethodGet, path, token, nil); res.Status != http.StatusForbidden {
			t.Errorf("being an organization's manager granted access to %s "+
				"(%d %s); it must grant nothing", path, res.Status, res.Code)
		}
	}

	// Nor may they nominate somebody else, which would be the field
	// bootstrapping itself into a role.
	if res := api.do(http.MethodPut, "/api/v1/organizations/"+orgID+"/manager", token,
		map[string]any{"managerId": ""}); res.Status != http.StatusForbidden {
		t.Errorf("an organization's manager could change the nomination "+
			"(%d %s)", res.Status, res.Code)
	}

	// Nor may they edit the organization they are named on.
	if res := api.do(http.MethodPut, "/api/v1/organizations/"+orgID, token,
		map[string]any{"name": "Renamed", "code": "PLATFORM"}); res.Status != http.StatusForbidden {
		t.Errorf("an organization's manager could edit it (%d %s)", res.Status, res.Code)
	}
}

func TestAttachmentDoesNotMovePrimaryMembership(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	home := api.createOrg(admin, "Platform", "PLATFORM", "")
	project := api.createOrg(admin, "Project X", "PROJECT-X", "")

	userID := api.createUser(admin, "seconded", "seconded-password-1", "USER")
	if res := api.do(http.MethodPut, "/api/v1/users/"+userID, admin, map[string]any{
		"displayName": "seconded", "role": "USER", "organizationId": home,
	}); res.Status != http.StatusOK {
		t.Fatalf("assign primary organization: %d %s %s", res.Status, res.Code, res.Message)
	}

	if res := api.do(http.MethodPost, "/api/v1/organizations/"+project+"/attachments", admin,
		map[string]any{"userId": userID}); res.Status != http.StatusOK {
		t.Fatalf("attach: %d %s %s", res.Status, res.Code, res.Message)
	}

	var user struct {
		OrganizationID string `json:"organizationId"`
		Attachments    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"attachments"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+userID, admin, nil).into(t, &user)

	if user.OrganizationID != home {
		t.Errorf("primary organization = %q, want %q — an attachment moved "+
			"the one membership that SCIM writes and an export names",
			user.OrganizationID, home)
	}
	if len(user.Attachments) != 1 || user.Attachments[0].ID != project {
		t.Errorf("attachments = %+v, want just the project", user.Attachments)
	}

	// Removing it is idempotent, because a caller reconciling a list should
	// not have to know what is already gone.
	for i := range 2 {
		res := api.do(http.MethodDelete,
			"/api/v1/organizations/"+project+"/attachments/"+userID, admin, nil)
		if res.Status != http.StatusOK {
			t.Errorf("detach %d: %d %s", i+1, res.Status, res.Code)
		}
	}
}

// Attaching somebody to the organization they already belong to would be a
// second, weaker record of the same fact — and the two could disagree once
// the primary membership moves.
func TestAttachingToTheirOwnOrganizationIsRefused(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Platform", "PLATFORM", "")
	userID := api.createUser(admin, "already-there", "already-there-password-1", "USER")
	api.do(http.MethodPut, "/api/v1/users/"+userID, admin, map[string]any{
		"displayName": "already-there", "role": "USER", "organizationId": orgID,
	})

	res := api.do(http.MethodPost, "/api/v1/organizations/"+orgID+"/attachments", admin,
		map[string]any{"userId": userID})
	if res.Code != "ALREADY_PRIMARY_ORGANIZATION" {
		t.Errorf("attaching to their own organization = %d %s, want "+
			"ALREADY_PRIMARY_ORGANIZATION", res.Status, res.Code)
	}
}

// Multiple roots were always supported; this says so, so that a later change
// cannot quietly introduce a single-root assumption.
func TestATenantMayHaveSeveralRootOrganizations(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	first := api.createOrg(admin, "China", "CN", "")
	second := api.createOrg(admin, "Singapore", "SG", "")

	var page []struct {
		ID       string `json:"id"`
		ParentID string `json:"parentId"`
	}
	api.do(http.MethodGet, "/api/v1/organizations", admin, nil).into(t, &page)

	roots := map[string]bool{}
	for _, org := range page {
		if org.ParentID == "" {
			roots[org.ID] = true
		}
	}
	if !roots[first] || !roots[second] {
		t.Errorf("both organizations should be roots; roots = %v", roots)
	}
}
