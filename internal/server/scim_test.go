package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// SCIM, driven the way a provisioning system drives it: a bearer token, the
// SCIM media type, and SCIM's own error shape on the way back.

// scimClient issues requests against /scim/v2 with a credential.
type scimClient struct {
	api   *apiTest
	token string
}

// newSCIMClient creates a credential through the console API, which is the
// only way to get one — there is no seeding shortcut, deliberately, because a
// test that bypassed issuance would not notice if issuance broke.
func newSCIMClient(t *testing.T, api *apiTest, name string) *scimClient {
	t.Helper()

	resp := api.do(http.MethodPost, "/api/v1/scim-credentials", api.adminToken(),
		map[string]string{"name": name})
	if resp.Status != http.StatusOK {
		t.Fatalf("issue scim credential: status %d, code %s", resp.Status, resp.Code)
	}

	var issued struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Data, &issued); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("no token in the response; a credential nobody can use")
	}
	return &scimClient{api: api, token: issued.Token}
}

// filterPath builds a filtered listing URL.
//
// The filter goes through url.QueryEscape because a SCIM filter contains
// spaces and quotes, and a real client encodes them — testing with a raw
// string would exercise a request no client sends.
func filterPath(filter string) string {
	return "/Users?filter=" + url.QueryEscape(filter)
}

type scimResponse struct {
	Status int
	Body   []byte
}

func (r scimResponse) decode(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, into); err != nil {
		t.Fatalf("decode scim response: %v (body=%s)", err, r.Body)
	}
}

// scimType reads the machine-readable error reason.
func (r scimResponse) scimType(t *testing.T) string {
	t.Helper()
	var body struct {
		ScimType string `json:"scimType"`
		Detail   string `json:"detail"`
	}
	r.decode(t, &body)
	return body.ScimType
}

func (c *scimClient) do(t *testing.T, method, path string, body any) scimResponse {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, "/scim/v2"+path, reader)
	req.Header.Set("Content-Type", "application/scim+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	rec := httptest.NewRecorder()
	c.api.srv.Handler().ServeHTTP(rec, req)
	return scimResponse{Status: rec.Code, Body: rec.Body.Bytes()}
}

// --- The consistency check -------------------------------------------
//
// A partial SCIM implementation fails by advertising more than it does: an
// identity provider reads the discovery endpoints to decide what its own
// configuration screen offers, and an administrator finds out the difference
// when a push half-works. This is the one property checkable without a real
// Okta tenant, so it is checked.

func TestAdvertisedCapabilitiesMatchTheImplementation(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "consistency")

	var config struct {
		Patch          struct{ Supported bool } `json:"patch"`
		Bulk           struct{ Supported bool } `json:"bulk"`
		Filter         struct{ Supported bool } `json:"filter"`
		ChangePassword struct{ Supported bool } `json:"changePassword"`
	}
	client.do(t, http.MethodGet, "/ServiceProviderConfig", nil).decode(t, &config)

	// Every advertised capability must have a handler that does not answer
	// "not implemented".
	if config.Patch.Supported {
		user := client.createUser(t, "patch-capability", "ext-patch-capability")
		resp := client.do(t, http.MethodPatch, "/Users/"+user.ID, patchOp("replace", "active", false))
		if resp.Status == http.StatusNotImplemented || resp.Status >= 500 {
			t.Errorf("patch is advertised as supported but answered %d", resp.Status)
		}
	}
	if config.Filter.Supported {
		resp := client.do(t, http.MethodGet, filterPath(`userName eq "nobody"`), nil)
		if resp.Status != http.StatusOK {
			t.Errorf("filter is advertised as supported but answered %d", resp.Status)
		}
	}
	// And the reverse: nothing unsupported may quietly work, because a
	// capability that works while advertised as absent is one an operator
	// will not configure and cannot rely on.
	if config.Bulk.Supported {
		t.Error("bulk is advertised but not implemented")
	}
	if config.ChangePassword.Supported {
		t.Error("changePassword is advertised; passwords are not settable over SCIM")
	}
}

