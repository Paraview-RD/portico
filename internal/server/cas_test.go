package server_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/casp"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/provision"
	"github.com/Paraview-RD/portico/internal/service"
)

// CAS has no cryptography, so these tests are about the two things it does
// have: which service URLs a ticket may be delivered to, and spending a
// ticket exactly once.

// casResponse is the shape a CAS client parses.
type casResponse struct {
	Success *struct {
		User       string `xml:"user"`
		Attributes *struct {
			DisplayName string `xml:"displayName"`
			Email       string `xml:"email"`
			TenantCode  string `xml:"tenant_code"`
			Role        string `xml:"role"`
		} `xml:"attributes"`
	} `xml:"authenticationSuccess"`
	Failure *struct {
		Code    string `xml:"code,attr"`
		Message string `xml:",chardata"`
	} `xml:"authenticationFailure"`
}

// registerCASService adds a CAS service to a tenant, as the CLI would.
func (f *federationTest) registerCASService(tenantCode, prefix, name string) model.CASService {
	f.t.Helper()

	p, err := provision.Open(f.cfg)
	if err != nil {
		f.t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	registered, err := p.RegisterCASService(context.Background(), tenantCode,
		service.RegisterCASInput{URLPrefix: prefix, Name: name})
	if err != nil {
		f.t.Fatalf("register CAS service: %v", err)
	}
	return registered
}

// casSignIn walks a browser through a CAS sign-in and returns the ticket the
// service was sent.
func (f *federationTest) casSignIn(mount, serviceURL, tenant, username, password string) string {
	f.t.Helper()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	res, err := client.Get(f.publicURL + mount + casp.LoginPath +
		"?service=" + url.QueryEscape(serviceURL))
	if err != nil {
		f.t.Fatalf("CAS login: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		f.t.Fatalf("CAS login returned %d, want a redirect to the sign-in screen", res.StatusCode)
	}

	loginURL, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		f.t.Fatalf("parse the sign-in redirect: %v", err)
	}
	if loginURL.Query().Get("cas_service") != serviceURL {
		f.t.Fatalf("the sign-in redirect carries %q, want %q",
			loginURL.Query().Get("cas_service"), serviceURL)
	}

	token := f.post("/api/v1/auth/login", "", map[string]string{
		"tenant": tenant, "identifier": username, "password": password,
	})["token"].(string)

	redirectTo := f.post("/api/v1/cas/authorize", token, map[string]string{
		"service": serviceURL,
	})["redirectTo"].(string)

	target, err := url.Parse(redirectTo)
	if err != nil {
		f.t.Fatalf("parse the service redirect: %v", err)
	}
	ticket := target.Query().Get("ticket")
	if ticket == "" {
		f.t.Fatalf("the service redirect (%s) carries no ticket", redirectTo)
	}
	if !strings.HasPrefix(ticket, "ST-") {
		f.t.Errorf("ticket = %q, want the ST- prefix the specification requires", ticket)
	}
	return ticket
}

// validate calls a validation endpoint and decodes the answer.
func (f *federationTest) validate(mount, path, serviceURL, ticket string) casResponse {
	f.t.Helper()

	target := f.publicURL + mount + path +
		"?service=" + url.QueryEscape(serviceURL) +
		"&ticket=" + url.QueryEscape(ticket)

	res, err := http.Get(target)
	if err != nil {
		f.t.Fatalf("validate: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	// Always 200, including for a failure: several CAS clients stop reading
	// on anything else and report a transport error instead of the reason.
	if res.StatusCode != http.StatusOK {
		f.t.Fatalf("validate returned %d, want 200 even for a failure", res.StatusCode)
	}

	var decoded casResponse
	if err := xml.NewDecoder(res.Body).Decode(&decoded); err != nil {
		f.t.Fatalf("decode CAS response: %v", err)
	}
	return decoded
}

// The whole flow: login, a ticket, and CAS 2.0 validation.
func TestCASSignOn(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://wiki.example.com/", "Wiki")

	const serviceURL = "https://wiki.example.com/login?return=/page"
	mount := casp.TenantMount(model.DefaultTenantCode)

	ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, adminUsername, adminPassword)

	answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket)
	if answer.Failure != nil {
		t.Fatalf("validation failed: %s — %s", answer.Failure.Code, answer.Failure.Message)
	}
	if answer.Success == nil || answer.Success.User != adminUsername {
		t.Fatalf("user = %+v, want %q", answer.Success, adminUsername)
	}
	// CAS 2.0 carries no attributes, and inventing some would be a document
	// a 2.0 client is not expecting.
	if answer.Success.Attributes != nil {
		t.Error("the CAS 2.0 response carries attributes, which belong to 3.0")
	}
}

// CAS 3.0 adds attributes, under the same names the other two protocols use.
func TestCASThreeCarriesAttributes(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://p3.example.com/", "P3")

	const serviceURL = "https://p3.example.com/cas"
	mount := casp.TenantMount(model.DefaultTenantCode)

	ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, adminUsername, adminPassword)

	answer := f.validate(mount, casp.ValidatePath3, serviceURL, ticket)
	if answer.Success == nil {
		t.Fatalf("validation failed: %+v", answer.Failure)
	}
	if answer.Success.Attributes == nil {
		t.Fatal("the CAS 3.0 response carries no attributes")
	}
	if got := answer.Success.Attributes.TenantCode; got != model.DefaultTenantCode {
		t.Errorf("tenant_code = %q, want %q", got, model.DefaultTenantCode)
	}
	if got := answer.Success.Attributes.Role; got != string(model.RoleSuperAdmin) {
		t.Errorf("role = %q, want %q", got, model.RoleSuperAdmin)
	}
}

