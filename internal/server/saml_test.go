package server_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/provision"
	"github.com/Paraview-RD/portico/internal/samlp"
	"github.com/Paraview-RD/portico/internal/service"
)

// The SAML tests drive Portico with the same library's service-provider
// side, over a real socket, for the same reason the federation tests use a
// real relying party: what matters is not that XML round-trips, but that the
// document a service provider receives verifies against the certificate the
// metadata published.

// testSP is a service provider that can send an authentication request and
// verify what comes back.
type testSP struct {
	t  *testing.T
	sp *saml.ServiceProvider
}

// newTestSP builds a service provider whose assertion consumer service is a
// loopback address nothing listens on. Nothing needs to: the assertion is
// delivered as a self-posting form, and the test reads it out of that form
// rather than following it.
func newTestSP(t *testing.T, entityID string) *testSP {
	t.Helper()

	key, certificate := selfSigned(t)

	metadataURL, _ := url.Parse("http://127.0.0.1:9998/saml/metadata")
	acsURL, _ := url.Parse("http://127.0.0.1:9998/saml/acs")

	sp := &saml.ServiceProvider{
		EntityID:    entityID,
		Key:         key,
		Certificate: certificate,
		MetadataURL: *metadataURL,
		AcsURL:      *acsURL,
	}
	return &testSP{t: t, sp: sp}
}

// configure fetches Portico's metadata for an issuer, exactly as an
// integrator configures a service provider.
func (s *testSP) configure(f *federationTest, mount string) {
	s.t.Helper()

	res, err := http.Get(f.publicURL + mount + samlp.MetadataPath)
	if err != nil {
		s.t.Fatalf("fetch IdP metadata: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		s.t.Fatalf("IdP metadata returned %d", res.StatusCode)
	}

	var descriptor saml.EntityDescriptor
	if err := xml.NewDecoder(res.Body).Decode(&descriptor); err != nil {
		s.t.Fatalf("decode IdP metadata: %v", err)
	}
	s.sp.IDPMetadata = &descriptor
}

// metadata is the document an operator registers with `portico sp register`.
func (s *testSP) metadata() string {
	s.t.Helper()
	document, err := xml.MarshalIndent(s.sp.Metadata(), "", "  ")
	if err != nil {
		s.t.Fatalf("marshal SP metadata: %v", err)
	}
	return string(document)
}

// selfSigned makes a throwaway keypair for a service provider.
func selfSigned(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test service provider"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return key, certificate
}

// registerSP adds a service provider to a tenant, as the CLI would.
func (f *federationTest) registerSP(tenantCode, metadata string) model.SAMLServiceProvider {
	f.t.Helper()

	p, err := provision.Open(f.cfg)
	if err != nil {
		f.t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	provider, err := p.RegisterServiceProvider(context.Background(), tenantCode,
		service.RegisterSPInput{MetadataXML: metadata, Name: "Test Service Provider"})
	if err != nil {
		f.t.Fatalf("register service provider: %v", err)
	}
	return provider
}

// samlResponseForm matches the base64 SAMLResponse the identity provider
// posts back inside a self-submitting form.
var samlResponseForm = regexp.MustCompile(`name="SAMLResponse" value="([^"]+)"`)

// samlSignIn walks a browser through a SAML authentication request the way a
// person does, and returns the base64 SAMLResponse the identity provider
// posted back.
func (f *federationTest) samlSignIn(authURL *url.URL, tenant, username, password string) string {
	f.t.Helper()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	res, err := client.Get(authURL.String())
	if err != nil {
		f.t.Fatalf("SSO: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		f.t.Fatalf("SSO returned %d, want a redirect to the sign-in screen", res.StatusCode)
	}

	loginURL, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		f.t.Fatalf("parse the sign-in redirect: %v", err)
	}
	requestID := loginURL.Query().Get("saml_request")
	if requestID == "" {
		f.t.Fatalf("the sign-in redirect (%s) carries no authentication request", loginURL)
	}

	token := f.post("/api/v1/auth/login", "", map[string]string{
		"tenant": tenant, "identifier": username, "password": password,
	})["token"].(string)

	redirectTo := f.post("/api/v1/saml/authenticate", token, map[string]string{
		"samlRequestId": requestID,
	})["redirectTo"].(string)

	// The callback answers with a self-posting form carrying the assertion.
	// A browser would submit it; the test reads it out instead, because the
	// service provider in this test has no server.
	body := f.get(redirectTo)
	match := samlResponseForm.FindStringSubmatch(body)
	if match == nil {
		f.t.Fatalf("the callback did not return a SAML response form:\n%s", body)
	}
	// The form is HTML, so the value is HTML-escaped — base64's "+" arrives
	// as "&#43;". A browser unescapes it when it submits the form; this has
	// to do the same, or it is testing a string no service provider ever
	// sees.
	return html.UnescapeString(match[1])
}

// get fetches a URL and returns the body.
func (f *federationTest) get(target string) string {
	f.t.Helper()

	res, err := http.Get(target)
	if err != nil {
		f.t.Fatalf("get %s: %v", target, err)
	}
	defer func() { _ = res.Body.Close() }()

	body := make([]byte, 0, 8192)
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	if res.StatusCode != http.StatusOK {
		f.t.Fatalf("get %s returned %d: %s", target, res.StatusCode, body)
	}
	return string(body)
}

// parse verifies an assertion the way a service provider does: signature
// against the published certificate, issuer, audience, and expiry.
func (s *testSP) parse(encoded string, requestID string) (*saml.Assertion, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		s.t.Fatalf("decode SAML response: %v", err)
	}
	return s.sp.ParseXMLResponse(decoded, []string{requestID}, s.sp.AcsURL)
}

// The whole flow, driven by a real service provider: metadata, an
// authentication request, a sign-in, and an assertion whose signature is
// checked against the certificate the metadata published.
func TestSAMLSignOnWithARealServiceProvider(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://sp.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, err := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatalf("build authentication request: %v", err)
	}
	authURL, err := request.Redirect("relay-abc", sp.sp)
	if err != nil {
		t.Fatalf("build redirect: %v", err)
	}

	encoded := f.samlSignIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)

	assertion, err := sp.parse(encoded, request.ID)
	if err != nil {
		t.Fatalf("the service provider rejected the assertion: %v", err)
	}

	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		t.Fatal("the assertion carries no name identifier")
	}
	if got := assertion.Issuer.Value; got != f.publicURL+samlp.TenantMount(model.DefaultTenantCode)+samlp.MetadataPath {
		t.Errorf("issuer = %q, want the tenant's metadata URL", got)
	}

	attributes := attributeValues(assertion)
	if attributes["uid"] != adminUsername {
		t.Errorf("uid = %q, want %q", attributes["uid"], adminUsername)
	}
	if attributes["tenantCode"] != model.DefaultTenantCode {
		t.Errorf("tenantCode = %q, want %q", attributes["tenantCode"], model.DefaultTenantCode)
	}
	if attributes["role"] != string(model.RoleSuperAdmin) {
		t.Errorf("role = %q, want %q", attributes["role"], model.RoleSuperAdmin)
	}
}

