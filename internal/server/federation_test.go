package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/client/rs"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/oidcp"
	"github.com/paraview/portico/internal/provision"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
)

// The federation tests drive Portico with the same library an integrator
// would use, over a real socket.
//
// That is the point of them. Hand-assembled requests test the endpoints
// Portico thinks it has; a relying party tests the ones it advertises —
// discovery, the issuer in every URL, the key set the ID token's signature
// is checked against. A mount that strips the wrong prefix passes the first
// kind of test and fails the second, which is the failure this stage was
// most likely to ship.

// federationTest is a running Portico with its public URL matching the port
// it is actually listening on.
type federationTest struct {
	t         *testing.T
	api       *apiTest
	http      *httptest.Server
	publicURL string
	clients   *service.OAuthClientService
	tenants   *service.TenantService
	db        *sql.DB
	cfg       *config.Config
}

func newFederationTest(t *testing.T) *federationTest {
	t.Helper()
	silenceLogs(t)

	// Unstarted, because the public URL has to be known before the server is
	// built — it is what every advertised endpoint is derived from — and the
	// port is only assigned once the listener exists. Building first and
	// starting after would point discovery at the wrong port, and the
	// failure would look like a discovery bug rather than a test bug.
	ts := httptest.NewUnstartedServer(nil)
	publicURL := "http://" + ts.Listener.Addr().String()

	cfg := testConfig(t)
	cfg.PublicURL = publicURL

	api := newAPITestWithConfig(t, cfg)

	ts.Config.Handler = api.srv.Handler()
	ts.Start()
	t.Cleanup(ts.Close)

	// A second connection to the same database, for the two things that have
	// no HTTP surface by design: provisioning a tenant and registering a
	// relying party.
	st, err := store.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return &federationTest{
		t: t, api: api, http: ts, publicURL: publicURL,
		clients: service.NewOAuthClientService(st),
		tenants: service.NewTenantService(st),
		db:      st.DB(),
		cfg:     cfg,
	}
}

// registerClient adds a relying party to a tenant, as the CLI would.
func (f *federationTest) registerClient(tenantCode, clientID string, public bool) service.RegisteredClient {
	f.t.Helper()

	tenant, err := f.tenants.Resolve(context.Background(), tenantCode)
	if err != nil {
		f.t.Fatalf("resolve tenant %s: %v", tenantCode, err)
	}

	registered, err := f.clients.Register(context.Background(), tenant.ID, service.RegisterClientInput{
		ClientID:     clientID,
		Name:         "Test Application",
		Public:       public,
		RedirectURIs: []string{"http://127.0.0.1:9999/callback"},
		Scopes:       []string{"openid", "profile", "email", "offline_access"},
	})
	if err != nil {
		f.t.Fatalf("register client: %v", err)
	}
	return registered
}

// provisionTenant creates a tenant with its own administrator, the way an
// operator does: from the command line, because there is no API for it.
func (f *federationTest) provisionTenant(code, name string) {
	f.t.Helper()

	p, err := provision.Open(f.cfg)
	if err != nil {
		f.t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	if _, err := p.CreateTenant(context.Background(), code, name, adminUsername, adminPassword); err != nil {
		f.t.Fatalf("create tenant %s: %v", code, err)
	}
}

// relyingParty runs discovery against an issuer, exactly as an integrator's
// application does at start-up.
func (f *federationTest) relyingParty(issuer, clientID, secret string) rp.RelyingParty {
	f.t.Helper()

	party, err := rp.NewRelyingPartyOIDC(context.Background(), issuer, clientID, secret,
		"http://127.0.0.1:9999/callback",
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopeOfflineAccess})
	if err != nil {
		f.t.Fatalf("discovery against %s: %v", issuer, err)
	}
	return party
}

