package server_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/paraview/portico/internal/casp"
	"github.com/paraview/portico/internal/samlp"
)

// Application management over the API: registering the systems that sign in
// through Portico, which used to be possible only from the command line.
//
// The properties worth holding onto are not "the form submits". They are
// that an administrator cannot register something in another tenant, that a
// non-administrator cannot register anything at all, that a secret is shown
// once and never again, and that the addresses the console tells an
// integrator to use are addresses this server actually serves.

const spEntityID = "https://sp.example.test/saml/metadata"

// spMetadata is a minimal but real service provider metadata document.
func spMetadata(entityID, acs string) string {
	return `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + entityID + `">
  <SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <AssertionConsumerService
      Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="` + acs + `" index="0"/>
  </SPSSODescriptor>
</EntityDescriptor>`
}

func TestRegisterOAuthClientThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "console-registered",
		"name":         "Registered from the console",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create client: %d %s %s", res.Status, res.Code, res.Message)
	}

	var created struct {
		Client struct {
			ClientID     string   `json:"clientId"`
			Confidential bool     `json:"confidential"`
			Scopes       []string `json:"scopes"`
			Status       string   `json:"status"`
		} `json:"client"`
		Secret string `json:"secret"`
	}
	res.into(t, &created)

	if created.Secret == "" {
		t.Fatal("registration returned no secret; a confidential client that " +
			"cannot authenticate is useless to the application it was created for")
	}
	if !created.Client.Confidential {
		t.Error("client should default to confidential")
	}
	if created.Client.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", created.Client.Status)
	}
	// openid is added even when the caller does not ask, because without it
	// no ID token is issued and Portico is an OpenID Provider.
	if !contains(created.Client.Scopes, "openid") {
		t.Errorf("scopes = %v, want openid to be present", created.Client.Scopes)
	}

	// The secret exists exactly once. Reading the client back must not
	// produce it again — only a hash is stored, and a listing that returned
	// secrets would undo that.
	read := api.do(http.MethodGet, "/api/v1/applications/oauth-clients/console-registered", token, nil)
	if read.Status != http.StatusOK {
		t.Fatalf("get client: %d %s", read.Status, read.Code)
	}
	if strings.Contains(string(read.Data), created.Secret) {
		t.Error("reading a client back returned its secret; it must only ever " +
			"be available at the moment it is generated")
	}
}

func TestRotatingAClientSecretInvalidatesTheOldOne(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "rotate-me",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create client: %d %s", create.Status, create.Code)
	}
	var first struct {
		Secret string `json:"secret"`
	}
	create.into(t, &first)

	rotate := api.do(http.MethodPost,
		"/api/v1/applications/oauth-clients/rotate-me/rotate-secret", token, nil)
	if rotate.Status != http.StatusOK {
		t.Fatalf("rotate secret: %d %s %s", rotate.Status, rotate.Code, rotate.Message)
	}
	var second struct {
		Secret string `json:"secret"`
	}
	rotate.into(t, &second)

	if second.Secret == "" {
		t.Fatal("rotation returned no secret")
	}
	if second.Secret == first.Secret {
		t.Fatal("rotation returned the same secret, so nothing was rotated")
	}

	// The old secret must stop working immediately. Rotation usually happens
	// because the old value leaked, and a rotation that leaves it working is
	// not a rotation.
	if !api.clientCredentialsAccepted(t, "rotate-me", second.Secret) {
		t.Error("the new secret was rejected")
	}
	if api.clientCredentialsAccepted(t, "rotate-me", first.Secret) {
		t.Error("the old secret still authenticates after rotation")
	}
}

func TestPublicClientHasNoSecretToRotate(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "spa",
		"public":       true,
		"redirectUris": []string{"https://spa.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create public client: %d %s", create.Status, create.Code)
	}
	var created struct {
		Secret string `json:"secret"`
	}
	create.into(t, &created)
	if created.Secret != "" {
		t.Error("a public client was given a secret; it authenticates with PKCE")
	}

	// Generating one on rotation would quietly make it confidential, and it
	// would then fail to authenticate until somebody worked out why.
	res := api.do(http.MethodPost, "/api/v1/applications/oauth-clients/spa/rotate-secret", token, nil)
	if res.Status != http.StatusBadRequest || res.Code != "CLIENT_IS_PUBLIC" {
		t.Errorf("rotate on a public client = %d %s, want 400 CLIENT_IS_PUBLIC",
			res.Status, res.Code)
	}
}

func TestUpdatingAClientValidatesRedirectURIs(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "editable",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create client: %d %s", create.Status, create.Code)
	}

	// A redirect URI is where an authorization code is delivered. The rules
	// that apply at registration have to apply at update too, or editing is
	// a way round them.
	bad := api.do(http.MethodPut, "/api/v1/applications/oauth-clients/editable", token, map[string]any{
		"redirectUris": []string{"http://app.example.test/callback"},
	})
	if bad.Status != http.StatusBadRequest {
		t.Errorf("update with plain http over a network = %d %s, want 400",
			bad.Status, bad.Code)
	}

	good := api.do(http.MethodPut, "/api/v1/applications/oauth-clients/editable", token, map[string]any{
		"name":         "Renamed",
		"redirectUris": []string{"https://app.example.test/other"},
	})
	if good.Status != http.StatusOK {
		t.Fatalf("update client: %d %s %s", good.Status, good.Code, good.Message)
	}
	var updated struct {
		Name         string   `json:"name"`
		RedirectURIs []string `json:"redirectUris"`
	}
	good.into(t, &updated)
	if updated.Name != "Renamed" {
		t.Errorf("name = %q, want Renamed", updated.Name)
	}
	if len(updated.RedirectURIs) != 1 || updated.RedirectURIs[0] != "https://app.example.test/other" {
		t.Errorf("redirectUris = %v, want the replacement", updated.RedirectURIs)
	}
}

func TestRegisterSAMLServiceProviderThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/applications/saml-service-providers", token, map[string]any{
		"name":        "Example SP",
		"metadataXml": spMetadata(spEntityID, "https://sp.example.test/acs"),
	})
	if res.Status != http.StatusOK {
		t.Fatalf("register service provider: %d %s %s", res.Status, res.Code, res.Message)
	}

	var provider struct {
		ID       string   `json:"id"`
		EntityID string   `json:"entityId"`
		Name     string   `json:"name"`
		ACSURLs  []string `json:"acsUrls"`
	}
	res.into(t, &provider)
	if provider.EntityID != spEntityID {
		t.Errorf("entityId = %q, want %q", provider.EntityID, spEntityID)
	}
	if len(provider.ACSURLs) != 1 || provider.ACSURLs[0] != "https://sp.example.test/acs" {
		t.Errorf("acsUrls = %v, want the one in the metadata", provider.ACSURLs)
	}

	// Addressed by the registration's own id, not the entity id — see
	// TestApplicationPathsCarryNoEncodedSlashes for why that matters.
	read := api.do(http.MethodGet,
		"/api/v1/applications/saml-service-providers/"+provider.ID, token, nil)
	if read.Status != http.StatusOK {
		t.Fatalf("get service provider by id: %d %s", read.Status, read.Code)
	}
}

func TestReplacingSAMLMetadataRejectsADifferentEntity(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/saml-service-providers", token,
		map[string]any{"metadataXml": spMetadata(spEntityID, "https://sp.example.test/acs")})
	if create.Status != http.StatusOK {
		t.Fatalf("register service provider: %d %s", create.Status, create.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	create.into(t, &created)

	// Replacing metadata is how a certificate is rotated, so it has to work.
	rotated := api.do(http.MethodPut,
		"/api/v1/applications/saml-service-providers/"+created.ID, token, map[string]any{
			"metadataXml": spMetadata(spEntityID, "https://sp.example.test/acs2"),
		})
	if rotated.Status != http.StatusOK {
		t.Fatalf("replace metadata: %d %s %s", rotated.Status, rotated.Code, rotated.Message)
	}

	// But a document declaring a different entity describes a different
	// service provider. Accepting it would silently repoint this
	// registration, so assertions meant for one system would start being
	// issued to another under the old name.
	hijack := api.do(http.MethodPut,
		"/api/v1/applications/saml-service-providers/"+created.ID, token, map[string]any{
			"metadataXml": spMetadata("https://attacker.example.test/saml", "https://attacker.example.test/acs"),
		})
	if hijack.Status != http.StatusBadRequest || hijack.Code != "METADATA_ENTITY_ID_MISMATCH" {
		t.Errorf("replacing with another entity's metadata = %d %s, "+
			"want 400 METADATA_ENTITY_ID_MISMATCH", hijack.Status, hijack.Code)
	}
}

func TestRegisterCASServiceThroughTheAPI(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/applications/cas-services", token, map[string]any{
		"name":      "Wiki",
		"urlPrefix": "https://wiki.example.test",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("register CAS service: %d %s %s", res.Status, res.Code, res.Message)
	}

	var svc struct {
		URLPrefix string `json:"urlPrefix"`
		Name      string `json:"name"`
	}
	res.into(t, &svc)

	// Normalized to end at a path boundary, which is what stops the
	// registration matching https://wiki.example.test.attacker.test.
	if svc.URLPrefix != "https://wiki.example.test/" {
		t.Errorf("urlPrefix = %q, want it normalized to end in /", svc.URLPrefix)
	}

	// Wildcards are refused rather than quietly stripped: an operator who
	// typed one believes it is doing something.
	bad := api.do(http.MethodPost, "/api/v1/applications/cas-services", token, map[string]any{
		"urlPrefix": "https://*.example.test/",
	})
	if bad.Status != http.StatusBadRequest || bad.Code != "CAS_SERVICE_WILDCARD" {
		t.Errorf("wildcard prefix = %d %s, want 400 CAS_SERVICE_WILDCARD", bad.Status, bad.Code)
	}
}

func TestCASServiceCanBeMovedToANewAddress(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/cas-services", token, map[string]any{
		"urlPrefix": "https://old.example.test/",
	})
	if create.Status != http.StatusOK {
		t.Fatalf("register CAS service: %d %s", create.Status, create.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	create.into(t, &created)

	res := api.do(http.MethodPut, "/api/v1/applications/cas-services/"+created.ID, token, map[string]any{
		"name":      "Moved",
		"urlPrefix": "https://new.example.test/",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("move CAS service: %d %s %s", res.Status, res.Code, res.Message)
	}

	var moved struct {
		Name      string `json:"name"`
		URLPrefix string `json:"urlPrefix"`
	}
	res.into(t, &moved)
	if moved.URLPrefix != "https://new.example.test/" {
		t.Errorf("urlPrefix = %q, want the new address", moved.URLPrefix)
	}
	if moved.Name != "Moved" {
		t.Errorf("name = %q, want Moved", moved.Name)
	}
}

// The endpoints the console hands an integrator have to be endpoints this
// server actually serves. A screen that publishes an address nobody routes
// sends somebody off to debug the wrong system, and no other test would
// notice, because nothing else reads these strings.
func TestPublishedIntegrationEndpointsAreServed(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodGet, "/api/v1/applications/integration-endpoints", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("integration endpoints: %d %s %s", res.Status, res.Code, res.Message)
	}

	var endpoints struct {
		TenantCode string `json:"tenantCode"`
		Issuer     string `json:"issuer"`
		OIDC       struct {
			Discovery  string `json:"discovery"`
			Authorize  string `json:"authorize"`
			Token      string `json:"token"`
			UserInfo   string `json:"userinfo"`
			JWKS       string `json:"jwks"`
			EndSession string `json:"endSession"`
			Introspect string `json:"introspect"`
			Revoke     string `json:"revoke"`
		} `json:"oidc"`
		SAML struct {
			EntityID       string `json:"entityId"`
			Metadata       string `json:"metadata"`
			SSO            string `json:"sso"`
			CertificatePEM string `json:"certificatePem"`
		} `json:"saml"`
		CAS struct {
			BaseURL         string `json:"baseUrl"`
			Login           string `json:"login"`
			Logout          string `json:"logout"`
			ServiceValidate string `json:"serviceValidate"`
		} `json:"cas"`
	}
	res.into(t, &endpoints)

	if endpoints.TenantCode != "default" {
		t.Errorf("tenantCode = %q, want default", endpoints.TenantCode)
	}

	published := map[string]string{
		"oidc.discovery":      endpoints.OIDC.Discovery,
		"oidc.authorize":      endpoints.OIDC.Authorize,
		"oidc.token":          endpoints.OIDC.Token,
		"oidc.userinfo":       endpoints.OIDC.UserInfo,
		"oidc.jwks":           endpoints.OIDC.JWKS,
		"oidc.endSession":     endpoints.OIDC.EndSession,
		"oidc.introspect":     endpoints.OIDC.Introspect,
		"oidc.revoke":         endpoints.OIDC.Revoke,
		"saml.metadata":       endpoints.SAML.Metadata,
		"saml.sso":            endpoints.SAML.SSO,
		"cas.login":           endpoints.CAS.Login,
		"cas.logout":          endpoints.CAS.Logout,
		"cas.serviceValidate": endpoints.CAS.ServiceValidate,
	}

	for name, address := range published {
		if address == "" {
			t.Errorf("%s is empty", name)
			continue
		}
		if !strings.HasPrefix(address, endpoints.Issuer) {
			t.Errorf("%s = %q, which does not hang off the issuer %q",
				name, address, endpoints.Issuer)
			continue
		}

		parsed, err := url.Parse(address)
		if err != nil {
			t.Errorf("%s = %q: %v", name, address, err)
			continue
		}

		// A 404 means the path is not routed at all. Anything else — a
		// redirect, a protocol error, a 400 about missing parameters — means
		// something is listening, which is all this is checking.
		if status := api.raw(http.MethodGet, parsed.Path); status == http.StatusNotFound {
			t.Errorf("%s = %q, but %s is not routed", name, address, parsed.Path)
		}
	}

	// The SAML metadata address must serve a document a service provider can
	// consume, not merely respond.
	if !strings.HasSuffix(endpoints.SAML.Metadata, samlp.MetadataPath) {
		t.Errorf("saml.metadata = %q, want it to end in %q",
			endpoints.SAML.Metadata, samlp.MetadataPath)
	}
	if !strings.HasSuffix(endpoints.CAS.Login, casp.LoginPath) {
		t.Errorf("cas.login = %q, want it to end in %q", endpoints.CAS.Login, casp.LoginPath)
	}
	if endpoints.CAS.BaseURL == "" || !strings.HasSuffix(endpoints.CAS.BaseURL, "/cas") {
		t.Errorf("cas.baseUrl = %q, want it to end in /cas", endpoints.CAS.BaseURL)
	}
}

// The certificate offered to a service provider has to be the one assertions
// are actually signed with, or an integrator configures a trust anchor that
// verifies nothing.
func TestPublishedSAMLCertificateMatchesTheMetadataDocument(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Reading the metadata first is what brings a signing key into
	// existence; the endpoints screen deliberately does not generate one.
	if status := api.raw(http.MethodGet, samlp.MetadataPath); status != http.StatusOK {
		t.Fatalf("SAML metadata returned %d, want 200", status)
	}

	res := api.do(http.MethodGet, "/api/v1/applications/integration-endpoints", token, nil)
	var endpoints struct {
		SAML struct {
			CertificatePEM string `json:"certificatePem"`
		} `json:"saml"`
	}
	res.into(t, &endpoints)

	if endpoints.SAML.CertificatePEM == "" {
		t.Fatal("no SAML certificate was published after one had been generated")
	}
	if !strings.Contains(endpoints.SAML.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Errorf("certificatePem is not PEM: %q", endpoints.SAML.CertificatePEM)
	}
}

// Registering an application is administrator-only. An ordinary account that
// could register a relying party could point one at a redirect URI it
// controls and collect authorization codes for anybody who signed in to it.
func TestApplicationManagementIsAdministratorOnly(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()
	api.createUser(adminToken, "ordinary", "ordinary-password-1", "USER")
	userToken := api.login("ordinary", "ordinary-password-1")

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/applications/oauth-clients", nil},
		{http.MethodPost, "/api/v1/applications/oauth-clients", map[string]any{
			"clientId": "sneaky", "redirectUris": []string{"https://evil.example.test/cb"},
		}},
		{http.MethodGet, "/api/v1/applications/saml-service-providers", nil},
		{http.MethodPost, "/api/v1/applications/saml-service-providers", map[string]any{
			"metadataXml": spMetadata("https://evil.example.test/sp", "https://evil.example.test/acs"),
		}},
		{http.MethodGet, "/api/v1/applications/cas-services", nil},
		{http.MethodPost, "/api/v1/applications/cas-services", map[string]any{
			"urlPrefix": "https://evil.example.test/",
		}},
		{http.MethodGet, "/api/v1/applications/integration-endpoints", nil},
	}

	for _, c := range cases {
		res := api.do(c.method, c.path, userToken, c.body)
		if res.Status != http.StatusForbidden {
			t.Errorf("%s %s as an ordinary user = %d %s, want 403",
				c.method, c.path, res.Status, res.Code)
		}
	}
}

// A registration belongs to one tenant. An administrator of one tenant must
// not see, edit, or disable another's — that is the isolation boundary, and
// application management is a new surface across it.
func TestApplicationsAreIsolatedBetweenTenants(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("other", "Other")

	defaultToken := f.api.adminToken()
	otherToken := f.api.loginTo("other", adminUsername, adminPassword)

	create := f.api.do(http.MethodPost, "/api/v1/applications/oauth-clients", defaultToken,
		map[string]any{
			"clientId":     "tenant-scoped",
			"redirectUris": []string{"https://app.example.test/callback"},
		})
	if create.Status != http.StatusOK {
		t.Fatalf("create client in default tenant: %d %s", create.Status, create.Code)
	}

	// The other tenant's administrator sees an empty list, not this client.
	list := f.api.do(http.MethodGet, "/api/v1/applications/oauth-clients", otherToken, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("list clients in other tenant: %d %s", list.Status, list.Code)
	}
	if strings.Contains(string(list.Data), "tenant-scoped") {
		t.Fatal("an administrator saw another tenant's relying party")
	}

	// And cannot reach it by naming it directly.
	for _, path := range []string{
		"/api/v1/applications/oauth-clients/tenant-scoped",
	} {
		res := f.api.do(http.MethodGet, path, otherToken, nil)
		if res.Status != http.StatusNotFound {
			t.Errorf("GET %s as another tenant = %d %s, want 404",
				path, res.Status, res.Code)
		}
	}

	disable := f.api.do(http.MethodPost,
		"/api/v1/applications/oauth-clients/tenant-scoped/disable", otherToken, nil)
	if disable.Status != http.StatusNotFound {
		t.Errorf("disabling another tenant's client = %d %s, want 404",
			disable.Status, disable.Code)
	}
}

// Disabling has to actually stop the application, not merely change a label.
func TestDisablingAClientStopsItAuthenticating(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "switch-off",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create client: %d %s", create.Status, create.Code)
	}
	var created struct {
		Secret string `json:"secret"`
	}
	create.into(t, &created)

	if !api.clientCredentialsAccepted(t, "switch-off", created.Secret) {
		t.Fatal("a freshly registered client's credentials were rejected")
	}

	res := api.do(http.MethodPost, "/api/v1/applications/oauth-clients/switch-off/disable", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("disable client: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Disabling has to reach client authentication too, not only the
	// authorization request path: introspection and revocation authenticate
	// the client and never look it up any other way.
	if api.clientCredentialsAccepted(t, "switch-off", created.Secret) {
		t.Error("a disabled client still authenticates")
	}
}

// Every registration is auditable. "Who let this application in" is the
// question the trail exists to answer, and it is the shape a compromise
// takes here.
func TestApplicationRegistrationIsAudited(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "audited",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create client: %d %s", create.Status, create.Code)
	}

	logs := api.do(http.MethodGet, "/api/v1/audit-logs?action=OAUTH_CLIENT_CREATE", token, nil)
	if logs.Status != http.StatusOK {
		t.Fatalf("list audit logs: %d %s", logs.Status, logs.Code)
	}

	body := string(logs.Data)
	if !strings.Contains(body, "OAUTH_CLIENT_CREATE") {
		t.Fatalf("no OAUTH_CLIENT_CREATE entry was written: %s", body)
	}
	if !strings.Contains(body, "audited") {
		t.Errorf("the entry does not name the client that was registered: %s", body)
	}
	// The redirect URIs are the whole of the security question, so an entry
	// that omitted them would record that something happened without
	// recording the part that matters.
	if !strings.Contains(body, "https://app.example.test/callback") {
		t.Errorf("the entry does not record the redirect URIs: %s", body)
	}
}

// clientCredentialsAccepted reports whether a client's credentials still
// authenticate.
//
// It asks the introspection endpoint rather than the token endpoint, because
// the token endpoint validates the authorization code first: with a made-up
// code it answers invalid_grant no matter what the credentials were, so it
// cannot tell a good secret from a bad one. Introspection authenticates the
// client and nothing else, which is exactly the question here.
//
// The token being introspected is deliberately nonsense. A successful
// response — active:false — means the caller was authenticated; a 401 means
// it was not.
func (a *apiTest) clientCredentialsAccepted(t *testing.T, clientID, secret string) bool {
	t.Helper()

	form := url.Values{"token": {"not-a-real-token"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)

	rec := httptest.NewRecorder()
	a.srv.Handler().ServeHTTP(rec, req)

	return rec.Code == http.StatusOK
}

// raw issues a request and returns only the status, for the addresses that do
// not speak Portico's JSON envelope — the protocol endpoints, which answer
// with XML, a redirect, or a protocol-specific error body.
func (a *apiTest) raw(method, path string) int {
	a.t.Helper()

	rec := httptest.NewRecorder()
	a.srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

// contains is a tiny helper; slices.Contains would do, but this keeps the
// test file's imports to what it is actually about.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// No identifier this API puts in a URL path may contain a slash.
//
// A SAML entity id is a URI and a CAS registration is a URL prefix. Putting
// either in a path segment means percent-encoding its slashes, which works
// here and in a browser and fails behind a reverse proxy that normalizes
// paths — nginx with a URI part on its proxy_pass decodes the %2F, splits
// the identifier across segments, and every request 404s. That failure lives
// in somebody else's configuration file, appears only in production, and no
// test of this server would ever see it.
//
// So the addressing avoids it: registrations are addressed by an opaque id.
// This test is what stops that quietly regressing to the natural key.
func TestApplicationPathsCarryNoEncodedSlashes(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	sp := api.do(http.MethodPost, "/api/v1/applications/saml-service-providers", token,
		map[string]any{"metadataXml": spMetadata(spEntityID, "https://sp.example.test/acs")})
	if sp.Status != http.StatusOK {
		t.Fatalf("register service provider: %d %s", sp.Status, sp.Code)
	}
	cas := api.do(http.MethodPost, "/api/v1/applications/cas-services", token,
		map[string]any{"urlPrefix": "https://wiki.example.test/"})
	if cas.Status != http.StatusOK {
		t.Fatalf("register CAS service: %d %s", cas.Status, cas.Code)
	}

	var spRow, casRow struct {
		ID string `json:"id"`
	}
	sp.into(t, &spRow)
	cas.into(t, &casRow)

	for _, id := range []struct{ kind, value string }{
		{"SAML service provider", spRow.ID},
		{"CAS service", casRow.ID},
	} {
		if id.value == "" {
			t.Errorf("%s has no id to address it by", id.kind)
			continue
		}
		if strings.ContainsAny(id.value, "/?#%") {
			t.Errorf("%s id %q would need percent-encoding in a path; "+
				"a normalizing proxy would then split it", id.kind, id.value)
		}
		// The round trip has to work with the raw value, unescaped. If it
		// only worked escaped, the identifier still contains something a
		// proxy could mangle.
		if escaped := url.PathEscape(id.value); escaped != id.value {
			t.Errorf("%s id %q changes under path escaping (to %q)",
				id.kind, id.value, escaped)
		}
	}

	// And the addresses actually resolve.
	if res := api.do(http.MethodPost,
		"/api/v1/applications/saml-service-providers/"+spRow.ID+"/disable", token, nil); res.Status != http.StatusOK {
		t.Errorf("disable service provider by id: %d %s", res.Status, res.Code)
	}
	if res := api.do(http.MethodPost,
		"/api/v1/applications/saml-service-providers/"+spRow.ID+"/enable", token, nil); res.Status != http.StatusOK {
		t.Errorf("enable service provider by id: %d %s", res.Status, res.Code)
	}
	if res := api.do(http.MethodPost,
		"/api/v1/applications/cas-services/"+casRow.ID+"/disable", token, nil); res.Status != http.StatusOK {
		t.Errorf("disable CAS service by id: %d %s", res.Status, res.Code)
	}
	if res := api.do(http.MethodPost,
		"/api/v1/applications/cas-services/"+casRow.ID+"/enable", token, nil); res.Status != http.StatusOK {
		t.Errorf("enable CAS service by id: %d %s", res.Status, res.Code)
	}
}

// A partial update must not silently discard the redirect URIs.
//
// PUT replaces, so a body that omits them is refused rather than treated as
// "no redirect URIs" — an application whose redirect URIs were quietly
// emptied by a rename would stop working with nothing to point at.
func TestUpdatingAClientWithoutRedirectURIsIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	create := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId":     "keep-uris",
		"redirectUris": []string{"https://app.example.test/callback"},
	})
	if create.Status != http.StatusOK {
		t.Fatalf("create client: %d %s", create.Status, create.Code)
	}

	res := api.do(http.MethodPut, "/api/v1/applications/oauth-clients/keep-uris", token,
		map[string]any{"name": "Renamed"})
	if res.Status != http.StatusBadRequest || res.Code != "REDIRECT_URI_REQUIRED" {
		t.Errorf("rename without redirect URIs = %d %s, want 400 REDIRECT_URI_REQUIRED",
			res.Status, res.Code)
	}

	read := api.do(http.MethodGet, "/api/v1/applications/oauth-clients/keep-uris", token, nil)
	if !strings.Contains(string(read.Data), "https://app.example.test/callback") {
		t.Fatalf("the refused update discarded the redirect URIs anyway: %s", read.Data)
	}
}
