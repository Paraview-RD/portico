package server_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// TestSMSLoginRoutesAnswerRatherThan500 is the smoke test for wiring, not
// for SMSLoginService's own rules (those are covered in
// internal/service/sms_login_test.go). It proves both handlers reach
// smsLogin.RequestCode/LoginWithCode at all -- with a well-formed body,
// against a real default deployment -- rather than panicking or 500ing on
// a nil dependency, or 404ing because a route never got registered.
func TestSMSLoginRoutesAnswerRatherThan500(t *testing.T) {
	api := newAPITest(t)

	code := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{
		"phone": "+8613800000000",
	})
	if code.Status != http.StatusServiceUnavailable || code.Code != "SMS_LOGIN_UNAVAILABLE" {
		t.Errorf("POST /auth/sms/code with no SMS provider configured = %d %s, want 503 SMS_LOGIN_UNAVAILABLE",
			code.Status, code.Code)
	}

	// LoginWithCode now re-checks SMSLoginEnabled/CanDeliverSMS itself,
	// same as RequestCode -- see its doc comment in
	// internal/service/sms_login.go. With the method unavailable, that
	// check fires before any phone/code lookup, so this answers the same
	// 503 SMS_LOGIN_UNAVAILABLE as /auth/sms/code above, not
	// INVALID_SMS_CODE.
	login := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": "+8613800000000",
		"code":  "123456",
	})
	if login.Status != http.StatusServiceUnavailable || login.Code != "SMS_LOGIN_UNAVAILABLE" {
		t.Errorf("POST /auth/sms/login with no SMS provider configured = %d %s, want 503 SMS_LOGIN_UNAVAILABLE",
			login.Status, login.Code)
	}
}

// TestRegistrationStatusReportsSMSLoginEnabled covers the flag added
// alongside the two routes above: a deployment with no SMS provider
// configured must answer false, even before any tenant setting is touched.
func TestRegistrationStatusReportsSMSLoginEnabled(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodGet, "/api/v1/auth/registration-status", "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("registration-status: %d %s", res.Status, res.Code)
	}

	var status struct {
		SMSLoginEnabled bool `json:"smsLoginEnabled"`
	}
	res.into(t, &status)
	if status.SMSLoginEnabled {
		t.Error("smsLoginEnabled = true on a deployment with no SMS provider configured, want false")
	}
}

// recordingSMS captures every Send call, the SMS analogue of recordingMailer
// in recovery_test.go.
type recordingSMS struct {
	mu   sync.Mutex
	sent []sentSMS
}

type sentSMS struct {
	phone  string
	kind   notify.SMSKind
	params map[string]string
}

func (r *recordingSMS) Send(_ context.Context, phone string, kind notify.SMSKind, params map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentSMS{phone, kind, params})
	return nil
}

func (r *recordingSMS) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func (r *recordingSMS) waitFor(t *testing.T, count int) sentSMS {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.sent)
		var got sentSMS
		if n >= count {
			got = r.sent[count-1]
		}
		r.mu.Unlock()
		if n >= count {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for SMS #%d; got %d", count, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quiet asserts that no further message arrives, mirroring
// recordingMailer.quiet in recovery_test.go.
func (r *recordingSMS) quiet(t *testing.T, was int) {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	if now := r.count(); now != was {
		t.Errorf("an SMS was sent when none should have been (%d -> %d)", was, now)
	}
}

// newSMSLoginTest builds a server with a real (recording) SMS sender
// substituted, then turns SMSLoginEnabled on for the default tenant and
// binds phone to the seeded admin account, using the same
// GET-then-PUT-merge pattern setRegistration (verification_test.go) and
// setLockout (lockout_test.go) use for /api/v1/settings, and the same
// PUT /api/v1/users/me the profile tests use for binding contact details.
func newSMSLoginTest(t *testing.T, phone string) (*apiTest, *recordingSMS) {
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

	sms := &recordingSMS{}
	srv, err := server.New(cfg, server.WithSMSSender(sms))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}
	admin := api.adminToken()

	current := api.do(http.MethodGet, "/api/v1/settings", admin, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["smsLoginEnabled"] = true
	if res := api.do(http.MethodPut, "/api/v1/settings", admin, settings); res.Status != http.StatusOK {
		t.Fatalf("enable sms login: %d %s %s", res.Status, res.Code, res.Message)
	}

	if phone != "" {
		// Through the administrator's own endpoint, not /users/me: binding
		// a fresh number through self-service now requires the
		// PhoneVerificationService round trip (see
		// self_service.go's ErrPhoneChangeRequiresVerification), which is
		// its own coverage elsewhere. This fixture only needs a phone
		// bound to sign in with, and an administrator setting one directly
		// -- on any account, including their own -- is exempt from that by
		// design.
		var me struct {
			ID string `json:"id"`
		}
		api.do(http.MethodGet, "/api/v1/users/me", admin, nil).into(t, &me)
		if res := api.do(http.MethodPut, "/api/v1/users/"+me.ID, admin, map[string]string{
			"displayName": "Admin",
			"phone":       phone,
			"role":        string(model.RoleSuperAdmin),
		}); res.Status != http.StatusOK {
			t.Fatalf("bind phone to admin: %d %s %s", res.Status, res.Code, res.Message)
		}
	}

	return api, sms
}

// TestSMSLoginEndToEnd is this task's most important addition: until now
// only the negative/unavailable paths through /auth/sms/code and
// /auth/sms/login had coverage. This exercises the real success path --
// requesting a code, reading the code the fake sender captured, and
// redeeming it for a session.
func TestSMSLoginEndToEnd(t *testing.T) {
	const phone = "+8613800138000"
	api, sms := newSMSLoginTest(t, phone)

	res := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": phone})
	if res.Status != http.StatusOK {
		t.Fatalf("request code: %d %s", res.Status, res.Message)
	}

	sent := sms.waitFor(t, 1)
	if sent.phone != phone {
		t.Errorf("sent to %q, want %q", sent.phone, phone)
	}
	if sent.kind != notify.SMSKindLoginCode {
		t.Errorf("kind = %q, want %q", sent.kind, notify.SMSKindLoginCode)
	}
	code := sent.params["Code"]
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	login := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": phone, "code": code,
	})
	if login.Status != http.StatusOK {
		t.Fatalf("login: %d %s", login.Status, login.Message)
	}
	var session struct {
		Token string `json:"token"`
	}
	login.into(t, &session)
	if session.Token == "" {
		t.Error("want a session token")
	}

	// The session actually works.
	me := api.do(http.MethodGet, "/api/v1/users/me", session.Token, nil)
	if me.Status != http.StatusOK {
		t.Errorf("the issued token did not work: %d %s", me.Status, me.Message)
	}
}

