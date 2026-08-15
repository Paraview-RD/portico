package server_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestEveryOfferedIndustryProducesATenantWorthLookingAt is what stage two of
// this feature exists for.
//
// A trial used to hand back a tenant with one account in it and nothing else,
// which answered none of the questions a visitor arrives with. Every industry
// the status endpoint offers is confirmed here and the result is read back
// through the API, as the console would — not out of the pack, which would only
// prove that a slice literal has the length it has.
//
// One server for all of them, deliberately: each pack costs a bcrypt hash per
// account, and a fresh server per subtest would multiply the slowest part of
// this file by five for nothing.
func TestEveryOfferedIndustryProducesATenantWorthLookingAt(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	status := api.do(http.MethodGet, "/api/v1/trial/status", "", nil)
	var offered struct {
		Industries []string `json:"industries"`
	}
	status.into(t, &offered)
	if len(offered.Industries) < 5 {
		t.Fatalf("%d industries offered, want the generic pack and four industries",
			len(offered.Industries))
	}

	for i, industry := range offered.Industries {
		t.Run(industry, func(t *testing.T) {
			code := fmt.Sprintf("filled-%s", industry)
			before := len(mailer.sent())
			asked := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
				"email":       fmt.Sprintf("filled%d@example.test", i),
				"companyName": "Filled " + industry,
				"tenantCode":  code,
				"industry":    industry,
			})
			if asked.Status != http.StatusOK {
				t.Fatalf("request: %d %s %s", asked.Status, asked.Code, asked.Message)
			}

			token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
			confirmed := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
				map[string]string{"token": token})
			if confirmed.Status != http.StatusOK {
				t.Fatalf("confirm: %d %s %s", confirmed.Status, confirmed.Code, confirmed.Message)
			}

			var out struct {
				TenantCode    string `json:"tenantCode"`
				AdminUsername string `json:"adminUsername"`
				AdminPassword string `json:"adminPassword"`
				DemoPassword  string `json:"demoPassword"`
				Industry      string `json:"industry"`
			}
			confirmed.into(t, &out)

			if out.Industry != industry {
				t.Fatalf("asked for %q and the response reports %q — the fill did not "+
					"happen, and the visitor has an empty tenant", industry, out.Industry)
			}
			if out.DemoPassword == "" {
				t.Fatal("no password for the seeded accounts, so the only way in is as " +
					"the administrator and the portal cannot be looked at")
			}
			if out.DemoPassword == out.AdminPassword {
				t.Error("the seeded accounts share the administrator's password; anybody " +
					"who guessed the tenant code would be an administrator")
			}

			token = api.loginTo(out.TenantCode, out.AdminUsername, out.AdminPassword)

			// Read back through the API, which is the only view that proves a
			// console would show something.
			for _, want := range []struct {
				path  string
				least int
				why   string
			}{
				{"/api/v1/organizations", 5, "a tree needs enough nodes to look like one"},
				{"/api/v1/users", 13, "twelve people and the administrator"},
				{"/api/v1/user-attributes", 3, "the facts this tenant decided to record"},
				{"/api/v1/groups", 2, "groups are the other half of the organization story"},
			} {
				got := api.do(http.MethodGet, want.path, token, nil)
				if got.Status != http.StatusOK {
					t.Errorf("GET %s: %d %s", want.path, got.Status, got.Code)
					continue
				}
				if n := countRows(t, got.Data); n < want.least {
					t.Errorf("GET %s returned %d rows, want at least %d — %s",
						want.path, n, want.least, want.why)
				}
			}

			// Applications are three endpoints because they are three protocols,
			// and what matters is the total plus that more than one protocol is
			// represented.
			var apps, protocols int
			for _, path := range []string{
				"/api/v1/applications/oauth-clients",
				"/api/v1/applications/saml-service-providers",
				"/api/v1/applications/cas-services",
			} {
				got := api.do(http.MethodGet, path, token, nil)
				if got.Status != http.StatusOK {
					t.Errorf("GET %s: %d %s", path, got.Status, got.Code)
					continue
				}
				n := countRows(t, got.Data)
				apps += n
				if n > 0 {
					protocols++
				}
			}
			if apps < 3 {
				t.Errorf("%d applications registered, want at least 3", apps)
			}
			if protocols < 2 {
				t.Errorf("applications use %d protocol(s); telling OAuth, SAML and CAS "+
					"apart on that screen needs more than one present", protocols)
			}

			// And the demonstration password actually opens one of the seeded
			// accounts. Everything above would pass for a tenant full of accounts
			// nobody can sign in to, which is the failure this catches.
			if seeded := api.do(http.MethodGet, "/api/v1/users", token, nil); seeded.Status == http.StatusOK {
				name := firstUsernameOtherThan(t, seeded.Data, out.AdminUsername)
				if name == "" {
					t.Fatal("the tenant holds nobody but its administrator")
				}
				if api.loginTo(out.TenantCode, name, out.DemoPassword) == "" {
					t.Errorf("%s cannot sign in with the password the trial handed out", name)
				}
			}
		})
	}
}

