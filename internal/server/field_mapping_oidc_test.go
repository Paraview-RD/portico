package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Mappings, over a real sign-in.
//
// internal/service proves what a rule decides. This proves the decision
// reaches the wire — which is a different claim, and the one an integrator
// cares about. It signs in for real, reads the ID token a relying party would
// read, and then reads the userinfo document for the same session.
//
// That second read is not incidental. The client is recoverable when a token
// is issued and not when userinfo is called, so the two disagree today. This
// pins that disagreement rather than leaving it to be found.

// mappedSignIn registers a client, configures it, signs somebody in, and
// returns the ID token's claims alongside the userinfo document.
func mappedSignIn(t *testing.T, f *federationTest, admin, username, password string,
	rules []map[string]any,
) (idToken map[string]any, userinfo map[string]any) {
	t.Helper()

	tenant, err := f.tenants.Resolve(context.Background(), model.DefaultTenantCode)
	if err != nil {
		t.Fatalf("resolve tenant: %v", err)
	}
	registered, err := f.clients.Register(context.Background(),
		service.CommandLineActor(tenant.ID), service.RegisterClientInput{
			ClientID:     "mapped-client",
			Name:         "Mapped",
			RedirectURIs: []string{"http://127.0.0.1:9999/callback"},
			Scopes:       []string{"openid", "profile", "email", "phone"},
		})
	if err != nil {
		t.Fatalf("register client: %v", err)
	}

	res := f.api.do(http.MethodPut,
		"/api/v1/applications/oauth-clients/"+registered.Client.ClientID+"/field-mappings",
		admin, map[string]any{"mappings": rules})
	if res.Status != http.StatusOK {
		t.Fatalf("save mappings: %d %s %s", res.Status, res.Code, res.Message)
	}

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party, err := rp.NewRelyingPartyOIDC(context.Background(), issuer,
		registered.Client.ClientID, registered.Secret,
		"http://127.0.0.1:9999/callback",
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone})
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}

	verifier := "a-code-verifier-long-enough-to-be-respectable"
	authURL := rp.AuthURL("state-mapped", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)))
	code, _ := f.signIn(authURL, model.DefaultTenantCode, username, password)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	idToken = map[string]any{}
	for name, value := range tokens.IDTokenClaims.Claims {
		idToken[name] = value
	}
	// The typed claims live beside the private ones rather than in the map,
	// so the two that a rename moves between them are copied in explicitly.
	if tokens.IDTokenClaims.Email != "" {
		idToken["email"] = string(tokens.IDTokenClaims.Email)
	}
	if tokens.IDTokenClaims.PhoneNumber != "" {
		idToken["phone_number"] = tokens.IDTokenClaims.PhoneNumber
	}

	return idToken, f.rawUserinfo(issuer, tokens.AccessToken)
}

// A rule reaches the ID token: renamed, suppressed, and added.
//
// One sign-in covering all three, because they interact — an addition landing
// while a suppression did not would look like a working feature.
func TestAConfiguredClientReceivesTheNamesItWasGiven(t *testing.T) {
	f := newFederationTest(t)
	admin := f.api.adminToken()

	var org struct {
		ID string `json:"id"`
	}
	res := f.api.do(http.MethodPost, "/api/v1/organizations", admin, map[string]any{
		"name": "Mapped Org", "code": "MAPPEDORG",
	})
	res.into(t, &org)

	const username = "mapped.person"
	const password = "mapped-password-1"
	if res := f.api.do(http.MethodPost, "/api/v1/users", admin, map[string]any{
		"username": username, "displayName": "Mapped Person",
		"email": "mapped@example.test", "phone": "+442079460102",
		"password": password, "role": "USER", "organizationId": org.ID,
	}); res.Status != http.StatusOK {
		t.Fatalf("create the account: %d %s %s", res.Status, res.Code, res.Message)
	}

	idToken, userinfo := mappedSignIn(t, f, admin, username, password, []map[string]any{
		{"sourceKey": "email", "targetName": "mail"},
		{"sourceKey": "phone", "suppressed": true},
		{"sourceKey": "organization_code", "targetName": "orgCode"},
	})

	if idToken["mail"] != "mapped@example.test" {
		t.Errorf("the renamed claim is %#v, want the address under `mail`", idToken["mail"])
	}
	if _, still := idToken["email"]; still {
		t.Error("the address arrived under `email` as well, so the rename sent it twice")
	}
	if _, still := idToken["email_verified"]; still {
		t.Error("email_verified was sent for a claim that is no longer called email; " +
			"a relying party reading `mail` has no reason to look at it")
	}
	if _, still := idToken["phone_number"]; still {
		t.Error("a suppressed claim reached the ID token, so a disclosure decision " +
			"was not applied")
	}
	// An addition: the organization code is in the catalogue, is resolved from
	// the tree, and reaches no application by default.
	if idToken["orgCode"] != "MAPPEDORG" {
		t.Errorf("the added claim is %#v, want the organization code under `orgCode`",
			idToken["orgCode"])
	}

	// And the userinfo endpoint agrees, which it did not when this feature
	// first shipped: the client is not a parameter there, so it is carried in
	// the access token's id. A relying party that reads a claim from the ID
	// token and the same claim from userinfo has to see one answer, and a
	// suppression is somebody's decision that has to hold wherever the field
	// could otherwise come out.
	if userinfo["mail"] != "mapped@example.test" {
		t.Errorf("userinfo returned mail=%#v, want the renamed claim to match the "+
			"ID token", userinfo["mail"])
	}
	if _, still := userinfo["email"]; still {
		t.Error("userinfo still sends the address as `email`, so the rename applies " +
			"to the ID token and not to userinfo")
	}
	if _, still := userinfo["phone_number"]; still {
		t.Error("userinfo still sends a suppressed field, so an application told not " +
			"to receive it can get it by asking a different endpoint")
	}
	if userinfo["orgCode"] != "MAPPEDORG" {
		t.Errorf("userinfo returned orgCode=%#v, want the added claim", userinfo["orgCode"])
	}
}
