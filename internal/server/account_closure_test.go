package server_test

import (
	"net/http"
	"testing"
)

// Closing your own account.
//
// The one place self-disabling is allowed, and the properties that make it
// safe to allow: the password is required, the tenant keeps an
// administrator, everything the account held stops working at once, and it
// can be undone.

func TestClosingYourAccountEndsEverythingAtOnce(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "leaver", "leaver-password-1", "USER")

	// Two sessions, because closing has to reach the one that is not making
	// the request as well.
	first := api.login("leaver", "leaver-password-1")
	second := api.login("leaver", "leaver-password-1")

	res := api.do(http.MethodPost, "/api/v1/users/me/close", first,
		map[string]string{"password": "leaver-password-1"})
	if res.Status != http.StatusOK {
		t.Fatalf("close account: %d %s %s", res.Status, res.Code, res.Message)
	}

	for name, token := range map[string]string{"the one that closed it": first, "the other one": second} {
		after := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
		if after.Status != http.StatusUnauthorized {
			t.Errorf("%s still works after closing (%d %s); a closed account "+
				"with a live session is not closed", name, after.Status, after.Code)
			continue
		}
		// And says which of the two things happened, here as well as at
		// sign-in. One path saying CLOSED and the neighbouring one saying
		// DISABLED is how a person ends up asking their administrator why
		// they were suspended.
		if after.Code != "ACCOUNT_CLOSED" {
			t.Errorf("%s was refused with %s, want ACCOUNT_CLOSED", name, after.Code)
		}
	}

	// And signing in says which of the two things happened. "Disabled" would
	// send somebody to their administrator to ask why they were suspended.
	login := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "leaver", "password": "leaver-password-1",
	})
	if login.Code != "ACCOUNT_CLOSED" {
		t.Errorf("signing in to a closed account = %s, want ACCOUNT_CLOSED", login.Code)
	}
}

// A stolen token must not be enough to destroy the account it was stolen
// from, which is the same reason changing a password requires the old one.
func TestClosingRequiresThePassword(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "cautious", "cautious-password-1", "USER")
	token := api.login("cautious", "cautious-password-1")

	res := api.do(http.MethodPost, "/api/v1/users/me/close", token,
		map[string]string{"password": "not-the-password"})
	if res.Code != "CURRENT_PASSWORD_MISMATCH" {
		t.Errorf("closing with a wrong password = %d %s, want CURRENT_PASSWORD_MISMATCH",
			res.Status, res.Code)
	}

	// And the account still works, which is the part that matters.
	if after := api.do(http.MethodGet, "/api/v1/users/me", token, nil); after.Status != http.StatusOK {
		t.Errorf("a refused closure ended the session anyway: %d %s", after.Status, after.Code)
	}
}

// The tenant has to keep an administrator. Somebody closing the last one
// leaves nobody who could reinstate them.
func TestTheLastAdministratorCannotCloseTheirAccount(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/users/me/close", admin,
		map[string]string{"password": adminPassword})
	if res.Code != "LAST_ADMIN" {
		t.Fatalf("the last administrator closed their account = %d %s, want LAST_ADMIN",
			res.Status, res.Code)
	}

	// With a second administrator there is somebody left, so it is allowed.
	api.createUser(admin, "second-admin", "second-admin-password-1", "SUPER_ADMIN")
	if res := api.do(http.MethodPost, "/api/v1/users/me/close", admin,
		map[string]string{"password": adminPassword}); res.Status != http.StatusOK {
		t.Errorf("closing with another administrator present = %d %s, want 200",
			res.Status, res.Code)
	}
}

// Deactivation rather than deletion is what makes this recoverable, so the
// recovery is asserted rather than assumed.
func TestAnAdministratorCanReinstateAClosedAccount(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "returner", "returner-password-1", "USER")
	token := api.login("returner", "returner-password-1")

	if res := api.do(http.MethodPost, "/api/v1/users/me/close", token,
		map[string]string{"password": "returner-password-1"}); res.Status != http.StatusOK {
		t.Fatalf("close: %d %s", res.Status, res.Code)
	}

	if res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/enable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("enable: %d %s %s", res.Status, res.Code, res.Message)
	}

	login := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "returner", "password": "returner-password-1",
	})
	if login.Status != http.StatusOK {
		t.Fatalf("signing in after reinstatement = %d %s", login.Status, login.Code)
	}

	// And the mark is gone.
	//
	// Signing in cannot establish this, which is worth stating because the
	// first version of this test tried to: the sign-in path checks the
	// status first and only consults the closure mark for an account that is
	// already refused, so a stale mark is invisible there. It is visible to
	// an administrator reading the list — which is the entire reason the
	// column exists — so that is what is asserted. A mutation removing the
	// clearing survived until this assertion replaced the other one.
	var reinstated struct {
		Status   string  `json:"status"`
		ClosedAt *string `json:"closedAt"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+userID, admin, nil).into(t, &reinstated)
	if reinstated.ClosedAt != nil {
		t.Errorf("closedAt is still %s after reinstatement, so the list shows "+
			"an account that reads as closed and signs in perfectly well",
			*reinstated.ClosedAt)
	}
	if reinstated.Status != "ACTIVE" {
		t.Errorf("status = %s after enabling", reinstated.Status)
	}
}

// Reinstating must not resurrect the sessions the closure ended.
//
// This is what the token version bump buys, and nothing else tests it: while
// the account is disabled every request is refused on the status alone, so a
// missing bump is invisible until somebody is reinstated — and then a token
// on a laptop they no longer have starts working again.
func TestReinstatingDoesNotReviveOldSessions(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "revived", "revived-password-1", "USER")
	token := api.login("revived", "revived-password-1")

	if res := api.do(http.MethodPost, "/api/v1/users/me/close", token,
		map[string]string{"password": "revived-password-1"}); res.Status != http.StatusOK {
		t.Fatalf("close: %d %s", res.Status, res.Code)
	}
	if res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/enable", admin, nil); res.Status != http.StatusOK {
		t.Fatalf("enable: %d %s", res.Status, res.Code)
	}

	if after := api.do(http.MethodGet, "/api/v1/users/me", token, nil); after.Status != http.StatusUnauthorized {
		t.Errorf("a token from before the closure works again after "+
			"reinstatement (%d %s); closing has to end sessions permanently, "+
			"not suspend them", after.Status, after.Code)
	}
}

// Closing is its own verb in the trail. A shared user-disable would lose the
// difference between "they left" and "we suspended them", which is exactly
// the question asked afterwards.
func TestClosingIsAuditedAsItself(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "audited", "audited-password-1", "USER")
	token := api.login("audited", "audited-password-1")

	if res := api.do(http.MethodPost, "/api/v1/users/me/close", token,
		map[string]string{"password": "audited-password-1"}); res.Status != http.StatusOK {
		t.Fatalf("close: %d %s", res.Status, res.Code)
	}

	logs := api.do(http.MethodGet, "/api/v1/audit-logs?kind=OPERATION&pageSize=50", admin, nil)
	var page struct {
		Items []struct {
			Action     string `json:"action"`
			ActorName  string `json:"actorName"`
			TargetName string `json:"targetName"`
		} `json:"items"`
	}
	logs.into(t, &page)

	for _, entry := range page.Items {
		if entry.Action == "ACCOUNT_CLOSE" && entry.ActorName == "audited" {
			return
		}
	}
	t.Errorf("no ACCOUNT_CLOSE entry attributed to the person who closed it: %+v", page.Items)
}
