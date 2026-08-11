package server_test

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// Password policy: composition, reuse, and expiry.
//
// The point of the tests is less that each rule works and more that all four
// ways a password enters the system go through the same one — creation,
// self-service change, administrator reset, and completed recovery. A policy
// enforced on three of four is a policy an operator believes they have.

func (a *apiTest) setPasswordPolicy(token string, patch map[string]any) {
	a.t.Helper()

	current := a.do(http.MethodGet, "/api/v1/settings", token, nil)
	var settings map[string]any
	current.into(a.t, &settings)
	for k, v := range patch {
		settings[k] = v
	}

	res := a.do(http.MethodPut, "/api/v1/settings", token, settings)
	if res.Status != http.StatusOK {
		a.t.Fatalf("set password policy: %d %s %s", res.Status, res.Code, res.Message)
	}
}

func TestCompositionRulesApplyWhereverAPasswordIsSet(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setPasswordPolicy(admin, map[string]any{
		"passwordMinLength":        12,
		"passwordRequireUppercase": true,
		"passwordRequireDigit":     true,
		"passwordRequireSymbol":    true,
	})

	// 1. Creating an account. Without this, every account starts life with a
	//    password the policy would have refused.
	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "policyuser", "displayName": "Policy", "role": "USER",
		"password": "shortlower",
	})
	if res.Status != http.StatusBadRequest || res.Code != "WEAK_PASSWORD" {
		t.Fatalf("create with a non-compliant password = %d %s, want 400 WEAK_PASSWORD",
			res.Status, res.Code)
	}

	// The message names every unmet rule at once. Reporting them one per
	// attempt is what makes people give up and reuse something.
	for _, want := range []string{"12 characters", "uppercase", "digit", "symbol"} {
		if !containsSubstring(res.Message, want) {
			t.Errorf("the message does not mention %q: %s", want, res.Message)
		}
	}

	userID := api.createUser(admin, "policyuser", "Compliant-Pass-1!", "USER")

	// 2. Administrator reset.
	reset := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
		map[string]string{"newPassword": "shortlower"})
	if reset.Code != "WEAK_PASSWORD" {
		t.Errorf("administrator reset bypassed the policy: %d %s", reset.Status, reset.Code)
	}

	// 3. Self-service change.
	token := api.login("policyuser", "Compliant-Pass-1!")
	change := api.do(http.MethodPost, "/api/v1/users/me/password", token, map[string]string{
		"currentPassword": "Compliant-Pass-1!", "newPassword": "shortlower",
	})
	if change.Code != "WEAK_PASSWORD" {
		t.Errorf("self-service change bypassed the policy: %d %s", change.Status, change.Code)
	}
}

func TestHistoryRefusesReuse(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setPasswordPolicy(admin, map[string]any{"passwordHistoryDepth": 3})

	userID := api.createUser(admin, "historyuser", "first-password-1", "USER")

	// Depth 3, so the three most recent are refused and the fourth back is
	// available again.
	for _, p := range []string{"second-password-2", "third-password-3"} {
		res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
			map[string]string{"newPassword": p})
		if res.Status != http.StatusOK {
			t.Fatalf("set %s: %d %s %s", p, res.Status, res.Code, res.Message)
		}
	}

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
		map[string]string{"newPassword": "second-password-2"})
	if res.Status != http.StatusBadRequest || res.Code != "PASSWORD_REUSED" {
		t.Errorf("reusing a recent password = %d %s, want 400 PASSWORD_REUSED",
			res.Status, res.Code)
	}

	// Setting the same password twice in a row is the case that fails if the
	// history records the password being replaced rather than the new one.
	same := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
		map[string]string{"newPassword": "third-password-3"})
	if same.Code != "PASSWORD_REUSED" {
		t.Errorf("setting the current password again = %s, want PASSWORD_REUSED", same.Code)
	}
}

func TestHistoryIsOffByDefault(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "nohistory", "first-password-1", "USER")

	// Twice to the same value: with no history configured, nothing objects.
	for i := 0; i < 2; i++ {
		res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
			map[string]string{"newPassword": "second-password-2"})
		if res.Status != http.StatusOK {
			t.Fatalf("attempt %d = %d %s; history should be off by default",
				i+1, res.Status, res.Code)
		}
	}
}

func TestExpiredPasswordRefusesSignInAndHasAWayBack(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "expireduser", "original-password-1", "USER")

	// Every password is younger than a day, so a one-day maximum expires all
	// of them without any clock manipulation.
	api.setPasswordPolicy(admin, map[string]any{"passwordMaxAgeDays": 1})
	api.expirePassword(t, "expireduser")

	res := api.attempt("expireduser", "original-password-1")
	if res.Status != http.StatusUnauthorized || res.Code != "PASSWORD_EXPIRED" {
		t.Fatalf("sign-in with an expired password = %d %s, want 401 PASSWORD_EXPIRED",
			res.Status, res.Code)
	}

	// The way back does not need a session, because there is none to be had.
	change := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"tenant": "", "identifier": "expireduser",
		"currentPassword": "original-password-1", "newPassword": "replacement-password-2",
	})
	if change.Status != http.StatusOK {
		t.Fatalf("change expired password: %d %s %s", change.Status, change.Code, change.Message)
	}

	// And it signs them in, rather than sending them back to type it again.
	var session struct {
		Token string `json:"token"`
	}
	change.into(t, &session)
	if session.Token == "" {
		t.Error("changing an expired password returned no session")
	}
}

