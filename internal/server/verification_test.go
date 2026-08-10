package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// Registration that has to prove the address it gave.
//
// The gap this closes: until now registration created a usable account with
// whatever email was typed, and that address is both a sign-in identifier
// and where a password-reset link is sent. So somebody could open an account
// under a colleague's address and receive their reset links.

// newVerifyingTest builds an instance with a mail recorder and registration
// open, so the tests can turn verification on and watch what happens.
func newVerifyingTest(t *testing.T) (*apiTest, *recordingMailer) {
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
	cfg.PublicURL = "https://portico.example.com"

	mailer := &recordingMailer{}
	srv, err := server.New(cfg, server.WithMailer(mailer))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}
	api.setRegistration(t, true, true)
	return api, mailer
}

// setRegistration turns self-registration and the verification requirement
// on or off.
func (a *apiTest) setRegistration(t *testing.T, open, verify bool) response {
	t.Helper()
	admin := a.adminToken()

	current := a.do(http.MethodGet, "/api/v1/settings", admin, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["registrationEnabled"] = open
	settings["registrationVerification"] = verify

	return a.do(http.MethodPut, "/api/v1/settings", admin, settings)
}

func verifyTokenFrom(t *testing.T, msg notify.Message) string {
	t.Helper()
	for _, field := range strings.Fields(msg.Body) {
		if !strings.Contains(field, "/verify?") {
			continue
		}
		parsed, err := url.Parse(field)
		if err != nil {
			t.Fatalf("parse verification link %q: %v", field, err)
		}
		if token := parsed.Query().Get("token"); token != "" {
			return token
		}
	}
	t.Fatalf("no verification link in message body:\n%s", msg.Body)
	return ""
}

func (a *apiTest) register(username, email, password string) response {
	a.t.Helper()
	return a.do(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": username, "displayName": username,
		"email": email, "password": password,
	})
}

func TestAnUnverifiedRegistrationCannotSignIn(t *testing.T) {
	api, mailer := newVerifyingTest(t)

	res := api.register("newcomer", "newcomer@example.test", "newcomer-password-1")
	if res.Status != http.StatusOK {
		t.Fatalf("register: %d %s %s", res.Status, res.Code, res.Message)
	}

	var created struct {
		VerificationRequired bool `json:"verificationRequired"`
	}
	res.into(t, &created)
	if !created.VerificationRequired {
		t.Error("the registration response does not say verification is " +
			"required, so the screen cannot tell somebody to check their email")
	}

	login := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "newcomer", "password": "newcomer-password-1",
	})
	if login.Code != "ACCOUNT_UNVERIFIED" {
		t.Fatalf("signing in before confirming = %d %s, want ACCOUNT_UNVERIFIED",
			login.Status, login.Code)
	}

	// The link arrives, and redeeming it lets them in.
	token := verifyTokenFrom(t, mailer.waitFor(t, 1))

	confirm := api.do(http.MethodPost, "/api/v1/auth/register/verify", "",
		map[string]string{"token": token})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirm.Status, confirm.Code, confirm.Message)
	}

	after := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "newcomer", "password": "newcomer-password-1",
	})
	if after.Status != http.StatusOK {
		t.Errorf("signing in after confirming = %d %s", after.Status, after.Code)
	}
}

// A link works once. Redeeming it twice must not be a way to re-verify an
// address somebody has since changed.
func TestAVerificationLinkWorksOnce(t *testing.T) {
	api, mailer := newVerifyingTest(t)

	api.register("once", "once@example.test", "once-password-1")
	token := verifyTokenFrom(t, mailer.waitFor(t, 1))

	if res := api.do(http.MethodPost, "/api/v1/auth/register/verify", "",
		map[string]string{"token": token}); res.Status != http.StatusOK {
		t.Fatalf("first use: %d %s", res.Status, res.Code)
	}

	again := api.do(http.MethodPost, "/api/v1/auth/register/verify", "",
		map[string]string{"token": token})
	if again.Code != "INVALID_VERIFICATION_TOKEN" {
		t.Errorf("reusing a link = %d %s, want INVALID_VERIFICATION_TOKEN",
			again.Status, again.Code)
	}
}

// Asking again invalidates the previous link, so a message somebody else has
// read does not still work.
func TestAskingAgainInvalidatesTheOlderLink(t *testing.T) {
	api, mailer := newVerifyingTest(t)

	api.register("resent", "resent@example.test", "resent-password-1")
	first := verifyTokenFrom(t, mailer.waitFor(t, 1))

	if res := api.do(http.MethodPost, "/api/v1/auth/register/verify/resend", "",
		map[string]string{"destination": "resent@example.test"}); res.Status != http.StatusOK {
		t.Fatalf("resend: %d %s", res.Status, res.Code)
	}
	second := verifyTokenFrom(t, mailer.waitFor(t, 2))

	if first == second {
		t.Fatal("the resent link is the same token as the first")
	}
	if res := api.do(http.MethodPost, "/api/v1/auth/register/verify", "",
		map[string]string{"token": first}); res.Code != "INVALID_VERIFICATION_TOKEN" {
		t.Errorf("the superseded link still works (%d %s)", res.Status, res.Code)
	}
	if res := api.do(http.MethodPost, "/api/v1/auth/register/verify", "",
		map[string]string{"token": second}); res.Status != http.StatusOK {
		t.Errorf("the newest link does not work (%d %s)", res.Status, res.Code)
	}
}

