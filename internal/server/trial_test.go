package server_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// Self-service trials, which are the only writes in this API a stranger can
// reach and the only ones that create a tenant.
//
// The property worth the most here is the first test: on a deployment that did
// not ask for this, the routes do not exist. Everything else is about what
// happens once somebody has declared their deployment a demonstration.

func newTrialTest(t *testing.T, enabled bool, quota ...int) (*apiTest, *recordingMailer) {
	t.Helper()
	silenceLogs(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = testdb.DSN(t)
	cfg.InitialAdminUsername = adminUsername
	cfg.InitialAdminPassword = adminPassword
	cfg.PublicURL = "https://demo.portico.example.com"
	cfg.AuthRateLimit, cfg.AuthRateLimitBurst = 100000, 100000
	cfg.TrialSignup = enabled
	cfg.TrialMaxTenants = 50
	if len(quota) > 0 {
		cfg.TrialMaxTenants = quota[0]
	}

	mailer := &recordingMailer{}
	srv, err := server.New(cfg, server.WithMailer(mailer))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	return &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}, mailer
}

// tokenFromLink pulls the confirmation token out of the link that was mailed.
// Reading it out of the message rather than the database is the point: what a
// visitor can do is bounded by what arrived in their inbox.
func tokenFromLink(t *testing.T, body string) string {
	t.Helper()
	found := regexp.MustCompile(`https?://\S+`).FindString(body)
	if found == "" {
		t.Fatalf("no link in the message: %q", body)
	}
	parsed, err := url.Parse(strings.TrimRight(found, ".\n"))
	if err != nil {
		t.Fatalf("parse link %q: %v", found, err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("link carries no token: %s", found)
	}
	return token
}

func TestTrialRoutesDoNotExistUnlessTheDeploymentAsksForThem(t *testing.T) {
	api, _ := newTrialTest(t, false)

	// Status answers, and says no. It is asked on every sign-in page load, so
	// a missing route would leave a 404 in the console of every deployment
	// that does not offer trials — noise that outlives its usefulness and
	// buys only the concealment of a feature whose source is public.
	status := api.do(http.MethodGet, "/api/v1/trial/status", "", nil)
	if status.Status != http.StatusOK {
		t.Fatalf("status answered %d with trials off, want 200 saying no",
			status.Status)
	}
	var offered struct {
		Enabled bool `json:"enabled"`
	}
	status.into(t, &offered)
	if offered.Enabled {
		t.Error("status says trials are on for a deployment that did not ask")
	}

	// The two that create something are the ones that must not exist: 404
	// from the router, not from a handler that decided to refuse.
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/trial", map[string]string{"email": "a@b.test"}},
		{http.MethodPost, "/api/v1/trial/confirm", map[string]string{"token": "x"}},
	} {
		got := api.do(call.method, call.path, "", call.body)
		if got.Status != http.StatusNotFound {
			t.Errorf("%s %s answered %d with trials disabled, want 404 — a "+
				"deployment that did not ask for self-service tenant creation "+
				"should not have the endpoint at all",
				call.method, call.path, got.Status)
		}
	}
}

func TestATrialTakesAnAddressAndGivesBackATenant(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	got := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "visitor@example.test",
		"companyName": "Example Industries",
		"tenantCode":  "example-industries",
	})
	if got.Status != http.StatusOK {
		t.Fatalf("request: %d %s %s", got.Status, got.Code, got.Message)
	}

	msg := mailer.waitFor(t, before+1)
	if msg.To != "visitor@example.test" {
		t.Errorf("link went to %q", msg.To)
	}

	confirmed := api.do(http.MethodPost, "/api/v1/trial/confirm", "", map[string]string{
		"token": tokenFromLink(t, msg.Body),
	})
	if confirmed.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirmed.Status, confirmed.Code, confirmed.Message)
	}

	var out struct {
		TenantCode    string `json:"tenantCode"`
		TenantName    string `json:"tenantName"`
		AdminUsername string `json:"adminUsername"`
		AdminPassword string `json:"adminPassword"`
		SignInURL     string `json:"signInUrl"`
	}
	confirmed.into(t, &out)

	if out.TenantCode != "example-industries" {
		t.Errorf("tenant code is %q", out.TenantCode)
	}
	if out.TenantName != "Example Industries" {
		t.Errorf("tenant name is %q", out.TenantName)
	}
	if out.AdminPassword == "" {
		t.Fatal("no password came back; the visitor has a tenant they cannot open")
	}
	if !strings.Contains(out.SignInURL, "tenant=example-industries") {
		t.Errorf("sign-in link does not name the tenant: %q", out.SignInURL)
	}

	// The claim this whole feature rests on: the credentials work. Asserted
	// through sign-in rather than by reading the row, because a tenant with an
	// administrator who cannot sign in is the failure this would otherwise
	// report as success.
	//
	// And not forced to change the password on the way in: the visitor did not
	// choose it and has nowhere to look it up but the email.
	signIn := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"tenant":     out.TenantCode,
		"identifier": out.AdminUsername,
		"password":   out.AdminPassword,
	})
	if signIn.Status != http.StatusOK {
		t.Fatalf("the credentials the trial issued do not work: %d %s %s",
			signIn.Status, signIn.Code, signIn.Message)
	}
}