// An assertion states each fact once.
//
// crewjam's assertion maker adds attributes of its own from the session's
// UserName, UserEmail, and UserCommonName fields, and Portico supplies the
// same facts again in CustomAttributes. Setting both sends `uid` and `mail`
// twice, and a service provider that keeps the first wins over one that
// keeps the last for no reason either of them chose.
//
// Counted by Name rather than FriendlyName, and asserted as a property
// rather than about `uid` in particular. The attribute Name is the OID a
// service provider maps on; and attributeValues above collapses into a map,
// which is why every existing test passed while this shipped — a second
// value for a name it already had simply overwrote the first.
func TestASAMLAssertionStatesEachAttributeOnce(t *testing.T) {
	f := newFederationTest(t)

	// An account with an email address, because the duplicate on `mail` only
	// appears when there is one to state — which is why looking at an
	// assertion for the bootstrap administrator shows half the defect.
	adminToken := f.api.adminToken()
	if res := f.api.do(http.MethodPost, "/api/v1/users", adminToken, map[string]string{
		"username":    "saml.duplicate",
		"displayName": "Attribute Duplicate",
		"email":       "saml.duplicate@example.com",
		"password":    "duplicate-password-1",
		"role":        "USER",
	}); res.Status != http.StatusOK {
		t.Fatalf("create the account: %d %s %s", res.Status, res.Code, res.Message)
	}

	sp := newTestSP(t, "https://once.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, err := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatalf("build authentication request: %v", err)
	}
	authURL, err := request.Redirect("", sp.sp)
	if err != nil {
		t.Fatalf("build redirect: %v", err)
	}

	encoded := f.samlSignIn(authURL, model.DefaultTenantCode,
		"saml.duplicate", "duplicate-password-1")

	assertion, err := sp.parse(encoded, request.ID)
	if err != nil {
		t.Fatalf("the service provider rejected the assertion: %v", err)
	}

	seen := map[string]int{}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			seen[attribute.Name]++
		}
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("the assertion carries %s %d times, want once", name, count)
		}
	}
}

