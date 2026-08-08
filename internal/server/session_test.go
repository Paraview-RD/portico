package server_test

import (
	"net/http"
	"testing"
)

// Sessions: one row per sign-in, so signing out can mean this one.
//
// Before this the only revocation available was bumping token_version, which
// invalidates everything an account holds — signing out on a laptop signed
// you out on your phone. These hold the new behaviour in place, including
// the parts that must NOT have changed: a password change and a disable
// still end everything.

func TestSigningOutEndsOnlyThatSession(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "twodevices", "a-real-password-1", "USER")

	laptop := api.login("twodevices", "a-real-password-1")
	phone := api.login("twodevices", "a-real-password-1")

	if res := api.do(http.MethodPost, "/api/v1/auth/logout", laptop, nil); res.Status != http.StatusOK {
		t.Fatalf("logout: %d %s", res.Status, res.Code)
	}

	if res := api.do(http.MethodGet, "/api/v1/users/me", laptop, nil); res.Status != http.StatusUnauthorized {
		t.Errorf("the session that signed out still works: %d %s", res.Status, res.Code)
	}
	if res := api.do(http.MethodGet, "/api/v1/users/me", phone, nil); res.Status != http.StatusOK {
		t.Errorf("signing out on one device ended the session on another: %d %s",
			res.Status, res.Code)
	}
}

func TestSigningOutEverywhereEndsAllOfThem(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "everywhere", "a-real-password-1", "USER")

	laptop := api.login("everywhere", "a-real-password-1")
	phone := api.login("everywhere", "a-real-password-1")

	res := api.do(http.MethodPost, "/api/v1/auth/logout-everywhere", laptop, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("logout everywhere: %d %s %s", res.Status, res.Code, res.Message)
	}

	for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
		if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusUnauthorized {
			t.Errorf("%s still works after signing out everywhere: %d %s",
				name, got.Status, got.Code)
		}
	}
}

func TestSessionListShowsWhichOneIsYours(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "lister", "a-real-password-1", "USER")

	first := api.login("lister", "a-real-password-1")
	api.login("lister", "a-real-password-1")

	res := api.do(http.MethodGet, "/api/v1/users/me/sessions", first, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list sessions: %d %s", res.Status, res.Code)
	}

	var sessions []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	res.into(t, &sessions)

	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	current := 0
	for _, s := range sessions {
		if s.Current {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current, want exactly 1 — the screen has "+
			"to be able to say which row is the reader", current)
	}

	// The token is not in the response and must never be: it is not stored,
	// so there is nothing here to leak even by accident.
	if containsSubstring(string(res.Data), first) {
		t.Error("the session list contains a token")
	}
}

func TestEndingAnotherOfYourOwnSessions(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "ender", "a-real-password-1", "USER")

	keep := api.login("ender", "a-real-password-1")
	drop := api.login("ender", "a-real-password-1")

	// Find the one that is not the caller's.
	res := api.do(http.MethodGet, "/api/v1/users/me/sessions", keep, nil)
	var sessions []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	res.into(t, &sessions)

	var other string
	for _, s := range sessions {
		if !s.Current {
			other = s.ID
		}
	}
	if other == "" {
		t.Fatal("no other session to end")
	}

	if got := api.do(http.MethodDelete, "/api/v1/users/me/sessions/"+other, keep, nil); got.Status != http.StatusOK {
		t.Fatalf("revoke session: %d %s %s", got.Status, got.Code, got.Message)
	}

	if got := api.do(http.MethodGet, "/api/v1/users/me", drop, nil); got.Status != http.StatusUnauthorized {
		t.Errorf("the revoked session still works: %d %s", got.Status, got.Code)
	}
	if got := api.do(http.MethodGet, "/api/v1/users/me", keep, nil); got.Status != http.StatusOK {
		t.Errorf("ending another session ended the caller's own: %d %s", got.Status, got.Code)
	}
}

// A session id is not a capability. Naming somebody else's must fail even
// with a valid token, or the list becomes a way to sign other people out.
func TestCannotEndSomebodyElsesSession(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "victim", "a-real-password-1", "USER")
	api.createUser(admin, "attacker", "a-real-password-2", "USER")

	victimToken := api.login("victim", "a-real-password-1")
	attackerToken := api.login("attacker", "a-real-password-2")

	res := api.do(http.MethodGet, "/api/v1/users/me/sessions", victimToken, nil)
	var sessions []struct {
		ID string `json:"id"`
	}
	res.into(t, &sessions)
	victimSession := sessions[0].ID

	got := api.do(http.MethodDelete, "/api/v1/users/me/sessions/"+victimSession, attackerToken, nil)
	if got.Status == http.StatusOK {
		t.Fatal("one user ended another's session by naming its id")
	}

	if still := api.do(http.MethodGet, "/api/v1/users/me", victimToken, nil); still.Status != http.StatusOK {
		t.Error("the victim's session was ended anyway")
	}
}

// An administrator can see and end what is signed in as somebody, which is
// what "I think my account is compromised" needs on the other end of the
// phone.
func TestAdministratorCanEndAUsersSession(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	userID := api.createUser(admin, "helpme", "a-real-password-1", "USER")

	token := api.login("helpme", "a-real-password-1")

	res := api.do(http.MethodGet, "/api/v1/users/"+userID+"/sessions", admin, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list a user's sessions: %d %s", res.Status, res.Code)
	}
	var sessions []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	res.into(t, &sessions)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].Current {
		t.Error("a session belonging to somebody else was marked as the caller's")
	}

	revoke := api.do(http.MethodDelete,
		"/api/v1/users/"+userID+"/sessions/"+sessions[0].ID, admin, nil)
	if revoke.Status != http.StatusOK {
		t.Fatalf("revoke: %d %s %s", revoke.Status, revoke.Code, revoke.Message)
	}
	if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusUnauthorized {
		t.Errorf("the session survived an administrator ending it: %d %s",
			got.Status, got.Code)
	}
}

// The blunt instruments must still be blunt. Sessions made "sign out" mean
// one device; they must not have made a password change or a disable mean
// one device too.
func TestPasswordChangeAndDisableStillEndEverything(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	t.Run("password change", func(t *testing.T) {
		userID := api.createUser(admin, "pwchange", "a-real-password-1", "USER")
		laptop := api.login("pwchange", "a-real-password-1")
		phone := api.login("pwchange", "a-real-password-1")

		res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
			map[string]string{"newPassword": "a-replacement-password-2"})
		if res.Status != http.StatusOK {
			t.Fatalf("reset password: %d %s", res.Status, res.Code)
		}

		for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
			if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusUnauthorized {
				t.Errorf("%s survived a password change: %d %s", name, got.Status, got.Code)
			}
		}
	})

	t.Run("disable", func(t *testing.T) {
		userID := api.createUser(admin, "todisable", "a-real-password-1", "USER")
		laptop := api.login("todisable", "a-real-password-1")
		phone := api.login("todisable", "a-real-password-1")

		res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", admin, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("disable: %d %s", res.Status, res.Code)
		}

		for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
			if got := api.do(http.MethodGet, "/api/v1/users/me", token, nil); got.Status != http.StatusUnauthorized {
				t.Errorf("%s survived the account being disabled: %d %s",
					name, got.Status, got.Code)
			}
		}
	})
}
