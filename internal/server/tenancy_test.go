package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/provision"
)

// These are the tests that actually prove tenant isolation. The guards in
// internal/store check that no query forgets its tenant predicate; these
// check that the resulting system behaves the way that is supposed to
// produce — two tenants holding identically named rows, neither able to see
// or change the other's.
//
// They go through the HTTP stack rather than calling services, because the
// interesting failures are at the seams: a handler taking the tenant from
// the wrong place, a token honoured outside the tenant it was minted for.

const secondTenantCode = "beta"

// tenantSetup is one tenant with an administrator signed in.
type tenantSetup struct {
	code  string
	token string
}

// newMultiTenantTest builds an instance with the default tenant plus a
// second one, each with its own administrator, and signs both in.
func newMultiTenantTest(t *testing.T) (*apiTest, tenantSetup, tenantSetup) {
	t.Helper()
	silenceLogs(t)

	cfg := testConfig(t)

	api := newAPITestWithConfig(t, cfg)

	// The second tenant is provisioned the way an operator would: from the
	// command-line path, because there is no API for it.
	p, err := provision.Open(cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	if _, err := p.CreateTenant(context.Background(),
		secondTenantCode, "Beta", adminUsername, adminPassword); err != nil {
		t.Fatalf("create second tenant: %v", err)
	}

	first := tenantSetup{
		code:  model.DefaultTenantCode,
		token: api.loginTo(model.DefaultTenantCode, adminUsername, adminPassword),
	}
	second := tenantSetup{
		code:  secondTenantCode,
		token: api.loginTo(secondTenantCode, adminUsername, adminPassword),
	}
	return api, first, second
}

// Both tenants have an "admin". Signing in has to land in the right one,
// or nothing below it means anything.
func TestSameUsernameInTwoTenants(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	if first.token == second.token {
		t.Fatal("two tenants' administrators received the same token")
	}

	for _, tenant := range []tenantSetup{first, second} {
		res := api.do(http.MethodGet, "/api/v1/users/me", tenant.token, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("%s: /users/me status = %d", tenant.code, res.Status)
		}

		var me struct {
			Username string `json:"username"`
			TenantID string `json:"tenantId"`
		}
		res.into(t, &me)

		if me.Username != adminUsername {
			t.Errorf("%s: username = %q, want %q", tenant.code, me.Username, adminUsername)
		}
		if me.TenantID == "" {
			t.Errorf("%s: profile carries no tenant", tenant.code)
		}
	}
}

// The listing every administrator sees must contain their own tenant's
// accounts and nobody else's.
func TestUserListsDoNotCross(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	api.createUser(first.token, "alice", "password-alpha-1", "USER")
	api.createUser(second.token, "bob", "password-beta-1", "USER")

	usernames := func(token string) map[string]bool {
		res := api.do(http.MethodGet, "/api/v1/users?pageSize=100", token, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("list users: %d %s", res.Status, res.Message)
		}
		var page struct {
			Items []struct {
				Username string `json:"username"`
			} `json:"items"`
		}
		res.into(t, &page)

		out := map[string]bool{}
		for _, item := range page.Items {
			out[item.Username] = true
		}
		return out
	}

	firstUsers := usernames(first.token)
	secondUsers := usernames(second.token)

	if !firstUsers["alice"] {
		t.Error("the first tenant cannot see its own user")
	}
	if firstUsers["bob"] {
		t.Error("the first tenant can see the second tenant's user")
	}
	if !secondUsers["bob"] {
		t.Error("the second tenant cannot see its own user")
	}
	if secondUsers["alice"] {
		t.Error("the second tenant can see the first tenant's user")
	}
}

// An id from another tenant is the classic way isolation fails: the row is
// found because the lookup is by primary key, and only afterwards does
// anyone think to ask whose it was. Ids leak by design here — they appear in
// audit entries and in anything syncing users downstream — so being
// unguessable is not a defence.
func TestUsersInAnotherTenantAreNotReachableByID(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	victimID := api.createUser(second.token, "victim", "password-beta-2", "USER")

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"read", http.MethodGet, "/api/v1/users/" + victimID, nil},
		{"update", http.MethodPut, "/api/v1/users/" + victimID, map[string]string{
			"displayName": "Renamed", "role": "USER",
		}},
		{"disable", http.MethodPost, "/api/v1/users/" + victimID + "/disable", nil},
		{"reset password", http.MethodPost, "/api/v1/users/" + victimID + "/password", map[string]string{
			"newPassword": "attacker-chosen-1",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := api.do(tc.method, tc.path, first.token, tc.body)
			if res.Status != http.StatusNotFound {
				t.Errorf("status = %d (%s), want 404: the other tenant's user was reachable",
					res.Status, res.Code)
			}
		})
	}

	// And the account is untouched: still enabled, still able to sign in
	// with the password its own tenant set.
	if token := api.loginTo(second.code, "victim", "password-beta-2"); token == "" {
		t.Error("the targeted account no longer works")
	}
}