// A service ticket is spent once. This is the property the whole protocol
// rests on: a ticket travels in a URL, through logs and referrers, and is
// worth nothing after the first validation.
func TestACASTicketIsSpentOnce(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://once.example.com/", "Once")

	const serviceURL = "https://once.example.com/cas"
	mount := casp.TenantMount(model.DefaultTenantCode)

	ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, adminUsername, adminPassword)

	if answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket); answer.Success == nil {
		t.Fatalf("the first validation failed: %+v", answer.Failure)
	}

	answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket)
	if answer.Success != nil {
		t.Error("the same ticket validated twice")
	}
	if answer.Failure == nil || answer.Failure.Code != "INVALID_TICKET" {
		t.Errorf("failure = %+v, want INVALID_TICKET", answer.Failure)
	}
}

// A ticket is bound to the service it was issued for. Without this, a
// service that legitimately receives a ticket could spend it at another
// service's validation endpoint and be signed in as that person there.
func TestACASTicketIsBoundToItsService(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://first.example.com/", "First")
	f.registerCASService(model.DefaultTenantCode, "https://second.example.com/", "Second")

	mount := casp.TenantMount(model.DefaultTenantCode)
	ticket := f.casSignIn(mount, "https://first.example.com/cas",
		model.DefaultTenantCode, adminUsername, adminPassword)

	answer := f.validate(mount, casp.ValidatePath, "https://second.example.com/cas", ticket)
	if answer.Success != nil {
		t.Error("a ticket issued for one service validated at another")
	}
	if answer.Failure == nil || answer.Failure.Code != "INVALID_SERVICE" {
		t.Errorf("failure = %+v, want INVALID_SERVICE", answer.Failure)
	}
}

