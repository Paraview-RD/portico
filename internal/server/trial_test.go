package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/provision"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/service"
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

// TestTheAdministratorKeepsTheAddressThatProvedItself is about an account
// that had no way back in.
//
// The address on the form is the one thing this whole flow establishes: a
// tenant exists because somebody opened a link sent to it. And until now that
// address was used for the link, used for the credentials, and then thrown
// away — the administrator it created had no email and no phone, so the
// person who had just proved an address could not use it to recover the
// account, and the portal told them so on every visit.
func TestTheAdministratorKeepsTheAddressThatProvedItself(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	const address = "founder@example.test"
	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       address,
		"companyName": "Founder Ltd",
		"tenantCode":  "founder-ltd",
	})
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
	}
	confirmed.into(t, &out)

	token = api.loginTo(out.TenantCode, out.AdminUsername, out.AdminPassword)
	me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	if me.Status != http.StatusOK {
		t.Fatalf("read the administrator: %d %s", me.Status, me.Code)
	}
	var admin struct {
		Email string `json:"email"`
	}
	me.into(t, &admin)

	if admin.Email != address {
		t.Errorf("the administrator's address is %q, want %q. It is the address "+
			"this tenant exists because of, and without it on the account the one "+
			"person who can administer the tenant cannot recover it.",
			admin.Email, address)
	}

	// The point of the address, asserted where it is spent rather than by
	// reading the column: recovery has to actually reach it.
	//
	// Recovery is asked by destination rather than by username — the address
	// is what finds the account — which makes this the exact question worth
	// asking here: can somebody who knows only the address they signed up
	// with get back into the tenant it created.
	recoveryBefore := len(mailer.sent())
	asked := api.do(http.MethodPost, "/api/v1/auth/password-recovery", "", map[string]string{
		"tenant":      out.TenantCode,
		"channel":     "EMAIL",
		"destination": address,
	})
	if asked.Status != http.StatusOK {
		t.Fatalf("ask for recovery: %d %s %s", asked.Status, asked.Code, asked.Message)
	}
	sent := mailer.waitFor(t, recoveryBefore+1)
	if sent.To != address {
		t.Errorf("the recovery link went to %q, want %q", sent.To, address)
	}
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

// Looking after trial tenants from the command line.
//
// These live here rather than in internal/provision because what they need is
// a trial tenant that a trial actually created — filled with a pack, with a
// confirmed request pointing at it — and this file is where one can be had.
// Constructing the same state by hand would be testing the constructor.

// openProvisioner builds the command-line provisioner against the same
// database the API test is using.
func openProvisioner(t *testing.T, api *apiTest) *provision.Provisioner {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = api.dsn

	p, err := provision.Open(cfg)
	if err != nil {
		t.Fatalf("open provisioner: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// confirmATrial runs the whole flow and returns the tenant code it produced.
func confirmATrial(t *testing.T, api *apiTest, mailer *recordingMailer, email, code, industry string) {
	t.Helper()

	before := len(mailer.sent())
	asked := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email": email, "companyName": code, "tenantCode": code, "industry": industry,
	})
	if asked.Status != http.StatusOK {
		t.Fatalf("request a trial: %d %s %s", asked.Status, asked.Code, asked.Message)
	}
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); got.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", got.Status, got.Code, got.Message)
	}
}