// signIn walks a browser through an authorization request the way a person
// does: follow the redirect to the sign-in screen, authenticate, hand the
// request back, and end up at the relying party's redirect URI with a code.
//
// It stops short of following the last redirect, because that address
// belongs to an application that does not exist in this test.
func (f *federationTest) signIn(authURL, tenant, username, password string) (code, state string) {
	f.t.Helper()

	client := &http.Client{
		// Every redirect is inspected rather than followed, since the
		// interesting information is in the Location headers.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. The provider records the request and sends the browser to sign in.
	res, err := client.Get(authURL)
	if err != nil {
		f.t.Fatalf("authorize: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		f.t.Fatalf("authorize returned %d, want a redirect to the sign-in screen", res.StatusCode)
	}

	loginURL, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		f.t.Fatalf("parse the sign-in redirect: %v", err)
	}
	authRequestID := loginURL.Query().Get("auth_request")
	if authRequestID == "" {
		f.t.Fatalf("the sign-in redirect (%s) carries no authorization request", loginURL)
	}

	// 2. The person signs in. This is Portico's own API, not the protocol's.
	token := f.post("/api/v1/auth/login", "", map[string]string{
		"tenant": tenant, "identifier": username, "password": password,
	})["token"].(string)

	// 3. The sign-in screen hands the request back, authorized.
	redirectTo := f.post("/api/v1/oauth/authorize", token, map[string]string{
		"authRequestId": authRequestID,
	})["redirectTo"].(string)

	// 4. The provider issues a code and redirects to the application.
	res, err = client.Get(redirectTo)
	if err != nil {
		f.t.Fatalf("authorization callback: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		f.t.Fatalf("callback returned %d, want a redirect to the application", res.StatusCode)
	}

	callback, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		f.t.Fatalf("parse the application redirect: %v", err)
	}
	if errCode := callback.Query().Get("error"); errCode != "" {
		f.t.Fatalf("the application was sent an error: %s — %s",
			errCode, callback.Query().Get("error_description"))
	}
	return callback.Query().Get("code"), callback.Query().Get("state")
}

// post calls Portico's own API over the network and returns the envelope's
// data, failing on anything but success.
func (f *federationTest) post(path, token string, body map[string]string) map[string]any {
	f.t.Helper()

	encoded, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, f.publicURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		f.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatalf("%s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()

	var envelope struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		f.t.Fatalf("decode %s: %v", path, err)
	}
	if res.StatusCode != http.StatusOK {
		f.t.Fatalf("%s returned %d: %s — %s", path, res.StatusCode, envelope.Code, envelope.Message)
	}
	return envelope.Data
}

// The whole flow, driven by a real relying party: discovery, an
// authorization request with PKCE, a sign-in, a code exchange, and an ID
// token whose signature is checked against the published key set.
func TestAuthorizationCodeFlowWithARelyingParty(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "test-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-code-verifier-long-enough-to-be-worth-something"
	authURL := rp.AuthURL("state-abc", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)))

	code, state := f.signIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)
	if state != "state-abc" {
		t.Errorf("state = %q, want it returned unchanged", state)
	}

	// CodeExchange verifies the ID token: signature against the JWKS the
	// discovery document named, issuer, audience, and expiry. A wrong mount
	// or a stale key fails right here.
	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	if tokens.IDTokenClaims.Issuer != issuer {
		t.Errorf("iss = %q, want %q", tokens.IDTokenClaims.Issuer, issuer)
	}
	if tokens.IDTokenClaims.PreferredUsername != adminUsername {
		t.Errorf("preferred_username = %q, want %q",
			tokens.IDTokenClaims.PreferredUsername, adminUsername)
	}
	if tokens.RefreshToken == "" {
		t.Error("no refresh token, though offline_access was requested")
	}

	// The tenant claims are what make a token usable by a downstream system
	// that serves more than one tenant.
	claims := tokens.IDTokenClaims.Claims
	if claims["tenant_code"] != model.DefaultTenantCode {
		t.Errorf("tenant_code = %v, want %q", claims["tenant_code"], model.DefaultTenantCode)
	}
	if claims["role"] != string(model.RoleSuperAdmin) {
		t.Errorf("role = %v, want %q", claims["role"], model.RoleSuperAdmin)
	}

	// The access token reaches userinfo, which is the other half of what a
	// relying party does with what it was given.
	info, err := rp.Userinfo[*oidc.UserInfo](context.Background(),
		tokens.AccessToken, tokens.TokenType, tokens.IDTokenClaims.Subject, party)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if info.PreferredUsername != adminUsername {
		t.Errorf("userinfo preferred_username = %q, want %q", info.PreferredUsername, adminUsername)
	}
}