// Resend is public and unauthenticated, so it must answer identically
// whether or not the address belongs to anybody.
//
// The asymmetry with sign-in is deliberate and is the reason this test
// exists next to the one above: sign-in tells an unverified person why they
// were refused, because otherwise they have no way forward — but that
// disclosure is confined to somebody who already has the password. This
// endpoint has no such excuse.
func TestResendTellsNobodyWhetherAnAccountExists(t *testing.T) {
	api, mailer := newVerifyingTest(t)

	api.register("known", "known@example.test", "known-password-1")
	mailer.waitFor(t, 1)

	before := len(mailer.sent())
	stranger := api.do(http.MethodPost, "/api/v1/auth/register/verify/resend", "",
		map[string]string{"destination": "nobody@example.test"})
	if stranger.Status != http.StatusOK || stranger.Code != "SUCCESS" {
		t.Errorf("resend for an unknown address = %d %s; it has to look "+
			"exactly like a hit", stranger.Status, stranger.Code)
	}
	mailer.quiet(t, before)

	// And an address that exists but is already confirmed answers the same.
	token := verifyTokenFrom(t, mailer.sent()[0])
	api.do(http.MethodPost, "/api/v1/auth/register/verify", "", map[string]string{"token": token})

	before = len(mailer.sent())
	settled := api.do(http.MethodPost, "/api/v1/auth/register/verify/resend", "",
		map[string]string{"destination": "known@example.test"})
	if settled.Status != http.StatusOK || settled.Code != "SUCCESS" {
		t.Errorf("resend for a confirmed account = %d %s, want the same "+
			"answer as every other case", settled.Status, settled.Code)
	}
	mailer.quiet(t, before)
}

// Everybody who registered before the requirement existed keeps their
// access. A policy change is not grounds for revoking access somebody
// already has, and the alternative locks out every existing member of a
// deployment the moment an administrator turns this on.
func TestTurningVerificationOnDoesNotLockOutExistingMembers(t *testing.T) {
	api, _ := newVerifyingTest(t)
	api.setRegistration(t, true, false)

	if res := api.register("early", "early@example.test", "early-password-1"); res.Status != http.StatusOK {
		t.Fatalf("register: %d %s %s", res.Status, res.Code, res.Message)
	}
	if res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "early", "password": "early-password-1",
	}); res.Status != http.StatusOK {
		t.Fatalf("sign in before the change: %d %s", res.Status, res.Code)
	}

	api.setRegistration(t, true, true)

	if res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "early", "password": "early-password-1",
	}); res.Status != http.StatusOK {
		t.Errorf("somebody who registered before the requirement was turned on "+
			"is now refused (%d %s); turning this on would lock out every "+
			"existing member", res.Status, res.Code)
	}
}

// Only self-registered accounts are gated. An administrator-created account
// is vouched for by the administrator who created it, and gating it would
// mean an operator who turns this on cannot use the console.
func TestAdministratorCreatedAccountsAreNotGated(t *testing.T) {
	api, _ := newVerifyingTest(t)
	admin := api.adminToken()
	api.createUser(admin, "made-by-admin", "made-by-admin-password-1", "USER")

	if res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "made-by-admin", "password": "made-by-admin-password-1",
	}); res.Status != http.StatusOK {
		t.Errorf("an administrator-created account was refused for being "+
			"unverified (%d %s)", res.Status, res.Code)
	}
}

// Requiring something the deployment cannot send is refused where it is
// asked for, rather than accepted and then stranding every registration on a
// message that never arrives.
func TestVerificationCannotBeRequiredWithNoWayToSendIt(t *testing.T) {
	// The ordinary harness has no mail relay configured.
	api := newAPITest(t)
	if res := api.setRegistration(t, true, false); res.Status != http.StatusOK {
		t.Fatalf("open registration: %d %s", res.Status, res.Code)
	}

	res := api.setRegistration(t, true, true)
	if res.Code != "NO_DELIVERY_CHANNEL" {
		t.Fatalf("requiring verification with no relay = %d %s, want "+
			"NO_DELIVERY_CHANNEL", res.Status, res.Code)
	}

	// The whole request was refused, so registration is untouched — the
	// settings endpoint replaces the object, and accepting the rest of a
	// rejected change would leave a tenant half-configured.
	if res := api.register("unaffected", "unaffected@example.test", "unaffected-password-1"); res.Status != http.StatusOK {
		t.Errorf("registration broke after a refused settings change: %d %s",
			res.Status, res.Code)
	}
}
