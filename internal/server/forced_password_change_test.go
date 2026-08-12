package server_test

import (
	"net/http"
	"testing"

	"github.com/Paraview-RD/portico/internal/service"
)

// The default administrator password, and the change it forces.
//
// A release now bootstraps its first administrator with a password that is
// written down in the manual, identical everywhere, and therefore known to
// anybody who can read. The only thing that makes that defensible is that
// the account is unusable until the password is replaced — so these tests
// are the whole justification for the feature, not a detail of it.

// newDefaultPasswordAdmin bootstraps a server the way a fresh installation
// does: with nothing configured, so the account takes the documented default.
//
// This doubles as the check that the default password satisfies the default
// policy. Creating an account applies the policy, so raising the shipped
// minimum length past the default password's would make bootstrap fail — and
// it would fail here, in a test, rather than on somebody's first install.
func newDefaultPasswordAdmin(t *testing.T) *apiTest {
	t.Helper()
	silenceLogs(t)

	cfg := testConfig(t)
	cfg.InitialAdminPassword = ""
	return newAPITestWithConfig(t, cfg)
}

func TestTheDefaultAdministratorPasswordDoesNotSignAnybodyIn(t *testing.T) {
	api := newDefaultPasswordAdmin(t)

	res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername,
		"password":   service.DefaultInitialAdminPassword,
	})
	if res.Status != http.StatusUnauthorized || res.Code != "PASSWORD_CHANGE_REQUIRED" {
		t.Fatalf("sign-in with the default password = %d %s, want 401 PASSWORD_CHANGE_REQUIRED",
			res.Status, res.Code)
	}

	// A wrong password must still be indistinguishable from any other wrong
	// password. If the forced change leaked before the credential check, the
	// refusal itself would confirm that the account is on its default — which
	// is an invitation to try the one password everybody knows.
	res = api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername,
		"password":   "not-the-default-password",
	})
	if res.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("sign-in with a wrong password = %s, want INVALID_CREDENTIALS", res.Code)
	}
}

func TestReplacingTheDefaultPasswordIsTheWayIn(t *testing.T) {
	api := newDefaultPasswordAdmin(t)

	const chosen = "chosen-by-the-operator-1"

	res := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"identifier":      adminUsername,
		"currentPassword": service.DefaultInitialAdminPassword,
		"newPassword":     chosen,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("replacing the default password = %d %s %s", res.Status, res.Code, res.Message)
	}

	// A token, not merely a 200. The endpoint finishes by signing the caller
	// in, and that sign-in runs the same refusal this test just cleared: if
	// the flag survived the change, this is where it becomes an unbreakable
	// loop — the endpoint would report success and hand back nothing.
	var session struct {
		Token string `json:"token"`
	}
	res.into(t, &session)
	if session.Token == "" {
		t.Fatal("replacing the password returned no token; the forced change was not lifted")
	}

	// And it is a working one, for the account it should be.
	me := api.do(http.MethodGet, "/api/v1/users/me", session.Token, nil)
	if me.Status != http.StatusOK {
		t.Fatalf("the token from the replacement = %d %s", me.Status, me.Code)
	}

	// Signing in again, the ordinary way, is the assertion that this is over
	// rather than deferred to the next attempt.
	api.login(adminUsername, chosen)

	// The old password is gone with it.
	res = api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername,
		"password":   service.DefaultInitialAdminPassword,
	})
	if res.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("sign-in with the replaced default = %s, want INVALID_CREDENTIALS", res.Code)
	}
}

// An operator who chose a password is not made to change it. Anything else
// would break every unattended installation, which configures a password and
// then signs in with it — and would be pointless besides, since the secret
// they picked is not in any manual.
func TestAPasswordSomebodyChoseIsNotForcedOut(t *testing.T) {
	api := newAPITest(t) // configures adminPassword
	api.adminToken()     // fails the test if sign-in is refused
}

// Nobody else is caught by it. The column defaults to false, and no path
// other than bootstrap sets it, so an ordinary account created a minute
// later signs in normally.
func TestAnOrdinaryAccountIsNotAskedToChangeAnything(t *testing.T) {
	api := newDefaultPasswordAdmin(t)

	const chosen = "chosen-by-the-operator-1"
	res := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"identifier":      adminUsername,
		"currentPassword": service.DefaultInitialAdminPassword,
		"newPassword":     chosen,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("replacing the default password = %d %s", res.Status, res.Code)
	}

	admin := api.login(adminUsername, chosen)
	api.createUser(admin, "ordinary", "ordinary-password-1", "USER")
	api.login("ordinary", "ordinary-password-1")
}

// The replacement endpoint stays shut to everybody else. Widening it to
// accept a forced change must not have turned it into a way to change a
// password without being signed in.
func TestTheReplacementEndpointStillRefusesAPasswordThatIsFine(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodPost, "/api/v1/auth/password/expired", "", map[string]string{
		"identifier":      adminUsername,
		"currentPassword": adminPassword,
		"newPassword":     "something-else-entirely-1",
	})
	if res.Status != http.StatusBadRequest || res.Code != "PASSWORD_NOT_EXPIRED" {
		t.Fatalf("replacing a password that is fine = %d %s, want 400 PASSWORD_NOT_EXPIRED",
			res.Status, res.Code)
	}
}