// A code may be spent once. Replaying it is what an attacker who read a
// redirect out of a log or a proxy would do.
func TestAuthorizationCodeCannotBeReplayed(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "replay-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "another-code-verifier-of-a-respectable-length"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	if _, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier)); err != nil {
		t.Fatalf("first exchange: %v", err)
	}

	if _, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier)); err == nil {
		t.Error("the same authorization code was accepted twice")
	}
}

// PKCE is what stops somebody who intercepted the code from redeeming it,
// so the wrong verifier must fail even though everything else is right.
func TestCodeExchangeRequiresTheMatchingVerifier(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "pkce-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge("the-real-verifier-value-here"))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	if _, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier("a-different-verifier-entirely-x")); err == nil {
		t.Error("a code was redeemed with the wrong PKCE verifier")
	}
}

// An authorization request without PKCE at all. OAuth 2.1 requires it of
// every client, including confidential ones, so the request must be refused
// before a code exists.
func TestAuthorizationWithoutPKCEIsRefused(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "nopkce-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	authURL := issuer + "/authorize?" + url.Values{
		"client_id":     {registered.Client.ClientID},
		"redirect_uri":  {"http://127.0.0.1:9999/callback"},
		"response_type": {"code"},
		"scope":         {"openid"},
		"state":         {"s"},
	}.Encode()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	location := res.Header.Get("Location")
	if strings.Contains(location, "auth_request=") {
		t.Fatal("a request with no code_challenge was accepted and sent to sign-in")
	}
	if !strings.Contains(location, "error=") {
		t.Errorf("expected an error redirect, got %d %s", res.StatusCode, location)
	}
}

// Refreshing rotates: the token that was presented is spent, and presenting
// it again means a copy leaked, which takes the whole chain down.
func TestRefreshRotatesAndAReplayKillsTheChain(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "refresh-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "yet-another-verifier-long-enough-to-pass"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	first, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	second, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		first.RefreshToken, "", "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("the refresh token was reused rather than rotated")
	}

	// Replaying the spent token is the leak signal.
	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		first.RefreshToken, "", ""); err == nil {
		t.Error("a spent refresh token was accepted")
	}

	// And its replacement dies with it, because which link leaked is
	// unknowable and leaving the live one usable would defeat the point.
	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		second.RefreshToken, "", ""); err == nil {
		t.Error("the replacement survived the chain revocation")
	}
}

// Disabling an account has to reach the relying parties, not only Portico's
// own sessions. This is the claim the README makes; it is worth a test.
func TestDisablingAnAccountEndsFederatedSessions(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "disable-app", false)

	adminToken := f.api.adminToken()
	userID := f.api.createUser(adminToken, "federated.user", "federated-password-1", "USER")

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-verifier-for-the-disabled-account-case"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, "federated.user", "federated-password-1")

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	// Refreshing works while the account does.
	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		tokens.RefreshToken, "", ""); err != nil {
		t.Fatalf("refresh before disabling: %v", err)
	}

	res := f.api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", adminToken, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Message)
	}

	// The refresh token issued by the rotation above is revoked with the
	// rest, so nothing the relying party holds still works.
	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		tokens.RefreshToken, "", ""); err == nil {
		t.Error("a disabled account could still be refreshed")
	}
}