func TestTheCommandLineCanSeeWhichTenantsAStrangerCreated(t *testing.T) {
	api, mailer := newTrialTest(t, true)
	confirmATrial(t, api, mailer, "listed@example.test", "listed-co", "banking")

	trials, err := openProvisioner(t, api).ListTrials(context.Background())
	if err != nil {
		t.Fatalf("list trials: %v", err)
	}

	var found bool
	for _, tr := range trials {
		if tr.TenantCode != "listed-co" {
			continue
		}
		found = true
		// The address is the point of the listing. A tenant somebody can see
		// but not attribute is one they cannot decide anything about.
		if tr.Email != "listed@example.test" {
			t.Errorf("the trial is attributed to %q", tr.Email)
		}
		if tr.Industry != "banking" {
			t.Errorf("the trial records industry %q, want banking", tr.Industry)
		}
		if tr.ConfirmedAt.IsZero() {
			t.Error("no confirmation time; only confirmed trials should be listed")
		}
	}
	if !found {
		t.Errorf("a confirmed trial is missing from the listing (%d listed)", len(trials))
	}

	// The default tenant is not a trial and must not appear: it would be the
	// one row in this listing somebody could act on by mistake.
	for _, tr := range trials {
		if tr.TenantCode == "default" {
			t.Error("the default tenant is listed as a trial")
		}
	}
}

// TestDeletingATrialTenantLeavesNothingBehind is the guard on an irreversible
// operation.
//
// Thirty-four tables carry a tenant_id and none of them cascade, so a delete
// that missed one would either fail loudly on the foreign key — fine — or
// leave rows belonging to a tenant that no longer exists, which is not: the
// tenant code becomes reusable, and the next trial to take it inherits
// somebody else's audit trail.
//
// So this does not check a list of tables. It asks the database which tables
// carry a tenant_id and checks every one of them, which is the same question
// the deletion itself asks.
func TestDeletingATrialTenantLeavesNothingBehind(t *testing.T) {
	api, mailer := newTrialTest(t, true)
	confirmATrial(t, api, mailer, "deleted@example.test", "deleted-co", "hospital")

	// A tenant with a pack in it, so the delete has something to do: fifteen
	// accounts, nine organizations, applications, attributes, audit entries.
	var tenantID string
	queryOne(t, api.dsn, "SELECT id FROM tenants WHERE code = $1", []any{"deleted-co"}, &tenantID)

	deleted, err := openProvisioner(t, api).DeleteTrialTenant(context.Background(), "deleted-co")
	if err != nil {
		t.Fatalf("delete the trial tenant: %v", err)
	}
	if deleted.Rows == 0 {
		t.Error("the delete reports removing no rows from a tenant that was filled with a pack")
	}
	if deleted.Email != "deleted@example.test" {
		t.Errorf("the delete reports the address %q", deleted.Email)
	}

	for _, table := range tenantScopedTables(t, api) {
		var remaining int
		queryOne(t, api.dsn,
			"SELECT count(*) FROM "+pq(table)+" WHERE tenant_id = $1", []any{tenantID}, &remaining)
		if remaining != 0 {
			t.Errorf("%s still holds %d row(s) for a tenant that was deleted", table, remaining)
		}
	}

	var tenants int
	queryOne(t, api.dsn, "SELECT count(*) FROM tenants WHERE code = $1", []any{"deleted-co"}, &tenants)
	if tenants != 0 {
		t.Errorf("the tenant row survived")
	}

	// And the address is free again. Deleting the tenant without releasing the
	// address would leave somebody permanently unable to try again, for a
	// tenant that no longer exists.
	again := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "deleted@example.test",
		"companyName": "Second Go",
		"tenantCode":  "deleted-co",
	})
	if again.Status != http.StatusOK {
		t.Errorf("after deleting the tenant the address is still refused: %d %s",
			again.Status, again.Code)
	}
}

func TestTheCommandLineRefusesTenantsNoTrialCreated(t *testing.T) {
	api, _ := newTrialTest(t, true)

	// `default` exists on every deployment and is the one nobody could afford
	// to lose. Nothing about the command should make it reachable.
	_, err := openProvisioner(t, api).DeleteTrialTenant(context.Background(), "default")
	if !errors.Is(err, provision.ErrNotATrialTenant) {
		t.Errorf("deleting the default tenant answered %v, want ErrNotATrialTenant", err)
	}

	var tenants int
	queryOne(t, api.dsn, "SELECT count(*) FROM tenants WHERE code = 'default'", nil, &tenants)
	if tenants != 1 {
		t.Fatal("the default tenant is gone")
	}
}