// The prefix boundary. A registration for https://app.example.com/ must not
// cover https://app.example.com.somewhere-else.test, which is what a naive
// prefix match would do and what makes CAS service matching worth testing.
func TestCASPrefixMatchingStopsAtABoundary(t *testing.T) {
	cases := []struct {
		prefix, service string
		want            bool
	}{
		{"https://app.example.com/", "https://app.example.com/", true},
		{"https://app.example.com/", "https://app.example.com/login", true},
		{"https://app.example.com/", "https://app.example.com/login?x=1", true},

		// The attack: a hostname that begins with the registered one.
		{"https://app.example.com/", "https://app.example.com.evil.test/", false},
		{"https://app.example.com/", "https://app.example.commercial.test/", false},

		// A registered subpath does not cover a sibling.
		{"https://app.example.com/wiki/", "https://app.example.com/wiki/page", true},
		{"https://app.example.com/wiki/", "https://app.example.com/admin", false},

		{"https://app.example.com/", "http://app.example.com/", false},
		{"https://app.example.com/", "", false},

		// A prefix that does not end at a boundary. Registration normalizes
		// one on, so this cannot come from the CLI — but the function is
		// exported and a stored row predates nothing, so the check has to be
		// in the matcher rather than only in the writer. Without it, this
		// pair matches, which is the hostname attack above with the
		// normalization removed.
		{"https://app.example.com", "https://app.example.com.evil.test/", false},
		{"https://app.example.com", "https://app.example.com", true},
	}

	for _, c := range cases {
		if got := service.MatchCASService(c.prefix, c.service); got != c.want {
			t.Errorf("MatchCASService(%q, %q) = %v, want %v",
				c.prefix, c.service, got, c.want)
		}
	}
}

// An unregistered service is refused before anybody types a password, not
// after.
func TestCASRefusesAnUnregisteredServiceBeforeSignIn(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://known.example.com/", "Known")

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(f.publicURL + casp.TenantMount(model.DefaultTenantCode) +
		casp.LoginPath + "?service=" + url.QueryEscape("https://stranger.example.com/cas"))
	if err != nil {
		t.Fatalf("CAS login: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusFound {
		t.Errorf("an unregistered service was sent to sign-in: %s",
			res.Header.Get("Location"))
	}
}

// And the ticket endpoint checks it too, so a link that goes straight to the
// sign-in screen with a service in it cannot produce a ticket for somewhere
// unregistered.
func TestCASRefusesAnUnregisteredServiceAtTicketIssue(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://known.example.com/", "Known")

	token := f.api.adminToken()
	code := f.postExpectingFailure("/api/v1/cas/authorize", token,
		map[string]string{"service": "https://stranger.example.com/cas"})
	if code != "CAS_SERVICE_NOT_REGISTERED" {
		t.Errorf("code = %q, want CAS_SERVICE_NOT_REGISTERED", code)
	}
}

// Registration refuses the shapes that make matching unsafe.
func TestCASRegistrationRefusesUnsafePrefixes(t *testing.T) {
	f := newFederationTest(t)

	p, err := provision.Open(f.cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()

	for _, prefix := range []string{
		"https://app.example.com/*",     // a wildcard
		"http://app.example.com/",       // plain http over a network
		"/relative/path",                // not absolute
		"https://app.example.com/?a=b",  // a query string
		"https://app.example.com/#frag", // a fragment
		"ftp://app.example.com/",        // not http
	} {
		_, err := p.RegisterCASService(context.Background(), model.DefaultTenantCode,
			service.RegisterCASInput{URLPrefix: prefix})
		if err == nil {
			t.Errorf("registered %q, which should have been refused", prefix)
		}
	}

	// Loopback http is allowed, because a local development client has
	// nowhere else to be.
	if _, err := p.RegisterCASService(context.Background(), model.DefaultTenantCode,
		service.RegisterCASInput{URLPrefix: "http://127.0.0.1:9999/"}); err != nil {
		t.Errorf("loopback http was refused: %v", err)
	}
}

// Disabling a service stops it receiving tickets without deleting it.
func TestADisabledCASServiceReceivesNoTickets(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://off.example.com/", "Off")

	p, err := provision.Open(f.cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.SetCASServiceStatus(context.Background(), model.DefaultTenantCode,
		"https://off.example.com/", model.StatusDisabled); err != nil {
		t.Fatalf("disable: %v", err)
	}

	token := f.api.adminToken()
	code := f.postExpectingFailure("/api/v1/cas/authorize", token,
		map[string]string{"service": "https://off.example.com/cas"})
	if code != "CAS_SERVICE_NOT_REGISTERED" {
		t.Errorf("code = %q, want CAS_SERVICE_NOT_REGISTERED — a disabled service "+
			"and an unregistered one must be indistinguishable", code)
	}
}

// A CAS service registered in one tenant is a stranger in another.
func TestACASServiceIsUnknownOutsideItsTenant(t *testing.T) {
	f := newFederationTest(t)
	f.provisionTenant("acme", "Acme")
	f.registerCASService("acme", "https://acme-only.example.com/", "Acme Only")

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(f.publicURL + casp.TenantMount(model.DefaultTenantCode) +
		casp.LoginPath + "?service=" + url.QueryEscape("https://acme-only.example.com/cas"))
	if err != nil {
		t.Fatalf("CAS login: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusFound {
		t.Error("another tenant's CAS service was accepted")
	}
}

// Disabling an account has to reach a ticket that has been issued but not
// yet validated. The window is a minute; letting it through would hand a
// service a session for somebody just switched off.
func TestADisabledAccountsCASTicketIsRefused(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://timing.example.com/", "Timing")

	adminToken := f.api.adminToken()
	userID := f.api.createUser(adminToken, "cas.user", "cas-password-1234", "USER")

	const serviceURL = "https://timing.example.com/cas"
	mount := casp.TenantMount(model.DefaultTenantCode)
	ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, "cas.user", "cas-password-1234")

	res := f.api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", adminToken, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Status, res.Message)
	}

	answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket)
	if answer.Success != nil {
		t.Error("a disabled account's ticket still validated")
	}
}

// The CAS endpoints answer at the root as well, so a single-tenant
// deployment never has to mention tenants.
func TestCASIsServedAtBothMounts(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://both.example.com/", "Both")

	for _, mount := range []string{"", casp.TenantMount(model.DefaultTenantCode)} {
		serviceURL := "https://both.example.com/cas?mount=" + url.QueryEscape(mount)
		ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, adminUsername, adminPassword)

		if answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket); answer.Success == nil {
			t.Errorf("mount %q: validation failed: %+v", mount, answer.Failure)
		}
	}
}