func TestEveryAdvertisedResourceTypeHasARoute(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "resource-types")

	var listing struct {
		Resources []struct {
			Name     string `json:"name"`
			Endpoint string `json:"endpoint"`
		} `json:"Resources"`
	}
	client.do(t, http.MethodGet, "/ResourceTypes", nil).decode(t, &listing)

	if len(listing.Resources) == 0 {
		t.Fatal("no resource types advertised at all")
	}
	for _, rt := range listing.Resources {
		resp := client.do(t, http.MethodGet, rt.Endpoint, nil)
		if resp.Status == http.StatusNotFound {
			t.Errorf("resource type %s advertises %s, which does not exist",
				rt.Name, rt.Endpoint)
		}
	}
}

// --- Authentication ---------------------------------------------------

func TestSCIMRefusesEverythingWithoutACredential(t *testing.T) {
	api := newAPITest(t)
	anonymous := &scimClient{api: api}

	for _, path := range []string{"/Users", "/ServiceProviderConfig", "/ResourceTypes", "/Schemas"} {
		resp := anonymous.do(t, http.MethodGet, path, nil)
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("GET %s without a token returned %d, want 401", path, resp.Status)
		}
	}
}

func TestASessionTokenIsNotASCIMCredential(t *testing.T) {
	api := newAPITest(t)

	// An administrator's own token must not work here. The two are different
	// kinds of principal, and accepting a session token would mean anybody
	// who phished one could drive the provisioning API.
	client := &scimClient{api: api, token: api.adminToken()}
	if resp := client.do(t, http.MethodGet, "/Users", nil); resp.Status != http.StatusUnauthorized {
		t.Errorf("a console session token was accepted for SCIM (status %d)", resp.Status)
	}
}

func TestADisabledCredentialStopsWorking(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	resp := api.do(http.MethodPost, "/api/v1/scim-credentials", admin,
		map[string]string{"name": "to-be-disabled"})
	var issued struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Data, &issued); err != nil {
		t.Fatalf("decode credential: %v", err)
	}

	client := &scimClient{api: api, token: issued.Token}
	if got := client.do(t, http.MethodGet, "/Users", nil); got.Status != http.StatusOK {
		t.Fatalf("a fresh credential did not work: %d", got.Status)
	}

	api.do(http.MethodPost, "/api/v1/scim-credentials/"+issued.ID+"/disable", admin, nil)

	if got := client.do(t, http.MethodGet, "/Users", nil); got.Status != http.StatusUnauthorized {
		t.Errorf("a disabled credential still works (status %d). Disabling is the "+
			"reversible half of revocation and has to take effect immediately.", got.Status)
	}
}

func TestTheTokenIsShownOnceAndNeverStored(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	resp := api.do(http.MethodPost, "/api/v1/scim-credentials", admin,
		map[string]string{"name": "shown-once"})
	var issued struct{ Token string }
	if err := json.Unmarshal(resp.Data, &issued); err != nil {
		t.Fatalf("decode: %v", err)
	}

	listing := api.do(http.MethodGet, "/api/v1/scim-credentials", admin, nil)
	if bytes.Contains(listing.Data, []byte(issued.Token)) {
		t.Error("the listing returned the token. What is stored is a digest, " +
			"so that a database dump is not a set of working credentials — " +
			"returning it here would undo that.")
	}
}

// --- Provisioning behaviour -------------------------------------------

// createUser provisions an account and returns it.
func (c *scimClient) createUser(t *testing.T, userName, externalID string) struct {
	ID       string `json:"id"`
	UserName string `json:"userName"`
	Active   bool   `json:"active"`
} {
	t.Helper()
	resp := c.do(t, http.MethodPost, "/Users", map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName":   userName,
		"externalId": externalID,
		"active":     true,
		"emails":     []map[string]any{{"value": userName + "@example.test", "primary": true}},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("provision %s: status %d, body %s", userName, resp.Status, resp.Body)
	}
	var user struct {
		ID       string `json:"id"`
		UserName string `json:"userName"`
		Active   bool   `json:"active"`
	}
	resp.decode(t, &user)
	return user
}

func patchOp(op, path string, value any) map[string]any {
	return map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": op, "path": path, "value": value},
		},
	}
}

