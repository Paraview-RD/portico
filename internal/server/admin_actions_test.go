package server_test

import (
	"net/http"
	"testing"
)

// An administrator resetting someone else's password is a credential change
// made by a third party, so it has to both work and cut off the target's
// existing sessions.
func TestAdminPasswordResetRevokesTargetSessions(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	userID := api.createUser(adminToken, "reset.me", "original-password-1", "USER")
	userToken := api.login("reset.me", "original-password-1")

	// The target has a working session before the reset.
	if res := api.do(http.MethodGet, "/api/v1/users/me", userToken, nil); res.Status != http.StatusOK {
		t.Fatalf("target's token should work before the reset: %d", res.Status)
	}

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", adminToken, map[string]string{
		"newPassword": "administrator-set-password",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("reset failed: %d %s %s", res.Status, res.Code, res.Message)
	}

	// The target's live session is gone...
	res = api.do(http.MethodGet, "/api/v1/users/me", userToken, nil)
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; a password reset must end the target's sessions", res.Status)
	}

	// ...the old password no longer works...
	res = api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "reset.me", "password": "original-password-1",
	})
	if res.Status == http.StatusOK {
		t.Error("the old password still works after a reset")
	}

	// ...and the new one does.
	api.login("reset.me", "administrator-set-password")
}

func TestAdminPasswordResetRejectsWeakPasswords(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()
	userID := api.createUser(adminToken, "weak.target", "original-password-1", "USER")

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", adminToken, map[string]string{
		"newPassword": "short",
	})
	if res.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.Status)
	}
	if res.Code != "WEAK_PASSWORD" {
		t.Errorf("code = %q, want WEAK_PASSWORD", res.Code)
	}

	// The original password must still work, i.e. nothing changed.
	api.login("weak.target", "original-password-1")
}

func TestAdminPasswordResetRequiresAdmin(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	victimID := api.createUser(adminToken, "victim.acct", "victim-password-1", "USER")
	api.createUser(adminToken, "attacker", "attacker-password-1", "USER")
	attackerToken := api.login("attacker", "attacker-password-1")

	// A normal user must not be able to take over another account by
	// resetting its password.
	res := api.do(http.MethodPost, "/api/v1/users/"+victimID+"/password", attackerToken, map[string]string{
		"newPassword": "attacker-chosen-password",
	})
	if res.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", res.Status)
	}

	// The victim's original password still works.
	api.login("victim.acct", "victim-password-1")
}

func TestAdminPasswordResetOnMissingUser(t *testing.T) {
	api := newAPITest(t)
	res := api.do(http.MethodPost, "/api/v1/users/no-such-id/password", api.adminToken(), map[string]string{
		"newPassword": "some-valid-password",
	})

	if res.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.Status)
	}
	if res.Code != "USER_NOT_FOUND" {
		t.Errorf("code = %q, want USER_NOT_FOUND", res.Code)
	}
}

func TestOrganizationUpdate(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Original Name", "code": "ORIG", "remark": "first note", "sortOrder": 5,
	})
	var org struct {
		ID string `json:"id"`
	}
	res.into(t, &org)

	res = api.do(http.MethodPut, "/api/v1/organizations/"+org.ID, token, map[string]any{
		"name": "Renamed", "remark": "updated note", "sortOrder": 1,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update failed: %d %s %s", res.Status, res.Code, res.Message)
	}

	var updated struct {
		Name      string `json:"name"`
		Code      string `json:"code"`
		Remark    string `json:"remark"`
		SortOrder int    `json:"sortOrder"`
	}
	res.into(t, &updated)

	if updated.Name != "Renamed" || updated.Remark != "updated note" || updated.SortOrder != 1 {
		t.Errorf("update did not take effect: %+v", updated)
	}
	// The code is immutable: imports and downstream systems reference it, so
	// an update payload must not be able to change it even implicitly.
	if updated.Code != "ORIG" {
		t.Errorf("code = %q, want it to stay ORIG", updated.Code)
	}
}

