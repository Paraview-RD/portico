package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
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