func TestProvisioningIsIdempotentOnExternalID(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "idempotent")

	client.createUser(t, "reconcile-me", "ext-reconcile")

	// The same externalId with a *different* userName, which is what a
	// directory sends after somebody is renamed. Re-posting the identical
	// body proves nothing: the username uniqueness constraint would refuse a
	// duplicate anyway, so the test would pass with the reconciliation
	// removed. This is the case where the externalId lookup is the only
	// thing standing between one account and two.
	resp := client.do(t, http.MethodPost, "/Users", map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName":   "reconcile-me-renamed",
		"externalId": "ext-reconcile",
		"active":     true,
	})
	if resp.Status >= 400 {
		t.Fatalf("re-provisioning a renamed account answered %d: %s", resp.Status, resp.Body)
	}

	var listing struct {
		TotalResults int `json:"totalResults"`
		Resources    []struct {
			UserName string `json:"userName"`
		} `json:"Resources"`
	}
	client.do(t, http.MethodGet, filterPath(`externalId eq "ext-reconcile"`), nil).
		decode(t, &listing)

	if listing.TotalResults != 1 {
		t.Fatalf("externalId ext-reconcile matches %d accounts, want 1: the "+
			"directory has been duplicated", listing.TotalResults)
	}

	// And the rename was applied rather than silently dropped.
	if got := listing.Resources[0].UserName; got != "reconcile-me-renamed" {
		t.Errorf("userName = %q, want the renamed one: reconciliation matched "+
			"the account but did not update it", got)
	}
}

func TestDeleteAndPatchActiveFalseBothDeprovision(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "deprovision")

	viaDelete := client.createUser(t, "leaver-delete", "ext-leaver-delete")
	viaPatch := client.createUser(t, "leaver-patch", "ext-leaver-patch")

	if resp := client.do(t, http.MethodDelete, "/Users/"+viaDelete.ID, nil); resp.Status != http.StatusNoContent {
		t.Fatalf("DELETE returned %d", resp.Status)
	}
	if resp := client.do(t, http.MethodPatch, "/Users/"+viaPatch.ID,
		patchOp("replace", "active", false)); resp.Status != http.StatusOK {
		t.Fatalf("PATCH active=false returned %d", resp.Status)
	}

	// Both must land in the same state. Deprovisioning that works one way and
	// not the other is a difference nobody notices until somebody leaves.
	for _, user := range []string{viaDelete.ID, viaPatch.ID} {
		var got struct {
			Active bool `json:"active"`
		}
		client.do(t, http.MethodGet, "/Users/"+user, nil).decode(t, &got)
		if got.Active {
			t.Errorf("account %s is still active after deprovisioning", user)
		}
	}
}

func TestDeleteDisablesRatherThanRemoving(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "disable-not-delete")

	user := client.createUser(t, "still-here", "ext-still-here")
	client.do(t, http.MethodDelete, "/Users/"+user.ID, nil)

	// The documented deviation: accounts are disabled, never deleted, so the
	// audit trail keeps naming something that exists. The resource therefore
	// stays readable — a client that treats 404 as "gone" would otherwise
	// recreate the account on its next sync.
	resp := client.do(t, http.MethodGet, "/Users/"+user.ID, nil)
	if resp.Status != http.StatusOK {
		t.Errorf("GET after DELETE returned %d; the account should still exist, disabled", resp.Status)
	}
}

func TestUnsupportedPatchPathIsInvalidPathNot501(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "patch-paths")

	user := client.createUser(t, "patch-target", "ext-patch-target")
	resp := client.do(t, http.MethodPatch, "/Users/"+user.ID,
		patchOp("replace", "title", "Head of Nothing"))

	// RFC 7644 §3.5.2. A 501 tells an administrator the server is broken; a
	// 400 with invalidPath names the attribute in their sync log, where they
	// are already looking.
	if resp.Status != http.StatusBadRequest {
		t.Errorf("patching an unsupported path returned %d, want 400", resp.Status)
	}
	if got := resp.scimType(t); got != "invalidPath" {
		t.Errorf("scimType = %q, want invalidPath", got)
	}
}

func TestPatchIsAllOrNothing(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "atomic-patch")

	user := client.createUser(t, "atomic", "ext-atomic")

	// Two operations, the second unsupported. The first must not be applied:
	// a client that gets an error will retry the whole body, and a partially
	// applied patch is a state neither side knows about.
	resp := client.do(t, http.MethodPatch, "/Users/"+user.ID, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": "replace", "path": "displayName", "value": "Changed"},
			{"op": "replace", "path": "title", "value": "Nope"},
		},
	})
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("expected the patch to be refused, got %d", resp.Status)
	}

	var got struct {
		DisplayName string `json:"displayName"`
	}
	client.do(t, http.MethodGet, "/Users/"+user.ID, nil).decode(t, &got)
	if got.DisplayName == "Changed" {
		t.Error("the first operation was applied even though the patch failed")
	}
}

