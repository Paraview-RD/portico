package server_test

import (
	"net/http"
	"testing"
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

	// LoginWithCode does not check SMSLoginEnabled/CanDeliverSMS itself --
	// see its doc comment in internal/service/sms_login.go. With the
	// method unavailable no code could ever have been issued, so the code
	// lookup finds nothing live and answers the same 401 INVALID_SMS_CODE
	// it would for any other phone number with no pending code. Confirmed
	// empirically here rather than assumed: an initial version of this test
	// expected 503, and running it against the real handler is what showed
	// the actual (and correct) behavior.
	login := api.do(http.MethodPost, "/api/v1/auth/sms/login", "", map[string]string{
		"phone": "+8613800000000",
		"code":  "123456",
	})
	if login.Status != http.StatusUnauthorized || login.Code != "INVALID_SMS_CODE" {
		t.Errorf("POST /auth/sms/login with no live code = %d %s, want 401 INVALID_SMS_CODE",
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