// The expired-password endpoint must not become a way to change a password
// without being signed in.
func TestExpiredPasswordEndpointRefusesAFreshPassword(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "freshuser", "original-password-1", "USER")
	api.setPasswordPolicy(admin, map[string]any{"passwordMaxAgeDays": 365})

	res := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"tenant": "", "identifier": "freshuser",
		"currentPassword": "original-password-1", "newPassword": "replacement-password-2",
	})
	if res.Status != http.StatusBadRequest || res.Code != "PASSWORD_NOT_EXPIRED" {
		t.Errorf("changing a password that has not expired = %d %s, "+
			"want 400 PASSWORD_NOT_EXPIRED", res.Status, res.Code)
	}

	// And a wrong current password gets the same answer a sign-in would,
	// rather than confirming the account exists.
	wrong := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"tenant": "", "identifier": "freshuser",
		"currentPassword": "not-the-password", "newPassword": "replacement-password-2",
	})
	if wrong.Code != "INVALID_CREDENTIALS" {
		t.Errorf("wrong current password = %s, want INVALID_CREDENTIALS", wrong.Code)
	}
}

// The floor in auth applies whatever a tenant configures, so a policy cannot
// be used to permit a four-character password.
func TestTenantCannotConfigureBelowTheFloor(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	current := api.do(http.MethodGet, "/api/v1/settings", admin, nil)
	var settings map[string]any
	current.into(t, &settings)
	settings["passwordMinLength"] = 4

	res := api.do(http.MethodPut, "/api/v1/settings", admin, settings)
	if res.Status != http.StatusBadRequest || res.Code != "INVALID_SETTINGS" {
		t.Errorf("a minimum length below the floor = %d %s, want 400 INVALID_SETTINGS",
			res.Status, res.Code)
	}
}

// expirePassword ages a password past any policy, by clearing the timestamp
// — which is what an imported account looks like, and which the policy
// treats as due.
func (a *apiTest) expirePassword(t *testing.T, username string) {
	t.Helper()
	a.execSQL(t, "UPDATE users SET password_changed_at = NULL WHERE username = $1", username)
}

func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Settings grow. A client written against an older shape must not turn a
// security control off simply by not knowing about it.
//
// This is the failure the pointer-valued request body exists to prevent:
// with plain values, omitting lockoutThreshold decodes as zero and switches
// lockout off, from a request whose visible intent was to rename the system.
func TestOmittedSettingsAreLeftAlone(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	api.setLockout(admin, 4, 20)

	// A request from a client that has never heard of lockout.
	res := api.do(http.MethodPut, "/api/v1/settings", admin, map[string]any{
		"systemName": "Renamed Deployment",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("partial settings update: %d %s %s", res.Status, res.Code, res.Message)
	}

	read := api.do(http.MethodGet, "/api/v1/settings", admin, nil)
	var settings struct {
		SystemName             string `json:"systemName"`
		LockoutThreshold       int    `json:"lockoutThreshold"`
		LockoutDurationMinutes int    `json:"lockoutDurationMinutes"`
		PasswordMinLength      int    `json:"passwordMinLength"`
	}
	read.into(t, &settings)

	if settings.SystemName != "Renamed Deployment" {
		t.Errorf("systemName = %q, want the value that was sent", settings.SystemName)
	}
	if settings.LockoutThreshold != 4 || settings.LockoutDurationMinutes != 20 {
		t.Errorf("lockout = %d/%d, want 4/20 — an omitted field switched a "+
			"security control off", settings.LockoutThreshold, settings.LockoutDurationMinutes)
	}
	if settings.PasswordMinLength == 0 {
		t.Error("passwordMinLength was zeroed by an update that never mentioned it")
	}
}

// The password an account was created with counts as used.
//
// Found end to end rather than by a unit test, which is the instructive
// part: history was recorded in setPassword, where self-service change,
// administrator reset, and recovery all meet — but not in creation, which
// hashes its own password. So the very first password was the one value
// that could always be set again. That is also the likeliest one to be
// tried, because it is usually the temporary password an administrator
// dictated and the person then "changes" straight back to.
func TestTheInitialPasswordCountsAsUsed(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setPasswordPolicy(admin, map[string]any{"passwordHistoryDepth": 3})

	userID := api.createUser(admin, "initialpw", "handed-over-password-1", "USER")

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
		map[string]string{"newPassword": "handed-over-password-1"})
	if res.Status != http.StatusBadRequest || res.Code != "PASSWORD_REUSED" {
		t.Errorf("resetting to the password the account was created with = %d %s, "+
			"want 400 PASSWORD_REUSED", res.Status, res.Code)
	}
}