// The tenant header is how an unauthenticated caller says which tenant they
// are signing in to. If an authenticated handler honoured it too, one
// tenant's administrator would reach every other tenant by adding a header.
func TestAuthenticatedRequestsIgnoreTenantHeader(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	api.createUser(second.token, "beta-only", "password-beta-3", "USER")

	res := api.doWithHeaders(http.MethodGet, "/api/v1/users?pageSize=100", first.token, nil,
		map[string]string{"X-Portico-Tenant": secondTenantCode})
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Status)
	}

	var page struct {
		Items []struct {
			Username string `json:"username"`
		} `json:"items"`
	}
	res.into(t, &page)

	for _, item := range page.Items {
		if item.Username == "beta-only" {
			t.Fatal("a tenant header on an authenticated request switched tenants")
		}
	}
}

// Organizations are the other tenant-scoped record, and the one whose code
// two tenants are most likely to pick identically.
func TestOrganizationCodesAreUniquePerTenantOnly(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	create := func(token string) response {
		return api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
			"name": "Sales", "code": "SALES",
		})
	}

	if res := create(first.token); res.Status != http.StatusOK {
		t.Fatalf("first tenant could not create SALES: %d %s", res.Status, res.Message)
	}
	if res := create(second.token); res.Status != http.StatusOK {
		t.Fatalf("second tenant could not reuse the code SALES: %d %s — "+
			"organization codes must be unique per tenant, not globally",
			res.Status, res.Message)
	}
	// Within one tenant it is still a conflict.
	if res := create(first.token); res.Status != http.StatusConflict {
		t.Errorf("duplicate code within a tenant: status = %d, want 409", res.Status)
	}

	// Each tenant sees exactly one.
	for _, tenant := range []tenantSetup{first, second} {
		res := api.do(http.MethodGet, "/api/v1/organizations", tenant.token, nil)
		var orgs []struct {
			Code string `json:"code"`
		}
		res.into(t, &orgs)
		if len(orgs) != 1 {
			t.Errorf("%s sees %d organizations, want 1", tenant.code, len(orgs))
		}
	}
}