// The signature has to be the thing that is checked, not the shape of the
// XML. Two ways of saying so, because "the assertion was accepted" is also
// what an implementation that signed nothing would produce.
//
// Assertions come back encrypted whenever the service provider publishes an
// encryption key — which crewjam's does — so there is no cleartext name
// identifier to rewrite. What is in the clear is the signature over the
// response, and that is what these tamper with.
func TestATamperedResponseIsRejected(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://tamper.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, err := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatalf("build authentication request: %v", err)
	}
	authURL, _ := request.Redirect("", sp.sp)

	encoded := f.samlSignIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode SAML response: %v", err)
	}

	// It parses as it stands.
	if _, err := sp.sp.ParseXMLResponse(decoded, []string{request.ID}, sp.sp.AcsURL); err != nil {
		t.Fatalf("the untampered response was rejected: %v", err)
	}

	// The digest is what binds the signature to the content. Changing it can
	// only be caught by verifying the signature — nothing else in a service
	// provider looks at it — so this fails if and only if the signature is
	// really being checked.
	digest := regexp.MustCompile(`(<ds:DigestValue>)([^<]+)(</ds:DigestValue>)`)
	if !digest.MatchString(string(decoded)) {
		t.Fatalf("no digest to tamper with:\n%s", decoded)
	}
	tampered := digest.ReplaceAllString(string(decoded),
		"${1}AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=${3}")

	if _, err := sp.sp.ParseXMLResponse([]byte(tampered), []string{request.ID}, sp.sp.AcsURL); err == nil {
		t.Error("a response with a rewritten digest was accepted — the signature is not being checked")
	}
}

// The same response, verified against a different certificate. If the
// signature were checked against anything other than the certificate the
// metadata published, this would still pass.
func TestAResponseSignedByAnotherKeyIsRejected(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://rotate.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	encoded := f.samlSignIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode SAML response: %v", err)
	}

	// Rotate. The certificate the response was signed with is now retired,
	// and the metadata publishes a new one.
	p, err := provision.Open(f.cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.RotateSAMLCertificate(context.Background(), model.DefaultTenantCode); err != nil {
		t.Fatalf("rotate certificate: %v", err)
	}

	// The same service provider, reconfigured from the metadata as an
	// integrator would after being told to. Its own keypair is unchanged —
	// only its trust in Portico's certificate is.
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	if _, err := sp.sp.ParseXMLResponse(decoded, []string{request.ID}, sp.sp.AcsURL); err == nil {
		t.Error("a response signed by a retired certificate verified against the new one")
	}

	// And rotation took effect without a restart: a fresh sign-on verifies
	// against the new certificate. This is what the absent provider cache
	// buys — a cached identity provider would still be signing with the
	// certificate the operator just retired.
	fresh, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	freshURL, _ := fresh.Redirect("", sp.sp)

	freshEncoded := f.samlSignIn(freshURL, model.DefaultTenantCode, adminUsername, adminPassword)
	freshDecoded, _ := base64.StdEncoding.DecodeString(freshEncoded)
	if _, err := sp.sp.ParseXMLResponse(freshDecoded, []string{fresh.ID}, sp.sp.AcsURL); err != nil {
		t.Errorf("after rotating, a new assertion did not verify against the new certificate: %v", err)
	}
}

// A person takes longer than ninety seconds to find a password, and the
// protocol library refuses an authentication request older than that. The
// deadline that applies is Portico's own, judged from when the request was
// accepted rather than from when the assertion is minted.
func TestSigningInSlowlyStillWorks(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://slow.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, err := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatalf("build authentication request: %v", err)
	}
	authURL, _ := request.Redirect("", sp.sp)

	// Two minutes pass between the request arriving and the sign-in
	// finishing — a person reaching for a password manager. The clock is
	// moved rather than the test waiting.
	//
	// saml.TimeNow is a package global, read by the server's own handlers in
	// this same process. Nothing in this file may run in parallel: a request
	// from another test overlapping this one would see a clock two minutes
	// ahead and fail for a reason that has nothing to do with what it is
	// testing.
	restore := saml.TimeNow
	t.Cleanup(func() { saml.TimeNow = restore })

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(authURL.String())
	if err != nil {
		t.Fatalf("SSO: %v", err)
	}
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	requestID := loginURL.Query().Get("saml_request")
	if requestID == "" {
		t.Fatalf("no authentication request in %s", loginURL)
	}

	saml.TimeNow = func() time.Time { return restore().Add(2 * time.Minute) }

	token := f.post("/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername, "password": adminPassword,
	})["token"].(string)
	redirectTo := f.post("/api/v1/saml/authenticate", token, map[string]string{
		"samlRequestId": requestID,
	})["redirectTo"].(string)

	body := f.get(redirectTo)
	if samlResponseForm.FindStringSubmatch(body) == nil {
		t.Fatalf("a sign-in that took two minutes was refused:\n%s", body)
	}
}

