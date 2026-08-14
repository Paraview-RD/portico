package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/service"
)

// Signing in through somebody else's provider.
//
// The parts worth testing without one to talk to are the refusals, and they
// are most of the security: a configuration that points inside the network,
// a callback nobody started, and a first-time arrival that nothing links to
// an account. The exchange itself is the library's and is exercised against
// a real provider in the walkthrough rather than mocked here — a mock that
// returns whatever the code asks for would assert that the code agrees with
// itself.

func TestAnIssuerInsideTheNetworkIsRefusedAtTheForm(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, issuer := range []string{
		"http://accounts.example.com",
		"https://127.0.0.1:8443",
		"https://169.254.169.254/latest/meta-data",
		"https://10.1.2.3/oidc",
	} {
		res := api.do(http.MethodPost, "/api/v1/external-identity-providers", admin,
			map[string]any{"name": "bad", "issuer": issuer, "clientId": "portico"})
		if res.Status == http.StatusOK {
			t.Errorf("POST accepted issuer %q; a tenant administrator could then "+
				"make this server fetch addresses inside its own network", issuer)
		}
	}
}

// The sign-in screen may ask what buttons to draw before anybody has signed
// in, and gets labels and nothing else.
func TestTheSignInScreenLearnsOnlyWhatAButtonSays(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodGet, "/api/v1/auth/external/providers", "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("providers = %d %s, want 200", res.Status, res.Code)
	}

	// A tenant with none configured answers an empty list rather than an
	// error: the screen asks unconditionally and draws nothing.
	var options []map[string]any
	if err := json.Unmarshal(res.Data, &options); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, option := range options {
		for _, leaked := range []string{"issuer", "clientId", "clientSecret", "trustVerifiedEmail"} {
			if _, ok := option[leaked]; ok {
				t.Errorf("the public provider list carries %q; that is configuration, "+
					"and this endpoint answers before anybody has proved anything", leaked)
			}
		}
	}
}

// A callback nobody started is refused, and so is one replayed.
func TestACallbackWithAStateNobodyIssuedIsRefused(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodGet,
		"/api/v1/auth/external/callback?state=invented&code=whatever", "", nil)
	if res.Code != "EXTERNAL_STATE_UNKNOWN" {
		t.Fatalf("callback with an invented state = %d %s, want EXTERNAL_STATE_UNKNOWN",
			res.Status, res.Code)
	}
}

// Starting a sign-in through a provider that does not exist says so rather
// than sending somebody to a blank address.
func TestStartingWithAnUnknownProviderIsRefused(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodPost, "/api/v1/auth/external/start", "",
		map[string]string{"provider": "no-such-provider"})
	if res.Status != http.StatusNotFound || res.Code != "EXTERNAL_IDP_NOT_FOUND" {
		t.Fatalf("start with an unknown provider = %d %s, want 404 EXTERNAL_IDP_NOT_FOUND",
			res.Status, res.Code)
	}
}

// The address handed to a provider must land on something a person can
// look at.
//
// It is a redirect target, which means a top-level navigation: whatever
// answers it is what somebody sees after coming back from Google. The API
// endpoint that completes the sign-in answers JSON, so the address is a
// console route instead, and the console spends the state and code from
// there. Point it back at /api/ and every sign-in ends on a page of JSON —
// working, and unusable.
//
// This asserts on the router rather than on a built front end, so it holds
// whether or not `web/dist` exists: a request the API router claims answers
// ROUTE_NOT_FOUND in the envelope, and one it lets through does not.
func TestTheAddressGivenToAProviderIsNotAnAPIEndpoint(t *testing.T) {
	api := newAPITest(t)

	if strings.HasPrefix(service.ExternalCallbackPath, "/api/") {
		t.Fatalf("the redirect path is %q, which the API router owns; a browser "+
			"following it would be shown the JSON envelope",
			service.ExternalCallbackPath)
	}

	for _, path := range []string{
		service.ExternalCallbackPath,
		"/t/acme" + service.ExternalCallbackPath,
	} {
		rec := httptest.NewRecorder()
		api.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if strings.Contains(rec.Body.String(), "ROUTE_NOT_FOUND") {
			t.Errorf("GET %s is answered by the API router; the console never "+
				"gets the callback and the sign-in ends on an error page", path)
		}
	}
}