// A wrong code must not be accepted, even against a real, live, pending
// code -- the success path must not have loosened comparison into some
// kind of prefix or case-insensitive match.
func TestSMSLoginRejectsAWrongCodeAgainstALiveOne(t *testing.T) {
	const phone = "+8613800138001"
	api, sms := newSMSLoginTest(t, phone)

	if res := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": phone}); res.Status != http.StatusOK {
		t.Fatalf("request code: %d %s", res.Status, res.Message)
	}
	sent := sms.waitFor(t, 1)
	wrong := "000000"
	if wrong == sent.params["Code"] {
		wrong = "111111"
	}

	res := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": phone, "code": wrong,
	})
	if res.Status != http.StatusUnauthorized || res.Code != "INVALID_SMS_CODE" {
		t.Errorf("wrong code = %d %s, want 401 INVALID_SMS_CODE", res.Status, res.Code)
	}

	// The right code still works afterwards -- one wrong guess must not
	// have burned the live code.
	ok := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": phone, "code": sent.params["Code"],
	})
	if ok.Status != http.StatusOK {
		t.Errorf("the correct code stopped working after one wrong guess: %d %s", ok.Status, ok.Message)
	}

	// And now that it has been spent once, the very same code must not work
	// a second time -- a consumed code that stayed usable would let a
	// leaked or shoulder-surfed code sign in indefinitely, not just once.
	reused := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": phone, "code": sent.params["Code"],
	})
	if reused.Status != http.StatusUnauthorized || reused.Code != "INVALID_SMS_CODE" {
		t.Errorf("reusing an already-consumed code = %d %s, want 401 INVALID_SMS_CODE",
			reused.Status, reused.Code)
	}
}

func TestSMSLoginRequestCodeDoesNotRevealWhetherThePhoneExists(t *testing.T) {
	api, sms := newSMSLoginTest(t, "+8613800138000")

	known := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": "+8613800138000"})
	unknown := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{"phone": "+8619999999999"})

	if known.Status != unknown.Status || known.Status != http.StatusOK {
		t.Errorf("known = %d, unknown = %d, want both 200", known.Status, unknown.Status)
	}

	sms.waitFor(t, 1) // the known phone's message
	sms.quiet(t, 1)   // and nothing more, for the unknown one
}

// A tenant that has not turned the setting on gets the same 503 as a
// deployment with no SMS provider at all, even though this deployment does
// have one configured.
func TestSMSLoginRefusesWhenTheTenantHasNotEnabledIt(t *testing.T) {
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

	sms := &recordingSMS{}
	srv, err := server.New(cfg, server.WithSMSSender(sms))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}

	// The tenant setting is left at its default of off -- this is exactly
	// newAPITest's shape, except with an SMS sender wired in at the process
	// level, to prove the 503 comes from the tenant setting and not from a
	// missing sender.
	res := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{
		"phone": "+8613800138000",
	})
	if res.Status != http.StatusServiceUnavailable || res.Code != "SMS_LOGIN_UNAVAILABLE" {
		t.Errorf("POST /auth/sms/code with the tenant setting off = %d %s, want 503 SMS_LOGIN_UNAVAILABLE",
			res.Status, res.Code)
	}
	sms.quiet(t, 0)
}

// TestSMSLoginRefusesWhenNoSMSSenderIsConfigured duplicates the assertion
// already made by TestSMSLoginRoutesAnswerRatherThan500 above, deliberately:
// that test's job is "the wiring answers rather than 500ing"; this one's is
// "and it is specifically because no sender is configured", named to be
// found next to TestSMSLoginRefusesWhenTheTenantHasNotEnabledIt so the two
// paths to the same 503 are easy to tell apart.
func TestSMSLoginRefusesWhenNoSMSSenderIsConfigured(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodPost, "/api/v1/auth/sms/code", "", map[string]string{
		"phone": "+8613800138000",
	})
	if res.Status != http.StatusServiceUnavailable || res.Code != "SMS_LOGIN_UNAVAILABLE" {
		t.Errorf("POST /auth/sms/code with no SMS provider configured = %d %s, want 503 SMS_LOGIN_UNAVAILABLE",
			res.Status, res.Code)
	}
}