// Logout sends the browser to the sign-in screen, which is what actually
// ends the session. It must not follow a caller-supplied service parameter:
// an endpoint that redirects anywhere it is told is an open redirect
// wearing a protocol's clothes.
func TestCASLogoutDoesNotFollowAServiceParameter(t *testing.T) {
	f := newFederationTest(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(f.publicURL + casp.LogoutPath +
		"?service=" + url.QueryEscape("https://somewhere-else.test/"))
	if err != nil {
		t.Fatalf("CAS logout: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	location := res.Header.Get("Location")
	if strings.Contains(location, "somewhere-else.test") {
		t.Errorf("logout redirected to a caller-supplied address: %s", location)
	}
	if !strings.HasPrefix(location, f.publicURL+"/login") {
		t.Errorf("logout redirected to %q, want Portico's own sign-in screen", location)
	}
}

// Expired tickets are swept, like everything else that only grows.
func TestExpiredCASTicketsAreSwept(t *testing.T) {
	f := newFederationTest(t)
	f.registerCASService(model.DefaultTenantCode, "https://sweep.example.com/", "Sweep")

	const serviceURL = "https://sweep.example.com/cas"
	mount := casp.TenantMount(model.DefaultTenantCode)
	ticket := f.casSignIn(mount, serviceURL, model.DefaultTenantCode, adminUsername, adminPassword)

	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if f.casTicketCount() == 0 {
		t.Fatal("the sweep deleted a ticket that had not expired")
	}

	if _, err := f.db.Exec(
		"UPDATE cas_tickets SET expires_at = now() - interval '1 hour'"); err != nil {
		t.Fatalf("expire ticket: %v", err)
	}
	if err := f.api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if f.casTicketCount() != 0 {
		t.Error("an expired ticket survived the sweep")
	}

	// And it is gone as a credential too, not merely as a row.
	if answer := f.validate(mount, casp.ValidatePath, serviceURL, ticket); answer.Success != nil {
		t.Error("a swept ticket still validated")
	}
}

func (f *federationTest) casTicketCount() int {
	f.t.Helper()

	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM cas_tickets").Scan(&count); err != nil {
		f.t.Fatalf("count tickets: %v", err)
	}
	return count
}