// An ordinary account may see and manage its own links, and may not
// configure providers. Both halves matter: the first is what the profile
// screen needs, and the second is that configuring who may vouch for
// accounts is an administrator's decision.
func TestLinksAreTheirOwnersAndProvidersAreTheAdministrators(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "ordinary", "ordinary-password-1", "USER")
	token := api.login("ordinary", "ordinary-password-1")

	res := api.do(http.MethodGet, "/api/v1/users/me/external-identities", token, nil)
	if res.Status != http.StatusOK {
		t.Errorf("an account reading its own links = %d %s, want 200", res.Status, res.Code)
	}

	res = api.do(http.MethodGet, "/api/v1/external-identity-providers", token, nil)
	if res.Status != http.StatusForbidden {
		t.Errorf("an ordinary account listing providers = %d %s, want 403",
			res.Status, res.Code)
	}
}

// WeChat and DingTalk need no issuer and must not accept one.
//
// The issuer is what every stored identity is namespaced under, and for
// these two it is a constant. Letting an administrator type one would let
// two tenants disagree about what "WeChat" means, after which the same
// person is two identities — the one thing an identity must not be.
func TestAVendorProviderTakesItsIssuerFromTheCodeAndNotTheForm(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/external-identity-providers", admin,
		map[string]any{
			"name": "微信", "kind": "WECHAT",
			"clientId": "wx-appid",
			// Offered, and expected to be ignored rather than honoured.
			"issuer": "https://not-wechat.example.com",
		})
	if res.Status != http.StatusOK {
		t.Fatalf("create a WeChat provider = %d %s, want 200", res.Status, res.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["issuer"] != "https://open.weixin.qq.com" {
		t.Errorf("issuer = %v, want the constant; an issuer somebody typed "+
			"means two tenants can disagree about what WeChat is",
			created["issuer"])
	}
	if created["kind"] != "WECHAT" {
		t.Errorf("kind = %v, want WECHAT — stored as OIDC it would be sent "+
			"through a discovery it has no document for", created["kind"])
	}
}

// A provider with no discovery document is saved without being contacted.
//
// The OIDC path proves an issuer resolves before writing the row. There is
// no equivalent question to ask WeChat, so this must not fail the way an
// unreachable issuer does — which is the whole reason a test names it.
func TestAVendorProviderIsSavedWithoutReachingTheVendor(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, kind := range []string{"WECHAT", "DINGTALK"} {
		res := api.do(http.MethodPost, "/api/v1/external-identity-providers", admin,
			map[string]any{
				"name": kind, "kind": kind, "clientId": kind + "-id",
			})
		if res.Status != http.StatusOK {
			t.Errorf("create %s = %d %s; these have no discovery document, so "+
				"a save that requires reaching them can never succeed on a "+
				"network that cannot", kind, res.Status, res.Code)
		}
	}
}

// The kind cannot be edited afterwards.
func TestAProvidersKindIsFixedOnceIdentitiesCanBeBoundToIt(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/external-identity-providers", admin,
		map[string]any{"name": "钉钉", "kind": "DINGTALK", "clientId": "ding-id"})
	if res.Status != http.StatusOK {
		t.Fatalf("create = %d %s", res.Status, res.Code)
	}
	var created map[string]any
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	edit := api.do(http.MethodPut,
		"/api/v1/external-identity-providers/"+created["id"].(string), admin,
		map[string]any{
			"name": "钉钉", "kind": "WECHAT", "clientId": "ding-id",
		})
	if edit.Status != http.StatusOK {
		t.Fatalf("edit = %d %s", edit.Status, edit.Code)
	}
	var edited map[string]any
	if err := json.Unmarshal(edit.Data, &edited); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if edited["kind"] != "DINGTALK" {
		t.Errorf("kind became %v; every identity already bound would keep its "+
			"(issuer, subject) while both halves changed meaning, and the "+
			"first sign of it is somebody told their account is not linked",
			edited["kind"])
	}
}

// A kind nobody implements is refused rather than stored and met later.
func TestAKindThisVersionDoesNotSpeakIsRefused(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/external-identity-providers", admin,
		map[string]any{"name": "QQ", "kind": "QQ", "clientId": "qq-id"})
	if res.Code != "EXTERNAL_IDP_KIND_UNKNOWN" {
		t.Errorf("create with an unknown kind = %d %s, want "+
			"EXTERNAL_IDP_KIND_UNKNOWN", res.Status, res.Code)
	}
}
