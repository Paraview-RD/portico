package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// The administrative side of a directory connector.
//
// The synchronization itself is tested in internal/service against a fake
// directory and in internal/directory against a real one. What is left for
// this layer is the part those cannot see: who may reach these endpoints,
// and whether the bind password can come back out.

func registerDirectory(t *testing.T, api *apiTest, token, name string, extra map[string]any) response {
	t.Helper()

	body := map[string]any{
		"name": name, "host": "ldap.example.test", "port": 389,
		"encryption": "none",
		"baseDn":     "dc=example,dc=org", "userFilter": "(objectClass=inetOrgPerson)",
		"attrUsername": "uid", "attrDisplayName": "cn", "attrExternalId": "entryUUID",
	}
	for k, v := range extra {
		body[k] = v
	}
	return api.do(http.MethodPost, "/api/v1/directories", token, body)
}

// A bind password goes in and never comes out. Not on the creating response,
// not on a later read, not on the listing — the model type has no field for
// it at all, and this is what asserts that stays true.
func TestBindPasswordIsWriteOnly(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	const secret = "s3rvice-account-p@ssword"
	created := registerDirectory(t, api, admin, "Head office", map[string]any{
		"bindDn": "cn=reader,dc=example,dc=org", "bindPassword": secret,
	})
	if created.Status != http.StatusOK {
		t.Fatalf("register directory: %d %s %s", created.Status, created.Code, created.Message)
	}
	if strings.Contains(string(created.Data), secret) {
		t.Fatal("the registration response contains the bind password")
	}

	var source struct {
		ID              string `json:"id"`
		HasBindPassword bool   `json:"hasBindPassword"`
	}
	created.into(t, &source)
	if !source.HasBindPassword {
		t.Error("hasBindPassword is false although one was stored; a form " +
			"cannot then tell 'set' from 'anonymous bind'")
	}

	for _, path := range []string{"/api/v1/directories", "/api/v1/directories/" + source.ID} {
		read := api.do(http.MethodGet, path, admin, nil)
		if read.Status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, read.Status, read.Code)
		}
		if strings.Contains(string(read.Data), secret) {
			t.Errorf("GET %s returned the bind password", path)
		}
	}
}

// Omitting the field on an update leaves the credential alone. An edit form
// cannot display it, so submitting the form unchanged must not blank it.
func TestUpdatingWithoutAPasswordKeepsTheStoredOne(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	created := registerDirectory(t, api, admin, "Keeps", map[string]any{
		"bindDn": "cn=reader,dc=example,dc=org", "bindPassword": "original",
	})
	var source struct {
		ID string `json:"id"`
	}
	created.into(t, &source)

	updated := api.do(http.MethodPut, "/api/v1/directories/"+source.ID, admin, map[string]any{
		"name": "Keeps", "host": "ldap2.example.test", "port": 389, "encryption": "none",
		"bindDn": "cn=reader,dc=example,dc=org",
		"baseDn": "dc=example,dc=org", "userFilter": "(objectClass=inetOrgPerson)",
		"attrUsername": "uid", "attrDisplayName": "cn", "attrExternalId": "entryUUID",
	})
	if updated.Status != http.StatusOK {
		t.Fatalf("update: %d %s %s", updated.Status, updated.Code, updated.Message)
	}

	var after struct {
		HasBindPassword bool `json:"hasBindPassword"`
	}
	updated.into(t, &after)
	if !after.HasBindPassword {
		t.Error("an update that omitted bindPassword cleared the stored one; " +
			"submitting an edit form unchanged would silently break the sync")
	}

	// And an explicit empty string does clear it, which is how somebody
	// moves a source to an anonymous bind.
	cleared := api.do(http.MethodPut, "/api/v1/directories/"+source.ID, admin, map[string]any{
		"name": "Keeps", "host": "ldap2.example.test", "port": 389, "encryption": "none",
		"bindDn": "", "bindPassword": "",
		"baseDn": "dc=example,dc=org", "userFilter": "(objectClass=inetOrgPerson)",
		"attrUsername": "uid", "attrDisplayName": "cn", "attrExternalId": "entryUUID",
	})
	cleared.into(t, &after)
	if after.HasBindPassword {
		t.Error("an explicit empty bindPassword did not clear the stored one")
	}
}

func TestDirectoryManagementIsAdministratorOnly(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "directory-bystander", "bystander-password-1", "USER")
	user := api.login("directory-bystander", "bystander-password-1")

	created := registerDirectory(t, api, admin, "Guarded", nil)
	var source struct {
		ID string `json:"id"`
	}
	created.into(t, &source)

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/directories", nil},
		{http.MethodPost, "/api/v1/directories", map[string]any{"host": "x"}},
		{http.MethodGet, "/api/v1/directories/" + source.ID, nil},
		{http.MethodPut, "/api/v1/directories/" + source.ID, map[string]any{"host": "x"}},
		{http.MethodPost, "/api/v1/directories/" + source.ID + "/disable", nil},
		{http.MethodPost, "/api/v1/directories/" + source.ID + "/sync", nil},
		{http.MethodGet, "/api/v1/directories/" + source.ID + "/runs", nil},
	} {
		res := api.do(call.method, call.path, user, call.body)
		if res.Status != http.StatusForbidden {
			t.Errorf("%s %s as a normal user = %d %s, want 403",
				call.method, call.path, res.Status, res.Code)
		}
	}
}

// A disabled connector is not synchronized, and says so rather than
// pretending to have run.
func TestDisabledDirectoryRefusesToSync(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	created := registerDirectory(t, api, admin, "Paused", nil)
	var source struct {
		ID string `json:"id"`
	}
	created.into(t, &source)

	if res := api.do(http.MethodPost, "/api/v1/directories/"+source.ID+"/disable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Code)
	}

	res := api.do(http.MethodPost, "/api/v1/directories/"+source.ID+"/sync", admin, nil)
	if res.Code != "LDAP_SOURCE_DISABLED" {
		t.Errorf("syncing a disabled directory = %d %s, want LDAP_SOURCE_DISABLED",
			res.Status, res.Code)
	}
}

func TestDirectoriesAreIsolatedBetweenTenants(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	created := registerDirectory(t, api, first.token, "Ours", nil)
	if created.Status != http.StatusOK {
		t.Fatalf("register: %d %s %s", created.Status, created.Code, created.Message)
	}
	var source struct {
		ID string `json:"id"`
	}
	created.into(t, &source)

	res := api.do(http.MethodGet, "/api/v1/directories/"+source.ID, second.token, nil)
	if res.Status != http.StatusNotFound {
		t.Errorf("reading another tenant's directory = %d %s, want 404",
			res.Status, res.Code)
	}

	listed := api.do(http.MethodGet, "/api/v1/directories", second.token, nil)
	if strings.Contains(string(listed.Data), "Ours") {
		t.Error("another tenant's directory appears in this tenant's listing")
	}
}