func TestPruningReleasesCodesHeldByLinksNobodyOpened(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "pruned@example.test",
		"companyName": "Pruned",
		"tenantCode":  "pruned-code",
	})
	mailer.waitFor(t, before+1)
	api.execSQL(t, `UPDATE trial_requests SET expires_at = now() - interval '1 hour'
		WHERE tenant_code = $1`, "pruned-code")

	removed, err := openProvisioner(t, api).PruneRequests(context.Background())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed == 0 {
		t.Error("pruning removed nothing while an expired unconfirmed request existed")
	}

	freed := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "wants-pruned@example.test",
		"companyName": "Wants It",
		"tenantCode":  "pruned-code",
	})
	if freed.Status != http.StatusOK {
		t.Errorf("the pruned code is still refused: %d %s", freed.Status, freed.Code)
	}
}

// tenantScopedTables asks the database which tables carry a tenant_id, so the
// guard above cannot fall behind a migration.
func tenantScopedTables(t *testing.T, api *apiTest) []string {
	t.Helper()

	db, err := sql.Open("pgx", api.dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT c.table_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_name = c.table_name AND t.table_schema = c.table_schema
		WHERE c.table_schema = 'public' AND c.column_name = 'tenant_id'
		  AND t.table_type = 'BASE TABLE' AND c.table_name <> 'trial_requests'
		ORDER BY c.table_name`)
	if err != nil {
		t.Fatalf("read tenant-scoped tables: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan a table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tenant-scoped tables: %v", err)
	}
	// A guard that silently checked nothing would pass forever.
	if len(tables) < 20 {
		t.Fatalf("only %d tenant-scoped tables found; this guard is not looking at "+
			"the schema it thinks it is", len(tables))
	}
	return tables
}

// pq quotes a table name read from the catalogue.
func pq(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// queryOne reads a single value straight from the test database, for the
// questions that are about rows rather than about what the API says.
func queryOne(t *testing.T, dsn, query string, args []any, dest any) {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.QueryRow(query, args...).Scan(dest); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

// The landing page toggle, asked at the endpoint the console reads.
//
// Two states and one endpoint, because the whole of this feature on the server
// is a boolean read from configuration. What the console does with each answer
// is in web/src/routing.test.ts, which can set the flag both ways without a
// second server.
func TestTheRootAddressSaysWhetherItHasAPageOfItsOwn(t *testing.T) {
	// Off, which is the default and what every deployment did before this
	// existed. Asserted rather than assumed: this is the answer that keeps
	// ordinary installations behaving as they always have.
	api, _ := newTrialTest(t, false)
	got := api.do(http.MethodGet, "/api/v1/landing", "", nil)
	if got.Status != http.StatusOK {
		t.Fatalf("landing: %d %s", got.Status, got.Code)
	}
	var answer struct {
		Enabled bool `json:"enabled"`
	}
	got.into(t, &answer)
	if answer.Enabled {
		t.Error("a deployment that did not ask for a landing page says it has one")
	}
}

func TestTheLandingPageIsOfferedWhereItWasAskedFor(t *testing.T) {
	api := newLandingTest(t)

	got := api.do(http.MethodGet, "/api/v1/landing", "", nil)
	if got.Status != http.StatusOK {
		t.Fatalf("landing: %d %s", got.Status, got.Code)
	}
	var answer struct {
		Enabled bool `json:"enabled"`
	}
	got.into(t, &answer)
	if !answer.Enabled {
		t.Error("PORTICO_LANDING_PAGE is on and the endpoint says otherwise")
	}

	// Readable without signing in, which is the only way it is any use: it is
	// read before the console renders anything for a visitor who has no
	// account.
	if got.Code != "SUCCESS" {
		t.Errorf("answered %s to an anonymous caller", got.Code)
	}
}

// newLandingTest builds a server with the landing page switched on.
func newLandingTest(t *testing.T) *apiTest {
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
	cfg.LandingPage = true

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}
}

// A relay that will not take the message.
//
// The most likely thing to go wrong with this feature on a real deployment,
// and the one furthest outside its control: a quota reached, a credential
// rotated, a network that drops SMTP. None of it is the visitor's doing and
// none of it is a defect in this server.
type failingMailer struct{}

func (failingMailer) Send(context.Context, notify.Message) error {
	return errors.New("dial failed: EOF")
}

func TestAMailRelayThatFailsIsNotReportedAsAServerFault(t *testing.T) {
	silenceLogs(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = testdb.DSN(t)
	cfg.InitialAdminUsername = adminUsername
	cfg.InitialAdminPassword = adminPassword
	cfg.AuthRateLimit, cfg.AuthRateLimitBurst = 100000, 100000
	cfg.TrialSignup = true
	cfg.TrialMaxTenants = 50

	srv, err := server.New(cfg, server.WithMailer(failingMailer{}))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}

	got := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "unreachable@example.test",
		"companyName": "Unreachable",
		"tenantCode":  "unreachable-co",
	})

	// 503 rather than 500: the request was fine, this server is fine, and a
	// dependency outside both is not. Reported as "something went wrong on
	// the server" it sends the visitor away believing the product is broken
	// and tells whoever runs the demonstration nothing.
	if got.Status != http.StatusServiceUnavailable {
		t.Errorf("a failed send answered %d, want 503", got.Status)
	}
	if got.Code != "TRIAL_MAIL_FAILED" {
		t.Errorf("answered %s, want TRIAL_MAIL_FAILED", got.Code)
	}
	if strings.Contains(got.Message, "EOF") || strings.Contains(got.Message, "dial") {
		t.Errorf("the message repeats the internal failure to the caller: %q", got.Message)
	}

	// And the reservation is released, so the same details can be retried the
	// moment the relay is working again.
	retry := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":       "someone@example.test",
		"companyName": "Someone",
		"tenantCode":  "unreachable-co",
	})
	if retry.Code == "TRIAL_CODE_TAKEN" {
		t.Error("a send that failed left the tenant code reserved")
	}
}

func TestTheOrganizationNameIsOptional(t *testing.T) {
	api, mailer := newTrialTest(t, true)

	// No companyName at all. It used to be the one field on this form asking
	// for something the product does not need — a tenant works with its code
	// as its name, and somebody trying a demonstration has not decided what
	// to call it yet.
	before := len(mailer.sent())
	asked := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "nameless@example.test",
		"tenantCode": "nameless-co",
	})
	if asked.Status != http.StatusOK {
		t.Fatalf("a request with no organization name: %d %s %s",
			asked.Status, asked.Code, asked.Message)
	}

	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	confirmed := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token})
	if confirmed.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirmed.Status, confirmed.Code, confirmed.Message)
	}

	var out struct {
		TenantCode string `json:"tenantCode"`
		TenantName string `json:"tenantName"`
	}
	confirmed.into(t, &out)

	// The code, rather than an empty name that would leave the console showing
	// a tenant with no title anywhere it is displayed.
	if out.TenantName != out.TenantCode {
		t.Errorf("the tenant is named %q, want its code %q", out.TenantName, out.TenantCode)
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

// The four checks that stand between a demonstration and somebody abusing it.
//
// Each of these is here because the rule it tests is invisible from the
// outside until it fails, and each failure lands on a different party: the
// first two on the demonstration itself, the third on a stranger who never
// asked for anything, and the fourth on every tenant that already exists.

func TestAPlusAddressIsNotASecondPerson(t *testing.T) {
	// "One tenant per address" was one tenant per spelling, and sub-addressing
	// turns one mailbox into an unlimited supply of spellings. A single inbox
	// could take the whole quota one plus-sign at a time, with every check in
	// front reporting the address as new — because as far as they could see,
	// it was.
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "plus@example.test",
		"tenantCode": "plus-first",
	})
	token := tokenFromLink(t, mailer.waitFor(t, before+1).Body)
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); got.Status != http.StatusOK {
		t.Fatalf("first trial: %d %s %s", got.Status, got.Code, got.Message)
	}

	again := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "plus+another@example.test",
		"tenantCode": "plus-second",
	})
	if again.Status != http.StatusConflict || again.Code != "TRIAL_EMAIL_USED" {
		t.Errorf("a +sub-address of an address that already has a tenant answered "+
			"%d %s, want 409 TRIAL_EMAIL_USED", again.Status, again.Code)
	}
}

func TestOneMailboxCannotBeMadeToReceiveEndlessLinks(t *testing.T) {
	// The one rule here that protects somebody who is not using this product.
	//
	// Nothing stops a stranger's address being typed into the form, and the
	// unique index cannot help: it is partial on confirmed rows, and an
	// unconfirmed request has already put a message in their inbox. Given a
	// different tenant code each time, nothing else collides either.
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	for i := range 3 {
		got := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
			"email":      "victim@example.test",
			"tenantCode": fmt.Sprintf("bomb-%d", i),
		})
		if got.Status != http.StatusOK {
			t.Fatalf("request %d answered %d %s; the cap is meant to allow a few "+
				"so that somebody who lost the first message can ask again",
				i+1, got.Status, got.Code)
		}
	}
	mailer.waitFor(t, before+3)

	stopped := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "victim@example.test",
		"tenantCode": "bomb-4",
	})
	if stopped.Status != http.StatusTooManyRequests || stopped.Code != "TRIAL_TOO_MANY_FOR_EMAIL" {
		t.Errorf("the fourth message to one address answered %d %s, want 429 "+
			"TRIAL_TOO_MANY_FOR_EMAIL", stopped.Status, stopped.Code)
	}
	// And it sent nothing, which is the half the status code does not cover.
	if sent := len(mailer.sent()); sent != before+3 {
		t.Errorf("%d messages sent, want %d — the refusal mailed the address anyway",
			sent, before+3)
	}

	// Counted by mailbox, not by spelling: the same address with a tag on it
	// is the same inbox being filled.
	tagged := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "victim+evade@example.test",
		"tenantCode": "bomb-5",
	})
	if tagged.Code != "TRIAL_TOO_MANY_FOR_EMAIL" {
		t.Errorf("a +sub-address answered %s, so the cap counts spellings and "+
			"the inbox can still be filled", tagged.Code)
	}
}

func TestAThrowawayMailboxIsRefused(t *testing.T) {
	// The address is the whole of the identity check, and what makes that
	// thin claim worth anything is that somebody could be reached at it
	// afterwards. A ten-minute mailbox satisfies the form and none of the
	// intent.
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	for _, address := range []string{
		"someone@mailinator.com",
		// Subdomains too: several of these services hand addresses out at
		// arbitrary ones, so matching the exact name would match the one
		// spelling nobody has to use.
		"someone@mail.guerrillamail.com",
	} {
		got := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
			"email":      address,
			"tenantCode": "throwaway",
		})
		if got.Status != http.StatusUnprocessableEntity || got.Code != "TRIAL_EMAIL_DOMAIN_BLOCKED" {
			t.Errorf("%s answered %d %s, want 422 TRIAL_EMAIL_DOMAIN_BLOCKED",
				address, got.Status, got.Code)
		}
	}
	if sent := len(mailer.sent()); sent != before {
		t.Errorf("%d messages sent for refused addresses, want none", sent-before)
	}

	// And the code they asked for is still free, since nothing was reserved.
	ok := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "real@example.test",
		"tenantCode": "throwaway",
	})
	if ok.Status != http.StatusOK {
		t.Errorf("a refused request reserved the code anyway: %d %s", ok.Status, ok.Code)
	}
}

func TestTheWholeDemonstrationHasAnHourlyCeiling(t *testing.T) {
	// Every other limit here is per-something, and anything per-something is
	// defeated by having more of that thing — more addresses, more clients.
	// This one is not, and what it protects is not the demonstration: a
	// sending quota and a sender reputation are spent by every message that
	// leaves, and losing either takes password recovery down for the tenants
	// that already exist.
	api, mailer := newTrialTest(t, true)

	// Aged into place rather than requested thirty times over: the per-client
	// cap would refuse the sixth, and what is under test is the ceiling above
	// all of them. These rows are what the last hour looked like.
	api.execSQL(t, `
		INSERT INTO trial_requests
			(id, email, email_key, company_name, tenant_code, industry,
			 token_hash, expires_at, request_ip, created_at)
		SELECT
			'busy-' || i, 'b' || i || '@example.test', 'b' || i || '@example.test',
			'Busy', 'busy-' || i, 'generic', 'hash-' || i,
			now() + interval '1 day', '10.0.0.' || i, now() - interval '5 minutes'
		FROM generate_series(1, 30) AS i`)

	before := len(mailer.sent())
	full := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "unlucky@example.test",
		"tenantCode": "one-too-many",
	})
	if full.Status != http.StatusTooManyRequests || full.Code != "TRIAL_BUSY" {
		t.Errorf("answered %d %s, want 429 TRIAL_BUSY", full.Status, full.Code)
	}
	if sent := len(mailer.sent()); sent != before {
		t.Errorf("the refusal sent %d messages, want none", sent-before)
	}

	// An hour later the ceiling has moved with the window, rather than being
	// a total the demonstration can exhaust once and never recover from.
	api.execSQL(t, `UPDATE trial_requests SET created_at = created_at - interval '2 hours'
		WHERE id LIKE 'busy-%'`)
	recovered := api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "unlucky@example.test",
		"tenantCode": "one-too-many",
	})
	if recovered.Status != http.StatusOK {
		t.Errorf("after the window passed the answer is still %d %s; the ceiling "+
			"is a total rather than a rate", recovered.Status, recovered.Code)
	}
}

func TestALinkLastsADayRatherThanAnAfternoon(t *testing.T) {
	// The TTL is the constant, and this is the assertion that it reaches the
	// row. A link that outlives its stated life is worse than one that dies
	// early: the visitor is told a day and finds out otherwise at the moment
	// they act on it.
	api, mailer := newTrialTest(t, true)

	before := len(mailer.sent())
	api.do(http.MethodPost, "/api/v1/trial", "", map[string]string{
		"email":      "patient@example.test",
		"tenantCode": "patient",
	})
	message := mailer.waitFor(t, before+1)
	token := tokenFromLink(t, message.Body)

	// Still good with an hour to spare, which two hours would not have been.
	api.execSQL(t, `UPDATE trial_requests SET created_at = created_at - interval '23 hours',
		expires_at = expires_at - interval '23 hours' WHERE tenant_code = $1`, "patient")
	if got := api.do(http.MethodPost, "/api/v1/trial/confirm", "",
		map[string]string{"token": token}); got.Status != http.StatusOK {
		t.Fatalf("a link 23 hours old answered %d %s %s, want 200",
			got.Status, got.Code, got.Message)
	}

	// And the message says the number the constant says, rather than a second
	// copy of it that drifts. It printed "within 2h0m0s" for as long as it
	// existed, which is not a sentence anybody wrote on purpose.
	//
	// The number rather than the sentence: the wording is translated copy and
	// belongs to whoever writes it, while the figure in it has to come from
	// TrialTokenTTL or the message is telling somebody a deadline the server
	// does not keep.
	hours := fmt.Sprintf("%d hours", int(service.TrialTokenTTL.Hours()))
	if !strings.Contains(message.Body, hours) {
		t.Errorf("the message does not state the life of the link in words (%q):\n%s",
			hours, message.Body)
	}
}