// An authentication request produces one assertion. A callback that could be
// replayed is one somebody who saw the URL could mint a second assertion
// from.
func TestASAMLRequestIsSpentOnce(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://once.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, _ := client.Get(authURL.String())
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	requestID := loginURL.Query().Get("saml_request")

	token := f.post("/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername, "password": adminPassword,
	})["token"].(string)
	redirectTo := f.post("/api/v1/saml/authenticate", token, map[string]string{
		"samlRequestId": requestID,
	})["redirectTo"].(string)

	if body := f.get(redirectTo); samlResponseForm.FindStringSubmatch(body) == nil {
		t.Fatalf("the first attempt produced no assertion:\n%s", body)
	}

	replay, err := http.Get(redirectTo)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer func() { _ = replay.Body.Close() }()
	if replay.StatusCode == http.StatusOK {
		t.Error("the callback minted a second assertion for the same request")
	}
}

// A service provider registered in one tenant is a stranger in another.
func TestASAMLProviderIsUnknownOutsideItsTenant(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("acme", "Acme")

	sp := newTestSP(t, "https://acme-only.example.com/saml")
	f.registerSP("acme", sp.metadata())

	// Configured against acme, so its requests are well formed...
	sp.configure(f, samlp.TenantMount("acme"))
	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	// ...but sent to the default tenant's SSO endpoint, which has never
	// heard of it.
	target := strings.Replace(authURL.String(),
		f.publicURL+samlp.TenantMount("acme"), f.publicURL, 1)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(target)
	if err != nil {
		t.Fatalf("SSO: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if strings.Contains(res.Header.Get("Location"), "saml_request=") {
		t.Error("another tenant's service provider was accepted")
	}
}

// Disabling a service provider stops it receiving assertions without
// deleting the registration.
func TestADisabledServiceProviderReceivesNothing(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://disabled.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	p, err := provision.Open(f.cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.SetServiceProviderStatus(context.Background(),
		model.DefaultTenantCode, sp.sp.EntityID, model.StatusDisabled); err != nil {
		t.Fatalf("disable service provider: %v", err)
	}

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(authURL.String())
	if err != nil {
		t.Fatalf("SSO: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if strings.Contains(res.Header.Get("Location"), "saml_request=") {
		t.Error("a disabled service provider was sent to sign-in")
	}
}

// Reaching the callback without signing in must produce nothing. It is the
// one URL in the flow whose id somebody could guess at or find in a log.
func TestTheSAMLCallbackRefusesAnUncompletedRequest(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://direct.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, _ := client.Get(authURL.String())
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	requestID := loginURL.Query().Get("saml_request")

	callback := f.publicURL + samlp.TenantMount(model.DefaultTenantCode) +
		samlp.CallbackPath + "?id=" + url.QueryEscape(requestID)

	direct, err := http.Get(callback)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = direct.Body.Close() }()

	if direct.StatusCode == http.StatusOK {
		t.Error("the callback issued an assertion for a request nobody had signed in for")
	}
}

// Another account must not take over a request somebody else completed.
func TestASAMLRequestCannotBeTakenOver(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://takeover.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	adminToken := f.api.adminToken()
	f.api.createUser(adminToken, "saml.bystander", "bystander-password-1", "USER")

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, _ := client.Get(authURL.String())
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	requestID := loginURL.Query().Get("saml_request")

	f.post("/api/v1/saml/authenticate", adminToken,
		map[string]string{"samlRequestId": requestID})

	bystander := f.api.login("saml.bystander", "bystander-password-1")
	code := f.postExpectingFailure("/api/v1/saml/authenticate", bystander,
		map[string]string{"samlRequestId": requestID})
	if code != "AUTH_REQUEST_TAKEN" {
		t.Errorf("code = %q, want AUTH_REQUEST_TAKEN", code)
	}
}

