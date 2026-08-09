package server_test

// Which language a message is written in, decided end to end.
//
// The console is translated and a reader picks their own language from a
// menu. These messages arrive somewhere with no menu — a mailbox — so the
// language has to be chosen for them, and this is where the choice is
// checked against what actually leaves the building.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// chineseSubject is a fragment of the translated recovery subject. Matching
// text rather than a marker is deliberate: a test that asserted on some
// invisible tag would pass on a message whose visible words were English.
const chineseSubject = "重置你在"

func createUserWithLanguage(t *testing.T, api *apiTest, admin, username, email, language string) string {
	t.Helper()

	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": username, "displayName": username, "password": username + "-pass-1",
		"email": email,
	})
	if res.Status != http.StatusOK && res.Status != http.StatusCreated {
		t.Fatalf("create %s: %d (%s)", username, res.Status, res.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}

	if language != "" {
		// Through the profile endpoint, which is the one an account uses on
		// itself — the same path a person setting their own language takes.
		res = api.do(http.MethodPut, "/api/v1/users/"+created.ID+"/profile", admin,
			map[string]string{"preferredLanguage": language})
		if res.Status != http.StatusOK {
			t.Fatalf("set language on %s: %d (%s) %s",
				username, res.Status, res.Code, res.Data)
		}
	}
	return created.ID
}

// setTenantLocale sends only the one field, which the settings endpoint
// overlays onto what is already there. Sending the whole object back would
// test the round trip rather than the setting.
func setTenantLocale(t *testing.T, api *apiTest, admin, locale string) response {
	t.Helper()
	return api.do(http.MethodPut, "/api/v1/settings", admin,
		map[string]any{"defaultLocale": locale})
}

func TestARecoveryMessageIsWrittenInTheAccountsOwnLanguage(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	createUserWithLanguage(t, api, admin, "meiling", "meiling@example.com", "zh-CN")

	before := len(mailer.sent())
	api.requestRecovery("EMAIL", "meiling@example.com")
	msg := mailer.waitFor(t, before+1)

	if !strings.Contains(msg.Subject, chineseSubject) {
		t.Errorf("subject = %q, want the Chinese one — the account asked for "+
			"zh-CN and this is the message that arrives where it cannot be "+
			"switched", msg.Subject)
	}
	// The link is the point of the message. A translation that renders
	// without it is a polite note about a password reset with no way to
	// reset one.
	if !strings.Contains(msg.Body, "/reset-password?") {
		t.Errorf("body carries no reset link:\n%s", msg.Body)
	}
}

func TestTheTenantsDefaultDecidesWhenTheAccountHasNoPreference(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	if res := setTenantLocale(t, api, admin, "zh-CN"); res.Status != http.StatusOK {
		t.Fatalf("set tenant locale: %d (%s) %s", res.Status, res.Code, res.Data)
	}
	createUserWithLanguage(t, api, admin, "wenjun", "wenjun@example.com", "")

	before := len(mailer.sent())
	api.requestRecovery("EMAIL", "wenjun@example.com")
	msg := mailer.waitFor(t, before+1)

	if !strings.Contains(msg.Subject, chineseSubject) {
		t.Errorf("subject = %q, want the tenant's language for an account "+
			"that stated none", msg.Subject)
	}
}

func TestAnAccountsOwnPreferenceBeatsTheTenants(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	if res := setTenantLocale(t, api, admin, "zh-CN"); res.Status != http.StatusOK {
		t.Fatalf("set tenant locale: %d (%s) %s", res.Status, res.Code, res.Data)
	}
	createUserWithLanguage(t, api, admin, "alex", "alex@example.com", "en-US")

	before := len(mailer.sent())
	api.requestRecovery("EMAIL", "alex@example.com")
	msg := mailer.waitFor(t, before+1)

	if strings.Contains(msg.Subject, chineseSubject) {
		t.Errorf("subject = %q; the account asked for English inside a "+
			"Chinese tenant, and the more specific preference is the one "+
			"that means something", msg.Subject)
	}
	if !strings.Contains(msg.Subject, "Reset your") {
		t.Errorf("subject = %q, want the English one", msg.Subject)
	}
}

// A preference nothing ships arrives from a directory synchronisation and a
// SCIM push as often as from a form. It must not turn a password reset into
// an error — the account still needs its link.
func TestALanguageThisBuildDoesNotHaveStillGetsALink(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	createUserWithLanguage(t, api, admin, "kirok", "kirok@example.com", "tlh")

	before := len(mailer.sent())
	api.requestRecovery("EMAIL", "kirok@example.com")
	msg := mailer.waitFor(t, before+1)

	if !strings.Contains(msg.Body, "/reset-password?") {
		t.Errorf("no reset link in a message for an account with an "+
			"unrecognised language:\n%s", msg.Body)
	}
}

func TestATenantCannotChooseALanguageThisBuildHasNoMessagesFor(t *testing.T) {
	api, _ := newRecoveryTest(t)
	admin := api.adminToken()

	res := setTenantLocale(t, api, admin, "de-DE")
	if res.Status != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400: a locale with nothing behind "+
			"it would store a setting that silently does nothing",
			res.Status, res.Code)
	}
	if res.Code != "INVALID_SETTINGS" {
		t.Errorf("code = %q, want INVALID_SETTINGS", res.Code)
	}
}

func TestClearingTheTenantLocaleFallsBackToTheDeployment(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	if res := setTenantLocale(t, api, admin, "zh-CN"); res.Status != http.StatusOK {
		t.Fatalf("set tenant locale: %d (%s)", res.Status, res.Code)
	}
	if res := setTenantLocale(t, api, admin, ""); res.Status != http.StatusOK {
		t.Fatalf("clear tenant locale: %d (%s) %s", res.Status, res.Code, res.Data)
	}
	createUserWithLanguage(t, api, admin, "sam", "sam@example.com", "")

	before := len(mailer.sent())
	api.requestRecovery("EMAIL", "sam@example.com")
	msg := mailer.waitFor(t, before+1)

	// The deployment default in tests is the shipped one, English. Empty has
	// to mean "follow the deployment" rather than "keep what was set", or a
	// tenant could never undo a choice.
	if !strings.Contains(msg.Subject, "Reset your") {
		t.Errorf("subject = %q, want English after clearing the tenant's "+
			"choice", msg.Subject)
	}
}
