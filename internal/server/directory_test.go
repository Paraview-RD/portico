package server_test

import (
	"context"
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

// The synchronization interval survives the round trip, and an interval the
// server will not honour is refused at the door.
//
// The schedule's behaviour is tested in internal/service; what is only visible
// here is the wiring, which has no other guard. A request field that never
// reaches the service reads as a saved setting that quietly does nothing —
// somebody would configure a nightly synchronization, see it accepted, and
// find out weeks later that nothing ever ran.
func TestASynchronizationIntervalIsStoredAndBounded(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	created := registerDirectory(t, api, admin, "Nightly", map[string]any{
		"syncIntervalMinutes": 1440,
	})
	if created.Status != http.StatusOK {
		t.Fatalf("register: %d %s %s", created.Status, created.Code, created.Message)
	}

	var source struct {
		ID                  string `json:"id"`
		SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
	}
	created.into(t, &source)
	if source.SyncIntervalMinutes != 1440 {
		t.Errorf("registration returned an interval of %d, want 1440; the field "+
			"is not reaching the service", source.SyncIntervalMinutes)
	}

	read := api.do(http.MethodGet, "/api/v1/directories/"+source.ID, admin, nil)
	read.into(t, &source)
	if source.SyncIntervalMinutes != 1440 {
		t.Errorf("stored interval = %d, want 1440", source.SyncIntervalMinutes)
	}

	// Under the floor, and refused rather than rounded up: an operator who
	// asked for five minutes and was silently given fifteen would go on
	// believing the directory is read three times as often as it is.
	refused := registerDirectory(t, api, admin, "Too eager", map[string]any{
		"syncIntervalMinutes": 5,
	})
	if refused.Code != "INVALID_SYNC_INTERVAL" {
		t.Errorf("a five-minute interval = %d %s, want INVALID_SYNC_INTERVAL",
			refused.Status, refused.Code)
	}

	// Omitted means off, which is what keeps an integration written against
	// the previous version from acquiring a schedule by being upgraded.
	silent := registerDirectory(t, api, admin, "Manual", nil)
	silent.into(t, &source)
	if source.SyncIntervalMinutes != 0 {
		t.Errorf("a directory registered without mentioning the interval got %d, want 0",
			source.SyncIntervalMinutes)
	}
}

// The scheduler's entry point runs, across tenants, with the directory
// service actually attached to the server.
//
// Everything about what a scheduled run does is tested in internal/service.
// What only exists here is the wiring, and it is the kind that fails silently:
// the field is set in New, the goroutine that calls this is the only caller,
// and nothing else depends on it — so a missing dependency would be a nil
// receiver panicking on the first tick of a background goroutine, with the
// server otherwise serving happily. Removing `directories:` from New still
// compiles and still passes every other test in this package.
//
// The directory here is disabled, so nothing dials anything: the assertion is
// that the pass completes, not that it synchronizes.
func TestTheScheduledSynchronizationPassRuns(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	if err := api.srv.SyncDirectories(context.Background()); err != nil {
		t.Fatalf("a pass with nothing configured: %v", err)
	}

	created := registerDirectory(t, api, admin, "Scheduled", map[string]any{
		"syncIntervalMinutes": 15,
	})
	var source struct {
		ID string `json:"id"`
	}
	created.into(t, &source)
	if res := api.do(http.MethodPost, "/api/v1/directories/"+source.ID+"/disable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Code)
	}

	if err := api.srv.SyncDirectories(context.Background()); err != nil {
		t.Fatalf("a pass with a scheduled directory: %v", err)
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