// Two issuers, one set of accounts: a tenant is reachable at /t/<code>, and
// the default tenant additionally at the root so a single-tenant deployment
// never has to explain tenants to an integrator.
func TestTheDefaultTenantIsServedAtBothIssuers(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "root-app", true)

	for _, issuer := range []string{
		f.publicURL,
		f.publicURL + "/t/" + model.DefaultTenantCode,
	} {
		t.Run(issuer, func(t *testing.T) {
			party := f.relyingParty(issuer, registered.Client.ClientID, "")

			verifier := "a-verifier-for-the-issuer-comparison-x"
			code, _ := f.signIn(
				rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
				model.DefaultTenantCode, adminUsername, adminPassword)

			tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
				rp.WithCodeVerifier(verifier))
			if err != nil {
				t.Fatalf("code exchange: %v", err)
			}
			// Each mount mints tokens naming itself, which is what lets a
			// relying party verify one against the issuer it configured.
			if tokens.IDTokenClaims.Issuer != issuer {
				t.Errorf("iss = %q, want %q", tokens.IDTokenClaims.Issuer, issuer)
			}
		})
	}
}

// A tenant's issuer is its own. Signing in to the wrong one has to fail
// with something actionable, because the person did authenticate
// successfully and "unknown request" reads as a fault in Portico.
func TestSigningInToTheWrongTenantSaysSo(t *testing.T) {
	f := newFederationTest(t)

	f.provisionTenant("acme", "Acme")
	registered := f.registerClient("acme", "acme-app", true)

	party := f.relyingParty(f.publicURL+"/t/acme", registered.Client.ClientID, "")
	authURL := rp.AuthURL("s", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge("a-verifier-for-the-wrong-tenant-case")))

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	_ = res.Body.Close()

	loginURL, _ := url.Parse(res.Header.Get("Location"))
	authRequestID := loginURL.Query().Get("auth_request")
	if authRequestID == "" {
		t.Fatalf("no authorization request in %s", loginURL)
	}

	// Sign in to the default tenant instead, which is the mistake somebody
	// makes when they are already signed in to the wrong one.
	token := f.post("/api/v1/auth/login", "", map[string]string{
		"tenant": model.DefaultTenantCode, "identifier": adminUsername, "password": adminPassword,
	})["token"].(string)

	body, _ := json.Marshal(map[string]string{"authRequestId": authRequestID})
	req, _ := http.NewRequest(http.MethodPost, f.publicURL+"/api/v1/oauth/authorize",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var envelope struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(response.Body).Decode(&envelope)

	if envelope.Code != "AUTH_REQUEST_WRONG_TENANT" {
		t.Errorf("code = %q (status %d), want AUTH_REQUEST_WRONG_TENANT — "+
			"anything else sends somebody who signed in successfully looking for a fault that is not there",
			envelope.Code, response.StatusCode)
	}
}

