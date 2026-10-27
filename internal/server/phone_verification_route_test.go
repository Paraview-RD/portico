package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// newPhoneVerificationTest is newSMSLoginTest's counterpart: a server with a
// real SMS sender wired in, but no tenant setting touched -- unlike SMS
// login, binding a phone number needs only CanDeliverSMS(), not
// settings.SMSLoginEnabled (see PhoneVerificationService.RequestPhoneChange).
func newPhoneVerificationTest(t *testing.T) (*apiTest, *recordingSMS) {
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

	return &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}, sms
}

// TestPhoneVerificationEndToEnd is the success path: request a code, read
// what the fake sender captured, confirm it, and prove the number now works
// as a sign-in identifier -- the same shape TestUpdateOwnProfile asserted
// for email, which needs no such round trip.
func TestPhoneVerificationEndToEnd(t *testing.T) {
	api, sms := newPhoneVerificationTest(t)
	admin := api.adminToken()

	api.createUser(admin, "priya.user", "profile-pass-1", "USER")
	token := api.loginTo("", "priya.user", "profile-pass-1")

	const phone = "+8613800004444"
	req := api.do(http.MethodPost, "/api/v1/users/me/phone/verification-code", token, map[string]string{
		"phone": phone,
	})
	if req.Status != http.StatusOK {
		t.Fatalf("request verification code: %d %s %s", req.Status, req.Code, req.Message)
	}

	sent := sms.waitFor(t, 1)
	if sent.phone != phone {
		t.Errorf("sent to %q, want %q", sent.phone, phone)
	}
	if sent.kind != notify.SMSKindPhoneVerification {
		t.Errorf("kind = %q, want %q", sent.kind, notify.SMSKindPhoneVerification)
	}
	code := sent.params["Code"]
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	confirm := api.do(http.MethodPost, "/api/v1/users/me/phone/confirm", token, map[string]string{
		"phone": phone, "code": code,
	})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirm.Status, confirm.Code, confirm.Message)
	}

	me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	var profile struct {
		Phone string `json:"phone"`
	}
	me.into(t, &profile)
	if profile.Phone != phone {
		t.Errorf("phone = %q, want %q", profile.Phone, phone)
	}

	// The number is now a working sign-in identifier, same as email already
	// is.
	api.loginTo("", phone, "profile-pass-1")
}

// TestUpdateOwnProfileRefusesAnUnverifiedPhoneChange is what replaced
// TestUpdateOwnProfile's old direct phone assertion: PUT /users/me must
// refuse a phone that differs from the one on file, in favour of the
// request/confirm exchange above.
func TestUpdateOwnProfileRefusesAnUnverifiedPhoneChange(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	api.createUser(admin, "orson.user", "profile-pass-1", "USER")
	token := api.loginTo("", "orson.user", "profile-pass-1")

	res := api.do(http.MethodPut, "/api/v1/users/me", token, map[string]string{
		"displayName": "Orson",
		"phone":       "+8613800005555",
	})
	if res.Status != http.StatusBadRequest || res.Code != "PHONE_CHANGE_REQUIRES_VERIFICATION" {
		t.Fatalf("PUT /users/me with a new phone = %d %s, want 400 PHONE_CHANGE_REQUIRES_VERIFICATION",
			res.Status, res.Code)
	}

	// The account's phone stayed unset -- the refusal did not half-apply.
	me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	var profile struct {
		Phone string `json:"phone"`
	}
	me.into(t, &profile)
	if profile.Phone != "" {
		t.Errorf("phone = %q, want empty", profile.Phone)
	}
}

// A number already bound to another account must be refused synchronously,
// and nothing should be sent to it -- unlike SMS login's phone-scoped
// checks, there is no enumeration concern here (the caller is already a
// known account) to justify staying quiet.
func TestRequestPhoneVerificationRefusesATakenNumberWithoutSendingAnything(t *testing.T) {
	api, sms := newPhoneVerificationTest(t)
	admin := api.adminToken()

	const taken = "+8613800006666"
	api.createUser(admin, "holder.user", "profile-pass-1", "USER")
	holderToken := api.loginTo("", "holder.user", "profile-pass-1")

	req := api.do(http.MethodPost, "/api/v1/users/me/phone/verification-code", holderToken, map[string]string{
		"phone": taken,
	})
	if req.Status != http.StatusOK {
		t.Fatalf("request verification code: %d %s %s", req.Status, req.Code, req.Message)
	}
	sent := sms.waitFor(t, 1)
	confirm := api.do(http.MethodPost, "/api/v1/users/me/phone/confirm", holderToken, map[string]string{
		"phone": taken, "code": sent.params["Code"],
	})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirm.Status, confirm.Code, confirm.Message)
	}

	api.createUser(admin, "asker.user", "profile-pass-1", "USER")
	askerToken := api.loginTo("", "asker.user", "profile-pass-1")

	res := api.do(http.MethodPost, "/api/v1/users/me/phone/verification-code", askerToken, map[string]string{
		"phone": taken,
	})
	if res.Status != http.StatusConflict || res.Code != "PHONE_TAKEN" {
		t.Fatalf("request verification code for a taken number = %d %s, want 409 PHONE_TAKEN",
			res.Status, res.Code)
	}
	if sms.count() != 1 {
		t.Errorf("sms.count() = %d, want 1 (only the original holder's message)", sms.count())
	}
}
