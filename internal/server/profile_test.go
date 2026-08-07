package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// A user maintains their own details without an administrator (§3.5).
func TestUpdateOwnProfile(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	api.createUser(admin, "mallory.user", "profile-pass-1", "USER")
	token := api.loginTo("", "mallory.user", "profile-pass-1")

	res := api.do(http.MethodPut, "/api/v1/users/me", token, map[string]string{
		"displayName": "Mallory Renamed",
		"email":       "mallory@example.com",
		"phone":       "13800002222",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update profile: %d %s %s", res.Status, res.Code, res.Message)
	}

	me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	var profile struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		Role        string `json:"role"`
	}
	me.into(t, &profile)

	if profile.DisplayName != "Mallory Renamed" {
		t.Errorf("displayName = %q", profile.DisplayName)
	}
	if profile.Email != "mallory@example.com" || profile.Phone != "13800002222" {
		t.Errorf("contact details did not stick: %+v", profile)
	}

	// The address is now a working sign-in identifier.
	api.loginTo("", "mallory@example.com", "profile-pass-1")
}

// The endpoint's field list is its security boundary. A self-service
// endpoint that accepted a role would be a privilege-escalation endpoint;
// one that accepted an organization would let anyone file themselves under
// any department.
func TestOwnProfileCannotChangePrivilegedFields(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgRes := api.do(http.MethodPost, "/api/v1/organizations", admin, map[string]any{
		"name": "Finance", "code": "FIN",
	})
	var org struct {
		ID string `json:"id"`
	}
	orgRes.into(t, &org)

	api.createUser(admin, "niaj.user", "profile-pass-2", "USER")
	token := api.loginTo("", "niaj.user", "profile-pass-2")

	rejected := []map[string]any{
		{"displayName": "Niaj", "role": "SUPER_ADMIN"},
		{"displayName": "Niaj", "organizationId": org.ID},
		{"displayName": "Niaj", "status": "DISABLED"},
		{"displayName": "Niaj", "username": "niaj.renamed"},
	}

	for _, body := range rejected {
		res := api.do(http.MethodPut, "/api/v1/users/me", token, body)
		if res.Status != http.StatusBadRequest {
			t.Errorf("body %v: status = %d (%s), want 400 — the field should not be accepted at all",
				body, res.Status, res.Code)
		}
	}

	// Nothing moved.
	me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	var profile struct {
		Username       string `json:"username"`
		Role           string `json:"role"`
		Status         string `json:"status"`
		OrganizationID string `json:"organizationId"`
	}
	me.into(t, &profile)

	if profile.Role != "USER" || profile.Username != "niaj.user" ||
		profile.Status != "ACTIVE" || profile.OrganizationID != "" {
		t.Errorf("a privileged field changed: %+v", profile)
	}
}

// Contact details are sign-in identifiers, so a user cannot take one another
// account in the tenant already holds.
func TestOwnProfileCannotTakeAnotherAccountsAddress(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "olivia", "displayName": "Olivia", "password": "profile-pass-3",
		"email": "olivia@example.com",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create olivia: %d %s", res.Status, res.Message)
	}

	api.createUser(admin, "peggy", "profile-pass-4", "USER")
	token := api.loginTo("", "peggy", "profile-pass-4")

	taken := api.do(http.MethodPut, "/api/v1/users/me", token, map[string]string{
		"displayName": "Peggy", "email": "olivia@example.com",
	})
	if taken.Status != http.StatusConflict || taken.Code != "EMAIL_TAKEN" {
		t.Errorf("status = %d, code = %q; want 409 EMAIL_TAKEN", taken.Status, taken.Code)
	}
}

// Repointing a recovery destination is how a stolen session becomes
// permanent access, so the trail has to say it happened and say what it
// changed to. "The profile was updated" would not tell an investigator
// anything.
func TestProfileChangeIsAudited(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	api.createUser(admin, "rupert", "profile-pass-5", "USER")
	token := api.loginTo("", "rupert", "profile-pass-5")

	res := api.do(http.MethodPut, "/api/v1/users/me", token, map[string]string{
		"displayName": "Rupert", "email": "attacker-controlled@example.com",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update profile: %d %s", res.Status, res.Message)
	}

	logs := api.do(http.MethodGet, "/api/v1/audit-logs?action=PROFILE_UPDATE_SELF&pageSize=50", admin, nil)
	var page struct {
		Items []struct {
			ActorName string `json:"actorName"`
			Detail    string `json:"detail"`
		} `json:"items"`
	}
	logs.into(t, &page)

	if len(page.Items) == 0 {
		t.Fatal("no PROFILE_UPDATE_SELF entry was recorded")
	}
	entry := page.Items[0]
	if entry.ActorName != "rupert" {
		t.Errorf("actor = %q, want rupert", entry.ActorName)
	}
	if entry.Detail == "" {
		t.Error("the entry does not say what changed")
	}
	if want := "attacker-controlled@example.com"; !strings.Contains(entry.Detail, want) {
		t.Errorf("detail = %q, want it to name the new address", entry.Detail)
	}
}