// When somebody's password stops working, told to them rather than sprung on
// them.
//
// The date is on /users/me and not on the settings endpoint, which is
// administrator-only: a normal user does not need to be told the tenant's
// rules to be told their own deadline. It is absent — not zero, not "never" —
// when the tenant does not expire passwords at all, which is the usual case
// and must not render as a configured policy on a home screen.
func TestOwnProfileSaysWhenThisPasswordStopsWorking(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "expiring-user", "original-password-1", "USER")

	// Off, which is the default and the common deployment.
	own := api.do(http.MethodGet, "/api/v1/users/me", api.login("expiring-user", "original-password-1"), nil)
	var profile struct {
		Username          string  `json:"username"`
		PasswordExpiresAt *string `json:"passwordExpiresAt"`
	}
	own.into(t, &profile)
	if profile.PasswordExpiresAt != nil {
		t.Errorf("passwordExpiresAt = %q on a tenant that does not expire "+
			"passwords; an absent policy would render as a configured one",
			*profile.PasswordExpiresAt)
	}

	// On: a date, and one that is in the future for a password just set.
	api.setPasswordPolicy(admin, map[string]any{"passwordMaxAgeDays": 90})

	own = api.do(http.MethodGet, "/api/v1/users/me", api.login("expiring-user", "original-password-1"), nil)
	own.into(t, &profile)
	if profile.PasswordExpiresAt == nil {
		t.Fatal("passwordExpiresAt is absent although the tenant expires " +
			"passwords after 90 days, so nobody is warned before the morning " +
			"the sign-in screen refuses them")
	}
	expires, err := time.Parse(time.RFC3339, *profile.PasswordExpiresAt)
	if err != nil {
		t.Fatalf("passwordExpiresAt = %q, which is not a timestamp: %v",
			*profile.PasswordExpiresAt, err)
	}
	if !expires.After(time.Now()) {
		t.Errorf("passwordExpiresAt = %s, in the past, for a password set "+
			"moments ago under a 90-day policy", expires)
	}

	// A password that ages out mid-session reports a date in the past rather
	// than nothing, so the screen can say "expired" instead of counting down
	// to a day that has been and gone. This is a real sequence and not a
	// contrived one: a token lasts hours, and an administrator may turn the
	// policy on, or shorten it, while people are signed in.
	//
	// The token has to be taken before the password is aged, because a
	// sign-in with an expired password is refused outright — which is the
	// behaviour this warning exists to give somebody notice of.
	token := api.login("expiring-user", "original-password-1")
	api.expirePassword(t, "expiring-user")

	own = api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	own.into(t, &profile)
	if profile.PasswordExpiresAt == nil {
		t.Fatal("passwordExpiresAt is absent for a password the sign-in path " +
			"would now refuse")
	}
	expires, err = time.Parse(time.RFC3339, *profile.PasswordExpiresAt)
	if err != nil {
		t.Fatalf("passwordExpiresAt = %q: %v", *profile.PasswordExpiresAt, err)
	}
	if expires.After(time.Now()) {
		t.Errorf("passwordExpiresAt = %s, in the future, for a password the "+
			"sign-in path already refuses; the warning and the refusal have "+
			"to agree", expires)
	}
}

// The guide panels can be switched off for the whole tenant, and the switch
// has to survive being saved.
//
// Its own test because the partial-update guard above cannot catch the way
// this one breaks. A settings field is wired in five places, and omitting
// the one that writes it to the store leaves a value that reads back
// correctly for the rest of the request and is gone on the next load — so a
// round trip through the API is the only thing that notices.
func TestTurningOffTheGuidePanelsSticks(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	var initial struct {
		ShowGuides bool `json:"showGuides"`
	}
	api.do(http.MethodGet, "/api/v1/settings", admin, nil).into(t, &initial)
	if !initial.ShowGuides {
		t.Error("guides are off by default; a deployment nobody has configured " +
			"is exactly the one whose operators have never seen these screens")
	}

	res := api.do(http.MethodPut, "/api/v1/settings", admin,
		map[string]any{"showGuides": false})
	if res.Status != http.StatusOK {
		t.Fatalf("turn off: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Read out of the table rather than back through the API, and this is
	// the whole point of the test. Update writes the row and then puts the
	// new value straight into an in-memory cache, so a GET afterwards is
	// answered from that cache — a field missing from the map that builds the
	// row reads back correctly for the life of the process and is gone after
	// a restart. Asking the store is the only way to see the difference.
	db, err := sql.Open("pgx", api.dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	var stored string
	err = db.QueryRow(
		`SELECT value FROM system_settings WHERE key = 'show_guides'`).Scan(&stored)
	if err != nil {
		t.Fatalf("show_guides was never written to the store: %v", err)
	}
	if stored != "false" {
		t.Errorf("stored value is %q; the switch would come back on at the "+
			"next restart", stored)
	}
}
