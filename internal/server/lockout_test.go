package server_test

import (
	"net/http"
	"testing"
)

// Account lockout: a control against online guessing, counted per account.
//
// It is not the same thing as the per-IP throttling the deployment guide
// asks a reverse proxy for, and neither substitutes for the other. A proxy
// rate limit stops one source hammering the endpoint and does nothing about
// a slow spray from many addresses at one account; this stops the second.
//
// The properties worth holding are: guessing stops working, the answers
// never become an oracle for which accounts exist, a lock cannot be used to
// keep somebody out indefinitely, and there is more than one way back in.

const lockoutVictim = "lockme"
const lockoutPassword = "correct-horse-battery-1"

// setLockout configures the tenant, since the defaults are deliberately
// loose enough to be invisible in normal use.
func (a *apiTest) setLockout(token string, threshold, minutes int) {
	a.t.Helper()

	current := a.do(http.MethodGet, "/api/v1/settings", token, nil)
	var settings map[string]any
	current.into(a.t, &settings)

	settings["lockoutThreshold"] = threshold
	settings["lockoutDurationMinutes"] = minutes

	res := a.do(http.MethodPut, "/api/v1/settings", token, settings)
	if res.Status != http.StatusOK {
		a.t.Fatalf("set lockout settings: %d %s %s", res.Status, res.Code, res.Message)
	}
}

// attempt signs in and returns the envelope, without failing the test.
func (a *apiTest) attempt(username, password string) response {
	return a.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"tenant": "", "identifier": username, "password": password,
	})
}

func TestLockoutStopsGuessing(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 3, 15)
	api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 1; i <= 3; i++ {
		res := api.attempt(lockoutVictim, "wrong-password-guess")
		if res.Code != "INVALID_CREDENTIALS" {
			t.Fatalf("attempt %d = %s, want INVALID_CREDENTIALS", i, res.Code)
		}
	}

	// The right password now does not work, which is the whole point: an
	// attacker who eventually guesses it gets a lock rather than a session.
	res := api.attempt(lockoutVictim, lockoutPassword)
	if res.Status != http.StatusUnauthorized || res.Code != "ACCOUNT_LOCKED" {
		t.Fatalf("sign-in after lockout = %d %s, want 401 ACCOUNT_LOCKED",
			res.Status, res.Code)
	}
}

// The lock must not become a way to tell which accounts exist. A wrong guess
// gets the same answer whether the account is locked, unlocked, or absent.
func TestLockoutIsNotAnEnumerationOracle(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 2, 15)
	api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 0; i < 4; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}

	locked := api.attempt(lockoutVictim, "still-wrong")
	absent := api.attempt("no-such-account-anywhere", "still-wrong")

	if locked.Code != absent.Code || locked.Status != absent.Status {
		t.Errorf("a wrong guess at a locked account answers %d %s but at a "+
			"non-existent one %d %s; the difference says which accounts exist",
			locked.Status, locked.Code, absent.Status, absent.Code)
	}
	if locked.Code != "INVALID_CREDENTIALS" {
		t.Errorf("wrong guess at a locked account = %s, want INVALID_CREDENTIALS",
			locked.Code)
	}
}

// Guessing at a locked account must not push the lock further out, or
// anyone who knows a username could keep that person locked out for as long
// as they cared to keep trying.
func TestGuessingDoesNotExtendALock(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 2, 15)
	api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 0; i < 2; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}

	first := api.lockedUntil(t, admin, lockoutVictim)
	if first == "" {
		t.Fatal("the account was not locked after reaching the threshold")
	}

	for i := 0; i < 5; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}

	if again := api.lockedUntil(t, admin, lockoutVictim); again != first {
		t.Errorf("further guesses moved the lock from %s to %s; a lock anyone "+
			"can extend is a denial of service anyone can perform", first, again)
	}
}

func TestAdministratorCanUnlockWithoutChangingThePassword(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 2, 15)
	userID := api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 0; i < 2; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}
	if api.attempt(lockoutVictim, lockoutPassword).Code != "ACCOUNT_LOCKED" {
		t.Fatal("the account was not locked")
	}

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/unlock", admin, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("unlock: %d %s %s", res.Status, res.Code, res.Message)
	}

	// The original password still works — an administrator clearing a lock
	// should not have to hand out a new password over the phone.
	if got := api.attempt(lockoutVictim, lockoutPassword); got.Status != http.StatusOK {
		t.Fatalf("sign-in after unlock = %d %s, want 200", got.Status, got.Code)
	}
}

// Resetting the password is the other way back in, and it has to clear the
// lock too: an administrator who resets a locked account's password and
// leaves it locked has not helped anybody.
func TestResettingThePasswordClearsALock(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 2, 15)
	userID := api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 0; i < 2; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}

	replacement := "brand-new-password-2"
	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/password", admin,
		map[string]string{"newPassword": replacement})
	if res.Status != http.StatusOK {
		t.Fatalf("reset password: %d %s %s", res.Status, res.Code, res.Message)
	}

	if got := api.attempt(lockoutVictim, replacement); got.Status != http.StatusOK {
		t.Fatalf("sign-in after reset = %d %s, want 200", got.Status, got.Code)
	}
}

// A successful sign-in forgets the failures before it, so somebody who
// mistypes twice a week never accumulates their way into a lock.
func TestSuccessResetsTheCount(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 3, 15)
	api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	api.attempt(lockoutVictim, "wrong-password-guess")
	api.attempt(lockoutVictim, "wrong-password-guess")

	if got := api.attempt(lockoutVictim, lockoutPassword); got.Status != http.StatusOK {
		t.Fatalf("sign-in below the threshold = %d %s, want 200", got.Status, got.Code)
	}

	// Two more must not tip it over: the count started again.
	api.attempt(lockoutVictim, "wrong-password-guess")
	api.attempt(lockoutVictim, "wrong-password-guess")

	if got := api.attempt(lockoutVictim, lockoutPassword); got.Status != http.StatusOK {
		t.Fatalf("sign-in after a success reset the count = %d %s, want 200",
			got.Status, got.Code)
	}
}

// A threshold of zero switches the whole thing off, which a deployment
// behind a proxy it trusts may well want.
func TestLockoutCanBeSwitchedOff(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.setLockout(admin, 0, 15)
	api.createUser(admin, lockoutVictim, lockoutPassword, "USER")

	for i := 0; i < 10; i++ {
		api.attempt(lockoutVictim, "wrong-password-guess")
	}

	if got := api.attempt(lockoutVictim, lockoutPassword); got.Status != http.StatusOK {
		t.Fatalf("sign-in with lockout off = %d %s, want 200", got.Status, got.Code)
	}
}

// lockedUntil reads the lock an administrator would see on the user list.
func (a *apiTest) lockedUntil(t *testing.T, adminToken, username string) string {
	t.Helper()

	res := a.do(http.MethodGet, "/api/v1/users?keyword="+username, adminToken, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list users: %d %s", res.Status, res.Code)
	}

	var page struct {
		Items []struct {
			Username    string `json:"username"`
			LockedUntil string `json:"lockedUntil"`
		} `json:"items"`
	}
	res.into(t, &page)

	for _, item := range page.Items {
		if item.Username == username {
			return item.LockedUntil
		}
	}
	t.Fatalf("no user named %s in the listing", username)
	return ""
}