func TestFilterIsEqualityNotSubstring(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "filter-exact")

	client.createUser(t, "bob", "ext-bob")
	client.createUser(t, "bobby", "ext-bobby")

	var listing struct {
		TotalResults int `json:"totalResults"`
		Resources    []struct {
			UserName string `json:"userName"`
		} `json:"Resources"`
	}
	client.do(t, http.MethodGet, filterPath(`userName eq "bob"`), nil).decode(t, &listing)

	// "does bob exist" must not be true because bobby does — a reconciling
	// client would conclude the account is there and never create it.
	if listing.TotalResults != 1 || listing.Resources[0].UserName != "bob" {
		t.Errorf("filter matched %d accounts (%v), want exactly bob",
			listing.TotalResults, listing.Resources)
	}
}

func TestAnUnsupportedFilterIsRefusedRatherThanIgnored(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "filter-refused")
	client.createUser(t, "somebody", "ext-somebody")

	// A silently ignored filter returns everybody, and a client that asked
	// "does this user exist" and received the whole directory concludes yes.
	resp := client.do(t, http.MethodGet, filterPath(`title eq "Manager"`), nil)
	if resp.Status != http.StatusBadRequest {
		t.Errorf("an unsupported filter returned %d, want 400", resp.Status)
	}
}

func TestTheTenantComesFromTheCredentialNotTheRequest(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "tenancy")

	user := client.createUser(t, "tenant-bound", "ext-tenant-bound")

	// SCIM is the first authenticated surface where the tenant does not come
	// from an auth.Principal, so the guards that cover the rest of the API do
	// not cover it. A header that was honoured here would be a cross-tenant
	// write with a valid token, and nothing else in the codebase would catch
	// it — hence this test rather than trust in the code reading correctly.
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/"+user.ID, nil)
	req.Header.Set("Authorization", "Bearer "+client.token)
	req.Header.Set("X-Portico-Tenant", "some-other-tenant")

	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("a tenant header changed the request's outcome (status %d). "+
			"The credential decides the tenant; nothing on the request may.", rec.Code)
	}
}

func TestDeprovisioningEndsLiveAccessImmediately(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "revocation")
	admin := api.adminToken()

	user := client.createUser(t, "about-to-leave", "ext-about-to-leave")

	// A provisioned account has a random password it never learns, so give it
	// one the way an administrator would, and sign in with it. The point is to
	// have a live session at the moment the directory deprovisions.
	const password = "leaver-password-123"
	resp := api.do(http.MethodPost, "/api/v1/users/"+user.ID+"/password", admin,
		map[string]string{"newPassword": password})
	if resp.Status != http.StatusOK {
		t.Fatalf("set password: %d %s", resp.Status, resp.Code)
	}

	token := api.login("about-to-leave", password)
	if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusOK {
		t.Fatalf("the account could not use its session before deprovisioning: %d", got.Status)
	}

	// This is the whole point of the integration: somebody leaves the
	// directory and loses access now, not when their token happens to expire.
	if got := client.do(t, http.MethodDelete, "/Users/"+user.ID, nil); got.Status != http.StatusNoContent {
		t.Fatalf("DELETE returned %d", got.Status)
	}

	if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusUnauthorized {
		t.Errorf("a deprovisioned account's session still works (status %d). "+
			"Access surviving deprovisioning is the failure this integration "+
			"exists to prevent.", got.Status)
	}

	// And the session rows are revoked, not merely made unusable by the
	// status check.
	//
	// This half is asserted separately because the check above passes either
	// way: the middleware reads the account's status on every request, so a
	// disabled account is refused whether or not its sessions were revoked.
	// Removing the revocation would leave that test green while the session
	// list still showed a live session for somebody who has left — and would
	// leave the whole thing resting on one check rather than two, which is
	// not what the console's own disable does.
	sessions := api.do(http.MethodGet, "/api/v1/users/"+user.ID+"/sessions", admin, nil)
	if sessions.Status != http.StatusOK {
		t.Fatalf("list sessions: %d", sessions.Status)
	}
	var live []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(sessions.Data, &live); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("%d session(s) still live after deprovisioning; disabling an "+
			"account revokes them, and a directory removing somebody must do "+
			"the same rather than relying on the status check alone", len(live))
	}
}