func TestATrialLinkIsSpentOnce(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "once@example.test",
		"companyName": "Once Ltd",
		"tenantCode":  "once-ltd",
	})
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)

	if first := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); first.Status != http.StatusOK {
		t.Fatalf("first confirm: %d %s", first.Status, first.Code)
	}

	// A second click must not create a second tenant, and must say why rather
	// than reporting a broken link — the visitor's credentials are valid and
	// they should be told to use them.
	second := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token})
	if second.Status != http.StatusConflict || second.Code != "TRIAL_LINK_SPENT" {
		t.Errorf("second confirm answered %d %s, want 409 TRIAL_LINK_SPENT",
			second.Status, second.Code)
	}
}

func TestATrialCannotTakeACodeThatIsAlreadyInUse(t *testing.T) {
	api, _ := newTrialTest(t, true)

	// `default` exists on every deployment, so it is the collision every
	// visitor could stumble into. Refused before a link is sent: it is the one
	// failure they can fix, and being told after checking their email is the
	// worst moment to hear it.
	got := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "collide@example.test",
		"companyName": "Collide",
		"tenantCode":  "default",
	})
	if got.Status != http.StatusConflict || got.Code != "TRIAL_CODE_TAKEN" {
		t.Errorf("answered %d %s, want 409 TRIAL_CODE_TAKEN", got.Status, got.Code)
	}
}

func TestOneAddressGetsOneTrialTenant(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "twice@example.test",
		"companyName": "Twice",
		"tenantCode":  "twice-first",
	})
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	api.do(http.MethodPost, "/api/v1/trial/confirm", "", map[string]string{"token": token})

	// Same address, different code. The quota is per person, not per name.
	again := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "twice@example.test",
		"companyName": "Twice Again",
		"tenantCode":  "twice-second",
	})
	if again.Status != http.StatusConflict || again.Code != "TRIAL_EMAIL_USED" {
		t.Errorf("second request answered %d %s, want 409 TRIAL_EMAIL_USED",
			again.Status, again.Code)
	}
}

func TestTheQuotaRefusesRatherThanQueueing(t *testing.T) {
	// A cap of one, so the boundary is one request away rather than fifty.
	api, mailer := newTrialTest(t, true, 1)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "first@example.test",
		"companyName": "First",
		"tenantCode":  "quota-first",
	})
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); got.Status != http.StatusOK {
		t.Fatalf("first trial: %d %s", got.Status, got.Code)
	}

	// Full. Said out loud rather than accepted and dropped: a visitor told to
	// check their email waits for a link that will never come and concludes
	// the product is broken, which is a worse outcome than being turned away.
	full := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "second@example.test",
		"companyName": "Second",
		"tenantCode":  "quota-second",
	})
	if full.Status != http.StatusConflict || full.Code != "TRIAL_QUOTA_REACHED" {
		t.Errorf("answered %d %s, want 409 TRIAL_QUOTA_REACHED", full.Status, full.Code)
	}

	// And the refusal sent nothing, which is the half a status code does not
	// cover. Two is the expected count: the link for the first request, and
	// the credentials that confirming it produced.
	if sent := len(mailer.sent()); sent != before+2 {
		t.Errorf("%d messages sent, want %d — a refused request mailed something",
			sent, before+2)
	}
}

func TestTheStatusEndpointOnlyOffersWorldsThatExist(t *testing.T) {
	api, _ := newTrialTest(t, true)

	status := api.do(http.MethodGet, "/api/v1/trial/status", "", nil)
	if status.Status != http.StatusOK {
		t.Fatalf("status: %d %s", status.Status, status.Code)
	}

	var offered struct {
		Enabled    bool     `json:"enabled"`
		Industries []string `json:"industries"`
	}
	status.into(t, &offered)

	if !offered.Enabled {
		t.Error("status says trials are off on a server that has them on")
	}
	// Every name here has to have seeded data behind it. Offering one that
	// does not hands somebody an empty tenant and a wrong expectation, which
	// is why the list comes from the service rather than from the form.
	if len(offered.Industries) == 0 {
		t.Fatal("no industries offered, so the form has nothing to list")
	}
	for _, name := range offered.Industries {
		if name == "" {
			t.Error("an unnamed industry is offered")
		}
	}
}