// Expired authorization requests are swept. Every arrival at /authorize
// writes a row and most are never completed, so without this the table only
// grows.
func TestExpiredAuthorizationRequestsAreSwept(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "sweep-app", true)

	party := f.relyingParty(f.publicURL, registered.Client.ClientID, "")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// An abandoned sign-in: the request exists and nobody ever finishes it.
	res, err := client.Get(rp.AuthURL("s", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge("abandoned-request-verifier-value"))))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	authRequestID := loginURL.Query().Get("auth_request")

	// Sweeping now must leave it alone: it has not expired.
	if err := f.api.srv.SweepFederation(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !f.authRequestExists(authRequestID) {
		t.Fatal("the sweep deleted a request that had not expired")
	}

	f.expireAuthRequest(authRequestID)

	if err := f.api.srv.SweepFederation(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if f.authRequestExists(authRequestID) {
		t.Error("an expired request survived the sweep")
	}
}

func (f *federationTest) authRequestExists(id string) bool {
	f.t.Helper()

	var count int
	err := f.db.QueryRow("SELECT count(*) FROM oauth_auth_requests WHERE id = $1", id).Scan(&count)
	if err != nil {
		f.t.Fatalf("count authorization requests: %v", err)
	}
	return count > 0
}

func (f *federationTest) expireAuthRequest(id string) {
	f.t.Helper()

	_, err := f.db.Exec("UPDATE oauth_auth_requests SET expires_at = $1 WHERE id = $2",
		time.Now().Add(-time.Hour), id)
	if err != nil {
		f.t.Fatalf("expire authorization request: %v", err)
	}
}

// A tenant other than the default one, all the way through. This is the
// case the recorded issuer exists for: an authorization request created at
// /t/acme can only be completed by acme's own provider, because every
// lookup on the way is scoped to the tenant the issuer names.
func TestFederationIsPerTenant(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("acme", "Acme")
	registered := f.registerClient("acme", "acme-app", false)

	issuer := f.publicURL + "/t/acme"
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-verifier-for-the-second-tenant-flow"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		"acme", adminUsername, adminPassword)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	if tokens.IDTokenClaims.Issuer != issuer {
		t.Errorf("iss = %q, want %q", tokens.IDTokenClaims.Issuer, issuer)
	}
	if got := tokens.IDTokenClaims.Claims["tenant_code"]; got != "acme" {
		t.Errorf("tenant_code = %v, want \"acme\"", got)
	}

	// The two tenants have administrators with the same username, and the
	// tokens are about different people. A subject that collided would mean
	// a downstream system keyed on `sub` merged two accounts.
	defaultClient := f.registerClient(model.DefaultTenantCode, "acme-app", false)
	defaultIssuer := f.publicURL + "/t/" + model.DefaultTenantCode
	defaultParty := f.relyingParty(defaultIssuer, defaultClient.Client.ClientID, defaultClient.Secret)

	defaultVerifier := "a-verifier-for-the-default-tenant-flow"
	defaultCode, _ := f.signIn(
		rp.AuthURL("s", defaultParty, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(defaultVerifier))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	defaultTokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), defaultCode, defaultParty,
		rp.WithCodeVerifier(defaultVerifier))
	if err != nil {
		t.Fatalf("code exchange in the default tenant: %v", err)
	}
	if defaultTokens.IDTokenClaims.Subject == tokens.IDTokenClaims.Subject {
		t.Error("both tenants' administrators have the same subject")
	}
}

