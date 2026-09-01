package server_test

// Invitation-gated registration over the API: the administrative CRUD
// surface, and the redemption path it feeds. See
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md.

import (
	"net/http"
	"testing"
)

// setInvitationOnly is setRegistration's sibling for the invitation-only
// switch — see that comment for why the whole settings object round-trips.
func (a *apiTest) setInvitationOnly(t *testing.T, only bool) response {
	t.Helper()
	admin := a.adminToken()

	current := a.do(http.MethodGet, "/api/v1/settings", admin, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["invitationOnlyRegistration"] = only

	return a.do(http.MethodPut, "/api/v1/settings", admin, settings)
}

func TestCreateInvitationThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "WELCOME2026", "quota": 5,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create invitation: %d %s %s", res.Status, res.Code, res.Message)
	}

	var created struct {
		ID     string `json:"id"`
		Code   string `json:"code"`
		Quota  int    `json:"quota"`
		Status string `json:"status"`
	}
	res.into(t, &created)
	if created.Code != "WELCOME2026" || created.Quota != 5 || created.Status != "ACTIVE" {
		t.Errorf("created = %+v, unexpected fields", created)
	}

	list := api.do(http.MethodGet, "/api/v1/invitations", token, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("list invitations: %d %s", list.Status, list.Code)
	}
}

func TestCreateInvitationValidatesQuota(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "BADQUOTA", "quota": 0,
	})
	if res.Status != http.StatusBadRequest {
		t.Errorf("quota=0: status = %d, want 400", res.Status)
	}
}

func TestCreateInvitationRejectsDuplicateCode(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	first := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "DUP", "quota": 1,
	})
	if first.Status != http.StatusOK {
		t.Fatalf("first create: %d %s", first.Status, first.Code)
	}

	second := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "DUP", "quota": 1,
	})
	if second.Status != http.StatusConflict {
		t.Errorf("duplicate code: status = %d, want 409", second.Status)
	}
}

func TestDisableInvitationThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	created := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "TO-DISABLE", "quota": 5,
	})
	var inv struct {
		ID string `json:"id"`
	}
	created.into(t, &inv)

	disabled := api.do(http.MethodPost, "/api/v1/invitations/"+inv.ID+"/disable", token, nil)
	if disabled.Status != http.StatusOK {
		t.Fatalf("disable: %d %s %s", disabled.Status, disabled.Code, disabled.Message)
	}
	var result struct {
		Status string `json:"status"`
	}
	disabled.into(t, &result)
	if result.Status != "DISABLED" {
		t.Errorf("status = %q, want DISABLED", result.Status)
	}
}

func TestRegister_InvitationRequired_ThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	api.setRegistration(t, true, false)
	api.setInvitationOnly(t, true)

	// No code: refused.
	noCode := api.register("nocode", "nocode@example.test", "nocode-password-1")
	if noCode.Code != "INVITATION_REQUIRED" {
		t.Fatalf("register without code: %d %s, want INVITATION_REQUIRED", noCode.Status, noCode.Code)
	}

	// A valid code succeeds and assigns the code's organization.
	token := api.adminToken()
	orgRes := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Engineering", "code": "ENG",
	})
	if orgRes.Status != http.StatusOK {
		t.Fatalf("create organization: %d %s", orgRes.Status, orgRes.Code)
	}
	var org struct {
		ID string `json:"id"`
	}
	orgRes.into(t, &org)

	invRes := api.do(http.MethodPost, "/api/v1/invitations", token, map[string]any{
		"code": "ENG-INVITE", "quota": 1, "organizationId": org.ID,
	})
	if invRes.Status != http.StatusOK {
		t.Fatalf("create invitation: %d %s", invRes.Status, invRes.Code)
	}

	withCode := api.do(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "invitee", "displayName": "Invitee",
		"password": "invitee-password-1", "invitationCode": "ENG-INVITE",
	})
	if withCode.Status != http.StatusOK {
		t.Fatalf("register with code: %d %s %s", withCode.Status, withCode.Code, withCode.Message)
	}

	var created struct {
		OrganizationID string `json:"organizationId"`
	}
	withCode.into(t, &created)
	if created.OrganizationID != org.ID {
		t.Errorf("organizationId = %q, want %q", created.OrganizationID, org.ID)
	}

	// The code is exhausted now — quota was 1.
	again := api.do(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "invitee2", "displayName": "Invitee 2",
		"password": "invitee-password-1", "invitationCode": "ENG-INVITE",
	})
	if again.Code != "INVITATION_INVALID" {
		t.Errorf("second registration on an exhausted code: %d %s, want INVITATION_INVALID",
			again.Status, again.Code)
	}
}

// A registration is always evaluated against the tenant the request names,
// and an invitation code is unique only within its tenant — so a code
// issued in one tenant must not resolve at all in another, the same
// guarantee TestSameUsernameInTwoTenants checks for usernames.
func TestInvitationCodesAreIsolatedBetweenTenants(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	if res := api.doWithHeaders(http.MethodPut, "/api/v1/settings", first.token,
		map[string]any{"registrationEnabled": true}, nil); res.Status != http.StatusOK {
		t.Fatalf("enable registration on first tenant: %d %s", res.Status, res.Code)
	}

	created := api.do(http.MethodPost, "/api/v1/invitations", first.token, map[string]any{
		"code": "FIRST-TENANT-CODE", "quota": 5,
	})
	if created.Status != http.StatusOK {
		t.Fatalf("create invitation in first tenant: %d %s", created.Status, created.Code)
	}

	// Second tenant has its own registration switch, off by default —
	// enable it too, so the only variable left is the code itself.
	if res := api.doWithHeaders(http.MethodPut, "/api/v1/settings", second.token,
		map[string]any{"registrationEnabled": true}, nil); res.Status != http.StatusOK {
		t.Fatalf("enable registration on second tenant: %d %s", res.Status, res.Code)
	}

	res := api.doWithHeaders(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "crosser", "displayName": "Crosser",
		"password": "crosser-password-1", "invitationCode": "FIRST-TENANT-CODE",
	}, map[string]string{"X-Portico-Tenant": secondTenantCode})
	if res.Code != "INVITATION_INVALID" {
		t.Errorf("registering in the second tenant with the first tenant's code: "+
			"%d %s, want INVITATION_INVALID (the code must not resolve across tenants)",
			res.Status, res.Code)
	}
}