func TestOrganizationUpdateRejectsMissingAndInvalid(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	t.Run("missing organization", func(t *testing.T) {
		res := api.do(http.MethodPut, "/api/v1/organizations/no-such-id", token, map[string]any{
			"name": "Whatever",
		})
		if res.Status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", res.Status)
		}
	})

	t.Run("blank name", func(t *testing.T) {
		res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
			"name": "Named", "code": "NAMED",
		})
		var org struct {
			ID string `json:"id"`
		}
		res.into(t, &org)

		res = api.do(http.MethodPut, "/api/v1/organizations/"+org.ID, token, map[string]any{
			"name": "   ",
		})
		if res.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.Status)
		}
	})
}

// The list carries member counts, and activeOnly filters out disabled
// organizations — the user form relies on both.
func TestOrganizationListCountsAndFilter(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Staffed", "code": "STAFF",
	})
	var staffed struct {
		ID string `json:"id"`
	}
	res.into(t, &staffed)

	res = api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Empty", "code": "EMPTY",
	})
	var empty struct {
		ID string `json:"id"`
	}
	res.into(t, &empty)

	for _, name := range []string{"member.one", "member.two"} {
		if r := api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
			"username": name, "displayName": name,
			"password": "password-12345", "organizationId": staffed.ID,
		}); r.Status != http.StatusOK {
			t.Fatalf("create %s: %d %s", name, r.Status, r.Code)
		}
	}

	type orgRow struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		UserCount int64  `json:"userCount"`
	}

	t.Run("member counts", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/organizations", token, nil)
		var orgs []orgRow
		res.into(t, &orgs)

		counts := map[string]int64{}
		for _, o := range orgs {
			counts[o.ID] = o.UserCount
		}
		if counts[staffed.ID] != 2 {
			t.Errorf("staffed count = %d, want 2", counts[staffed.ID])
		}
		if counts[empty.ID] != 0 {
			t.Errorf("empty count = %d, want 0", counts[empty.ID])
		}
	})

	t.Run("activeOnly excludes disabled", func(t *testing.T) {
		if r := api.do(http.MethodPost, "/api/v1/organizations/"+empty.ID+"/disable", token, nil); r.Status != http.StatusOK {
			t.Fatalf("disable failed: %d %s", r.Status, r.Code)
		}

		res := api.do(http.MethodGet, "/api/v1/organizations?activeOnly=true", token, nil)
		var orgs []orgRow
		res.into(t, &orgs)

		for _, o := range orgs {
			if o.ID == empty.ID {
				t.Error("activeOnly returned a disabled organization")
			}
		}

		// Without the filter it is still listed, so the UI can show it.
		res = api.do(http.MethodGet, "/api/v1/organizations", token, nil)
		res.into(t, &orgs)
		found := false
		for _, o := range orgs {
			if o.ID == empty.ID {
				found = true
			}
		}
		if !found {
			t.Error("the unfiltered list dropped a disabled organization")
		}
	})
}

// A disabled organization must keep its existing members rather than
// silently detaching them, since that would erase who belonged where.
func TestDisablingOrganizationKeepsMembers(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Sunset", "code": "SUNSET",
	})
	var org struct {
		ID string `json:"id"`
	}
	res.into(t, &org)

	api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
		"username": "stays.put", "displayName": "Stays Put",
		"password": "password-12345", "organizationId": org.ID,
	})

	if r := api.do(http.MethodPost, "/api/v1/organizations/"+org.ID+"/disable", token, nil); r.Status != http.StatusOK {
		t.Fatalf("disable failed: %d %s", r.Status, r.Code)
	}

	res = api.do(http.MethodGet, "/api/v1/users?keyword=stays.put", token, nil)
	var page struct {
		Items []struct {
			OrganizationID   string `json:"organizationId"`
			OrganizationName string `json:"organizationName"`
		} `json:"items"`
	}
	res.into(t, &page)

	if len(page.Items) != 1 {
		t.Fatalf("found %d users, want 1", len(page.Items))
	}
	if page.Items[0].OrganizationID != org.ID {
		t.Error("the member was detached when the organization was disabled")
	}
	if page.Items[0].OrganizationName != "Sunset" {
		t.Errorf("organizationName = %q, want it still resolved", page.Items[0].OrganizationName)
	}
}