// The metadata is what an integrator configures from, so every claim in it
// has to be true of the running server.
func TestSAMLMetadataDescribesTheRunningServer(t *testing.T) {
	f := newFederationTest(t)

	for _, mount := range []string{"", samlp.TenantMount(model.DefaultTenantCode)} {
		res, err := http.Get(f.publicURL + mount + samlp.MetadataPath)
		if err != nil {
			t.Fatalf("fetch metadata: %v", err)
		}
		var descriptor saml.EntityDescriptor
		err = xml.NewDecoder(res.Body).Decode(&descriptor)
		_ = res.Body.Close()
		if err != nil {
			t.Fatalf("decode metadata: %v", err)
		}

		if descriptor.EntityID != f.publicURL+mount+samlp.MetadataPath {
			t.Errorf("entityID = %q, want %q",
				descriptor.EntityID, f.publicURL+mount+samlp.MetadataPath)
		}
		if len(descriptor.IDPSSODescriptors) != 1 {
			t.Fatalf("expected exactly one IDPSSODescriptor, got %d",
				len(descriptor.IDPSSODescriptors))
		}

		idp := descriptor.IDPSSODescriptors[0]
		if len(idp.KeyDescriptors) == 0 {
			t.Error("the metadata publishes no certificate, so nothing could verify an assertion")
		}

		// Every endpoint it names must answer.
		for _, sso := range idp.SingleSignOnServices {
			probe, err := http.Get(sso.Location)
			if err != nil {
				t.Errorf("SSO endpoint %s: %v", sso.Location, err)
				continue
			}
			_ = probe.Body.Close()
			if probe.StatusCode == http.StatusNotFound {
				t.Errorf("the metadata names %s, which is not mounted", sso.Location)
			}
		}

		// Single logout is deliberately absent, and saying otherwise would
		// have a service provider send logout requests into a 404.
		if len(idp.SingleLogoutServices) != 0 {
			t.Error("the metadata advertises single logout, which is not implemented")
		}
	}
}

// attributeValues flattens an assertion's attributes by friendly name.
func attributeValues(assertion *saml.Assertion) map[string]string {
	values := map[string]string{}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if len(attribute.Values) == 0 {
				continue
			}
			name := attribute.FriendlyName
			if name == "" {
				name = attribute.Name
			}
			values[name] = attribute.Values[0].Value
		}
	}
	return values
}

// The POST-binding page is the one response in this application allowed to
// post a form to another origin and run an inline script — which is exactly
// what its Content-Security-Policy forbids everywhere else.
//
// This is here because every other SAML test passed while it was broken. A
// test client does not enforce CSP; a browser refuses to submit the form and
// refuses to run the script, and the person is left looking at a page that
// does nothing.
func TestThePostBindingPageIsAllowedToDoItsJob(t *testing.T) {
	f := newFederationTest(t)

	sp := newTestSP(t, "https://csp.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, _ := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	authURL, _ := request.Redirect("", sp.sp)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, _ := client.Get(authURL.String())
	_ = res.Body.Close()
	loginURL, _ := url.Parse(res.Header.Get("Location"))
	requestID := loginURL.Query().Get("saml_request")

	token := f.post("/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername, "password": adminPassword,
	})["token"].(string)
	redirectTo := f.post("/api/v1/saml/authenticate", token, map[string]string{
		"samlRequestId": requestID,
	})["redirectTo"].(string)

	page, err := http.Get(redirectTo)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = page.Body.Close() }()

	policy := page.Header.Get("Content-Security-Policy")

	// form-action has to name the assertion consumer service. The
	// application's own policy says 'self', under which the browser refuses
	// to submit the form at all.
	if !strings.Contains(policy, "form-action "+sp.sp.AcsURL.String()) {
		t.Errorf("form-action does not name the assertion consumer service.\npolicy: %s", policy)
	}
	if strings.Contains(policy, "form-action 'self'") {
		t.Error("the page inherited the application's form-action, which blocks the whole binding")
	}

	// And the script that submits it has to be permitted, by hash rather
	// than by opening the page to any inline script.
	if !strings.Contains(policy, "script-src 'sha256-") {
		t.Errorf("the submit script is not permitted by hash.\npolicy: %s", policy)
	}
	if strings.Contains(policy, "'unsafe-inline'") {
		t.Error("the page permits any inline script, which is more than the binding needs")
	}

	// The hash has to be the hash of the script that is actually on the
	// page — a policy naming a different one is a policy that blocks it.
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	script := regexp.MustCompile(`<script>([^<]*)</script>`).FindSubmatch(body)
	if script == nil {
		t.Fatalf("the page carries no script to submit the form:\n%s", body)
	}
	sum := sha256.Sum256(script[1])
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(policy, want) {
		t.Errorf("the policy names a different script than the page carries.\nwant %s\npolicy: %s",
			want, policy)
	}
}