// Counting rows is seed_test.go's countRows, which already handles all three
// shapes this file asks about.

// firstUsernameOtherThan picks somebody the pack created, so that the
// demonstration password can be tried against a real account rather than a
// name this test invented.
func firstUsernameOtherThan(t *testing.T, data json.RawMessage, exclude string) string {
	t.Helper()

	var envelope struct {
		Items []struct {
			Username string `json:"username"`
			Status   string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("read the account list: %v", err)
	}
	for _, u := range envelope.Items {
		// Disabled accounts are in every pack on purpose and cannot sign in,
		// which would make this assertion fail for the right reason at the
		// wrong place.
		if u.Username != exclude && u.Status != "DISABLED" {
			return u.Username
		}
	}
	return ""
}

// TestAnAbandonedRequestStopsHoldingItsTenantCode is about the one row in
// this feature that outlives its own usefulness.
//
// A tenant code is reserved the moment somebody asks, before any link has
// been clicked, and the index enforcing that covers every row rather than
// only the confirmed ones — deliberately, because two people asking for the
// same code an instant apart must not both be told yes. The cost of that
// choice is that an abandoned request keeps a name nobody can have.
//
// Which is fine only if something takes it back. On a public demonstration
// the first names typed are `demo`, `test`, and the visitor's own company,
// and most of those people never open the email. Without a sweep those codes
// are refused forever, against tenants that do not exist — a refusal nobody
// can explain by looking at the tenant list, because there is nothing in it.
func TestAnAbandonedRequestStopsHoldingItsTenantCode(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	first := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "abandons@example.test",
		"companyName": "Abandoned Ltd",
		"tenantCode":  "abandoned-code",
	})
	if first.Status != http.StatusOK {
		t.Fatalf("first request: %d %s %s", first.Status, first.Code, first.Message)
	}
	mailer.waitFor(t, before+1)

	// Never confirmed, and the link has run out. Aged rather than waited for:
	// the alternative is a two-hour test.
	api.execSQL(t, `UPDATE trial_requests SET expires_at = now() - interval '1 hour'
		WHERE tenant_code = $1`, "abandoned-code")

	// Still taken, which is correct until the sweep runs and is the whole
	// reason the sweep has to.
	taken := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "wants-it@example.test",
		"companyName": "Wants It",
		"tenantCode":  "abandoned-code",
	})
	if taken.Code != "TRIAL_CODE_TAKEN" {
		t.Fatalf("before the sweep the code answered %s, want TRIAL_CODE_TAKEN — "+
			"this test is not exercising what it thinks", taken.Code)
	}

	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// And now somebody else may have it. Asserted through the API rather than
	// by counting rows: what matters is not that a row went but that the name
	// is available again.
	after := len(mailer.sent())
	freed := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "wants-it@example.test",
		"companyName": "Wants It",
		"tenantCode":  "abandoned-code",
	})
	if freed.Status != http.StatusOK {
		t.Fatalf("after the sweep the code is still refused: %d %s %s. An expired "+
			"request that nobody confirmed is holding a name against a tenant that "+
			"does not exist.", freed.Status, freed.Code, freed.Message)
	}
	mailer.waitFor(t, after+1)
}

// TestTheSweepLeavesLiveAndConfirmedRequestsAlone is the other half.
//
// A sweep that took too much would be worse than one that took nothing: a
// live link deleted is a visitor who followed the instructions and was told
// their link is invalid, and a confirmed row deleted takes the record that
// their address already has a tenant with it.
func TestTheSweepLeavesLiveAndConfirmedRequestsAlone(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	// One that has been confirmed.
	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "keeper@example.test",
		"companyName": "Keeper",
		"tenantCode":  "keeper-code",
	})
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); got.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s", got.Status, got.Code)
	}

	// And one that is still live, whose link has not been followed yet.
	pendingBefore := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "pending@example.test",
		"companyName": "Pending",
		"tenantCode":  "pending-code",
	})
	liveToken := tokenFromLink(t, mailer.waitFor(t, pendingBefore+1).Body)

	// Confirmed rows are expired by now too — the row keeps the expiry it was
	// issued with — so a sweep that only looked at the clock would take it.
	api.execSQL(t, `UPDATE trial_requests SET expires_at = now() - interval '1 hour'
		WHERE tenant_code = $1`, "keeper-code")

	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The live link still works.
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": liveToken}); got.Status != http.StatusOK {
		t.Errorf("a link that had not expired stopped working after a sweep: %d %s",
			got.Status, got.Code)
	}

	// And the confirmed address is still known to have had its tenant.
	again := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "keeper@example.test",
		"companyName": "Keeper Again",
		"tenantCode":  "keeper-second",
	})
	if again.Code != "TRIAL_EMAIL_USED" {
		t.Errorf("after the sweep the address answered %s, want TRIAL_EMAIL_USED — "+
			"the row recording that it already has a tenant was deleted", again.Code)
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