// A user may not be filed under an organization belonging to someone else.
func TestUsersCannotJoinAnotherTenantsOrganization(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	res := api.do(http.MethodPost, "/api/v1/organizations", second.token, map[string]any{
		"name": "Beta Engineering", "code": "BETA-ENG",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create organization: %d %s", res.Status, res.Message)
	}
	var org struct {
		ID string `json:"id"`
	}
	res.into(t, &org)

	created := api.do(http.MethodPost, "/api/v1/users", first.token, map[string]string{
		"username": "crossing", "displayName": "Crossing",
		"password": "password-alpha-2", "organizationId": org.ID,
	})
	if created.Status != http.StatusNotFound {
		t.Errorf("status = %d (%s), want 404: a user was filed under another tenant's organization",
			created.Status, created.Code)
	}
}

// The audit trail is per tenant too. It records who did what, which is
// exactly the kind of thing a neighbouring tenant must not be able to read.
func TestAuditTrailsDoNotCross(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	api.createUser(second.token, "beta-audited", "password-beta-4", "USER")

	res := api.do(http.MethodGet, "/api/v1/audit-logs?pageSize=100", first.token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list audit logs: %d %s", res.Status, res.Message)
	}

	var page struct {
		Items []struct {
			TargetName string `json:"targetName"`
			ActorName  string `json:"actorName"`
		} `json:"items"`
	}
	res.into(t, &page)

	if len(page.Items) == 0 {
		t.Fatal("the first tenant's audit trail is empty; the assertion below would be vacuous")
	}
	for _, item := range page.Items {
		if item.TargetName == "beta-audited" {
			t.Error("one tenant's audit trail contains another tenant's event")
		}
	}
}

// Settings are per tenant: one may accept sign-ups while another does not,
// and each names itself.
func TestSettingsAreIsolated(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	res := api.do(http.MethodPut, "/api/v1/settings", second.token, map[string]any{
		"tokenTtlMinutes": 30, "registrationEnabled": true, "systemName": "Beta Portal",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update settings: %d %s", res.Status, res.Message)
	}

	read := api.do(http.MethodGet, "/api/v1/settings", first.token, nil)
	var settings struct {
		SystemName          string `json:"systemName"`
		RegistrationEnabled bool   `json:"registrationEnabled"`
	}
	read.into(t, &settings)

	if settings.SystemName == "Beta Portal" {
		t.Error("one tenant's settings changed another's")
	}
	if settings.RegistrationEnabled {
		t.Error("enabling registration in one tenant enabled it in another")
	}

	// And the public endpoint reports each tenant's own answer, which is
	// what the sign-in screen renders before anyone is authenticated.
	status := api.doWithHeaders(http.MethodGet, "/api/v1/auth/registration-status", "", nil,
		map[string]string{"X-Portico-Tenant": secondTenantCode})
	var public struct {
		SystemName          string `json:"systemName"`
		RegistrationEnabled bool   `json:"registrationEnabled"`
	}
	status.into(t, &public)

	if public.SystemName != "Beta Portal" || !public.RegistrationEnabled {
		t.Errorf("public status for %s = %+v, want the second tenant's own settings",
			secondTenantCode, public)
	}
}

// A disabled tenant refuses sign-in without anything being deleted.
func TestDisabledTenantRefusesSignIn(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = api.dsn

	p, err := provision.Open(cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	if _, err := p.SetTenantStatus(context.Background(), second.code, model.StatusDisabled); err != nil {
		t.Fatalf("disable tenant: %v", err)
	}

	res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"tenant": second.code, "identifier": adminUsername, "password": adminPassword,
	})
	if res.Status != http.StatusForbidden {
		t.Errorf("status = %d (%s), want 403 for a disabled tenant", res.Status, res.Code)
	}

	// The other tenant is unaffected.
	if res := api.do(http.MethodGet, "/api/v1/users/me", first.token, nil); res.Status != http.StatusOK {
		t.Errorf("disabling one tenant broke another: status = %d", res.Status)
	}
}

// Naming a tenant that does not exist is an operator error, not a
// credential probe, so it says so rather than pretending the password was
// wrong.
func TestUnknownTenantIsReported(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"tenant": "no-such-tenant", "identifier": adminUsername, "password": adminPassword,
	})
	if res.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.Status)
	}
	if res.Code != "TENANT_NOT_FOUND" {
		t.Errorf("code = %q, want TENANT_NOT_FOUND", res.Code)
	}
}

// A deployment that never creates a second tenant must not have to know
// that tenants exist: sign-in with no tenant named resolves to the default.
func TestSingleTenantDeploymentNeverMentionsTenants(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername, "password": adminPassword,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("sign-in without a tenant failed: %d %s %s", res.Status, res.Code, res.Message)
	}
}