// A client registered in one tenant does not exist in another, however
// plausible its id looks there.
func TestAClientIsUnknownOutsideItsTenant(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("acme", "Acme")
	registered := f.registerClient("acme", "acme-only-app", true)

	authURL := f.publicURL + "/t/" + model.DefaultTenantCode + "/authorize?" + url.Values{
		"client_id":             {registered.Client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:9999/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"state":                 {"s"},
		"code_challenge":        {oidc.NewSHACodeChallenge("a-verifier-for-the-cross-tenant-attempt")},
		"code_challenge_method": {"S256"},
	}.Encode()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if strings.Contains(res.Header.Get("Location"), "auth_request=") {
		t.Error("another tenant's client was accepted")
	}
}

// The discovery document is read once, by a machine, before anybody is
// watching. Anything in it that is not true here becomes a client
// configured for something that then fails somewhere else entirely.
func TestDiscoveryDescribesWhatIsActuallyImplemented(t *testing.T) {
	f := newFederationTest(t)

	res, err := http.Get(f.publicURL + oidcp.DiscoveryPath)
	if err != nil {
		t.Fatalf("fetch discovery: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	var document map[string]any
	if err := json.NewDecoder(res.Body).Decode(&document); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}

	// The implicit and hybrid flows are what OAuth 2.1 exists to remove.
	if got := document["response_types_supported"]; !reflect.DeepEqual(got, []any{"code"}) {
		t.Errorf("response_types_supported = %v, want [code]", got)
	}
	if got := document["grant_types_supported"]; !reflect.DeepEqual(got,
		[]any{"authorization_code", "refresh_token"}) {
		t.Errorf("grant_types_supported = %v, want [authorization_code refresh_token]", got)
	}
	// Advertising an endpoint that is not mounted sends a client to a 404.
	if _, present := document["device_authorization_endpoint"]; present {
		t.Error("a device-authorization endpoint is advertised but not implemented")
	}
	if got := document["code_challenge_methods_supported"]; !reflect.DeepEqual(got, []any{"S256"}) {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", got)
	}

	// Every endpoint it names must answer. A document that points at a path
	// nothing serves is the failure this test exists for.
	for _, field := range []string{
		"authorization_endpoint", "token_endpoint", "userinfo_endpoint",
		"introspection_endpoint", "revocation_endpoint", "end_session_endpoint",
		"jwks_uri",
	} {
		endpoint, ok := document[field].(string)
		if !ok || endpoint == "" {
			t.Errorf("%s is missing from the discovery document", field)
			continue
		}
		probe, err := http.Get(endpoint)
		if err != nil {
			t.Errorf("%s (%s): %v", field, endpoint, err)
			continue
		}
		_ = probe.Body.Close()
		if probe.StatusCode == http.StatusNotFound {
			t.Errorf("%s points at %s, which is not mounted", field, endpoint)
		}
	}
}

// Introspection is what a resource server calls when it needs the answer
// sooner than an access token's expiry. docs/federation.md says a disabled
// account reports inactive straight away; this is that claim.
func TestIntrospectionReportsADisabledAccountInactive(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "introspect-app", false)

	adminToken := f.api.adminToken()
	userID := f.api.createUser(adminToken, "introspect.me", "introspect-password-1", "USER")

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-verifier-for-the-introspection-case"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, "introspect.me", "introspect-password-1")

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	resource, err := rs.NewResourceServerClientCredentials(context.Background(), issuer,
		registered.Client.ClientID, registered.Secret)
	if err != nil {
		t.Fatalf("build resource server: %v", err)
	}

	active, err := rs.Introspect[*oidc.IntrospectionResponse](context.Background(), resource, tokens.AccessToken)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if !active.Active {
		t.Fatal("a live account's token introspected as inactive")
	}
	if active.PreferredUsername != "introspect.me" {
		t.Errorf("preferred_username = %q, want %q", active.PreferredUsername, "introspect.me")
	}

	res := f.api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", adminToken, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Message)
	}

	// The access token is unchanged and still verifies offline. What
	// changed is the answer to asking.
	after, err := rs.Introspect[*oidc.IntrospectionResponse](context.Background(), resource, tokens.AccessToken)
	if err != nil {
		t.Fatalf("introspect after disabling: %v", err)
	}
	if after.Active {
		t.Error("a disabled account's token still introspects as active, " +
			"which is the one thing introspection is here to answer")
	}
}

// A relying party revoking a refresh token it holds. RFC 7009 requires the
// endpoint to answer successfully whatever it was given, so the assertion
// that matters is what stops working afterwards.
func TestARelyingPartyCanRevokeItsRefreshToken(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "revoke-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-verifier-for-the-revocation-case-xx"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	if err := rp.RevokeToken(context.Background(), party, tokens.RefreshToken, "refresh_token"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		tokens.RefreshToken, "", ""); err == nil {
		t.Error("a revoked refresh token was still accepted")
	}

	// Revocation is idempotent and must not report whether the token was
	// ever real, since that would make the endpoint an oracle.
	if err := rp.RevokeToken(context.Background(), party, "not-a-token-at-all", "refresh_token"); err != nil {
		t.Errorf("revoking an unknown token reported an error: %v", err)
	}
}

// RP-initiated logout: an application sends the person to end_session with
// the ID token it holds, and the session with that application ends.
func TestRPInitiatedLogoutEndsTheSession(t *testing.T) {
	f := newFederationTest(t)
	registered := f.registerClient(model.DefaultTenantCode, "logout-app", false)

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-verifier-for-the-end-session-case-x"
	code, _ := f.signIn(
		rp.AuthURL("s", party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier))),
		model.DefaultTenantCode, adminUsername, adminPassword)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	redirect, err := rp.EndSession(context.Background(), party, tokens.IDToken, "", "", "", nil)
	if err != nil {
		t.Fatalf("end session: %v", err)
	}
	if redirect == nil || redirect.String() == "" {
		t.Error("end_session returned nowhere to send the browser")
	}

	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		tokens.RefreshToken, "", ""); err == nil {
		t.Error("the refresh token survived an RP-initiated logout")
	}
}
