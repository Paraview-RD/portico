package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// The operator console, which is the only part of this API that sees past the
// caller's own tenant.
//
// Everything in tenancy_test.go asserts that one tenant cannot reach another.
// This feature is the single deliberate exception, so the tests here are
// mostly about the shape of the exception rather than the feature: who is let
// in, what is refused, and — the one that matters most — that what crosses is
// how many rather than who.

func newConsoleTest(t *testing.T, on bool) *apiTest {
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
	cfg.AuthRateLimit, cfg.AuthRateLimitBurst = 100000, 100000
	cfg.TenantConsole = on

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	return &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}
}

// tenantOverview is the shape the console reads.
type tenantOverview struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Users         int64  `json:"users"`
	ActiveUsers   int64  `json:"activeUsers"`
	Organizations int64  `json:"organizations"`
	Applications  int64  `json:"applications"`
}

func TestTheOperatorConsoleDoesNotExistUnlessTheDeploymentAsksForIt(t *testing.T) {
	// The outer gate, and the one that matters for every deployment that did
	// not ask for this: not a refusal, an absence. Before this feature there
	// was no API that could name another tenant, and on a deployment that
	// leaves the flag alone there still is not.
	api := newConsoleTest(t, false)
	token := api.login(adminUsername, adminPassword)

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodPut, "/api/v1/tenants/default/status"},
	} {
		got := api.do(call.method, call.path, token, map[string]string{
			"status": "DISABLED", "confirm": "default",
		})
		if got.Status != http.StatusNotFound {
			t.Errorf("%s %s answered %d %s, want 404 — with the console off "+
				"these addresses must not exist at all",
				call.method, call.path, got.Status, got.Code)
		}
	}

	// And the console is told, so it draws no menu entry it cannot use.
	var me struct {
		MayManageTenants bool `json:"mayManageTenants"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", token, nil).into(t, &me)
	if me.MayManageTenants {
		t.Error("an ordinary deployment says its administrator may manage tenants")
	}
}

func TestTheConsoleCountsWhatIsInEachTenantWithoutNamingAnyOfIt(t *testing.T) {
	api := newConsoleTest(t, true)
	token := api.login(adminUsername, adminPassword)

	// A second tenant with something in it, written directly: what is under
	// test is the counting, and going through the provisioning CLI would put
	// its behaviour in the middle of this assertion.
	api.execSQL(t, `INSERT INTO tenants (id, code, name, status, created_at, updated_at)
		VALUES ('t-other', 'other', 'Other Ltd', 'ACTIVE', now(), now())`)
	api.execSQL(t, `INSERT INTO organizations (id, tenant_id, name, code, status, created_at, updated_at)
		VALUES ('o-other', 't-other', 'Somewhere', 'somewhere', 'ACTIVE', now(), now())`)
	api.execSQL(t, `INSERT INTO users (id, tenant_id, username, password_hash, display_name, role, status, created_at, updated_at)
		VALUES
		  ('u-other-1', 't-other', 'ada', 'x', 'Ada Lovelace', 'USER', 'ACTIVE', now(), now()),
		  ('u-other-2', 't-other', 'grace', 'x', 'Grace Hopper', 'USER', 'DISABLED', now(), now())`)

	res := api.do(http.MethodGet, "/api/v1/tenants", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("answered %d %s %s", res.Status, res.Code, res.Message)
	}

	var tenants []tenantOverview
	res.into(t, &tenants)

	byCode := map[string]tenantOverview{}
	for _, tenant := range tenants {
		byCode[tenant.Code] = tenant
	}
	other, found := byCode["other"]
	if !found {
		t.Fatalf("the second tenant is missing from %v", byCode)
	}
	if other.Users != 2 || other.ActiveUsers != 1 {
		t.Errorf("counted %d users and %d active, want 2 and 1",
			other.Users, other.ActiveUsers)
	}
	if other.Organizations != 1 {
		t.Errorf("counted %d organizations, want 1", other.Organizations)
	}
	if other.Name != "Other Ltd" {
		t.Errorf("the tenant is named %q", other.Name)
	}
	// The counts belong to their own tenant. A missing predicate would show
	// every tenant the same totals, which is the failure this whole feature
	// is one mistake away from.
	if def, ok := byCode["default"]; !ok {
		t.Error("the default tenant is missing from its own console")
	} else if def.Users == other.Users {
		t.Errorf("both tenants report %d users; the counts are not scoped", def.Users)
	}

	// The property the design rests on: how many, never who. Asserted against
	// the raw body rather than the decoded struct, because a struct only sees
	// the fields it declares — the question here is what was *sent*.
	body := string(res.Data)
	for _, leaked := range []string{"ada", "grace", "Ada Lovelace", "Grace Hopper", "Somewhere"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the overview carries %q, which belongs to another tenant's "+
				"people. This endpoint may report sizes and nothing else.", leaked)
		}
	}
}

func TestAnotherTenantsAdministratorIsToldNothingAtAll(t *testing.T) {
	// 404 rather than 403. A refusal that says "you are the wrong person"
	// confirms both that the console exists and that somebody else has it,
	// which is a fact about the deployment that an administrator of a tenant
	// on it has no business learning from a status code.
	api := newConsoleTest(t, true)

	api.execSQL(t, `INSERT INTO tenants (id, code, name, status, created_at, updated_at)
		VALUES ('t-guest', 'guest', 'Guest Ltd', 'ACTIVE', now(), now())`)
	// Their administrator, created through the API of their own tenant so the
	// password is hashed the way sign-in expects.
	admin := api.login(adminUsername, adminPassword)
	created := api.do(http.MethodPost, "/api/v1/users", admin, map[string]any{
		"username": "guest-admin", "password": "Guest@12345",
		"displayName": "Guest Admin", "role": "SUPER_ADMIN",
	})
	if created.Status != http.StatusOK {
		t.Fatalf("create the other tenant's administrator: %d %s", created.Status, created.Code)
	}
	var user struct {
		ID string `json:"id"`
	}
	created.into(t, &user)
	// Moved across, which is the only way to get an account into a tenant
	// whose administrator does not exist yet.
	api.execSQL(t, `UPDATE users SET tenant_id = 't-guest' WHERE id = $1`, user.ID)

	token := api.loginTo("guest", "guest-admin", "Guest@12345")
	got := api.do(http.MethodGet, "/api/v1/tenants", token, nil)
	if got.Status != http.StatusNotFound {
		t.Errorf("answered %d %s, want 404", got.Status, got.Code)
	}

	var me struct {
		MayManageTenants bool `json:"mayManageTenants"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", token, nil).into(t, &me)
	if me.MayManageTenants {
		t.Error("another tenant's administrator is told they may manage tenants")
	}
}

func TestSwitchingATenantOffTakesTwoDeliberateActs(t *testing.T) {
	api := newConsoleTest(t, true)
	token := api.login(adminUsername, adminPassword)

	api.execSQL(t, `INSERT INTO tenants (id, code, name, status, created_at, updated_at)
		VALUES ('t-off', 'switchme', 'Switch Me', 'ACTIVE', now(), now())`)

	// The confirmation is the tenant's own code, typed. Without it, disabling
	// the wrong row is one mis-click — and every account in that tenant is
	// signed out immediately, by somebody they have never heard of.
	wrong := api.do(http.MethodPut, "/api/v1/tenants/switchme/status", token,
		map[string]string{"status": "DISABLED", "confirm": "switchmee"})
	if wrong.Status != http.StatusBadRequest || wrong.Code != "TENANT_CONFIRM_MISMATCH" {
		t.Errorf("a mistyped confirmation answered %d %s, want 400 TENANT_CONFIRM_MISMATCH",
			wrong.Status, wrong.Code)
	}

	ok := api.do(http.MethodPut, "/api/v1/tenants/switchme/status", token,
		map[string]string{"status": "DISABLED", "confirm": "switchme"})
	if ok.Status != http.StatusOK {
		t.Fatalf("answered %d %s %s", ok.Status, ok.Code, ok.Message)
	}

	// It took effect where it counts: that tenant's sign-in.
	refused := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"tenant": "switchme", "identifier": "anybody", "password": "anything",
	})
	if refused.Code != "TENANT_DISABLED" {
		t.Errorf("sign-in to the disabled tenant answered %s, want TENANT_DISABLED",
			refused.Code)
	}

	// And it is in the operator's own audit trail, under their name. The
	// affected tenant's log would be a record its administrators could not
	// reach — they are the ones who were just switched off.
	logs := api.do(http.MethodGet, "/api/v1/audit-logs", token, nil)
	if logs.Status != http.StatusOK {
		t.Fatalf("read the audit trail: %d %s", logs.Status, logs.Code)
	}
	if !strings.Contains(string(logs.Data), "switchme") {
		t.Errorf("disabling a tenant left no audit entry naming it:\n%s", logs.Data)
	}

	// Reversible, which is the whole reason this is a status rather than a
	// deletion.
	back := api.do(http.MethodPut, "/api/v1/tenants/switchme/status", token,
		map[string]string{"status": "ACTIVE", "confirm": "switchme"})
	if back.Status != http.StatusOK {
		t.Fatalf("re-enabling answered %d %s", back.Status, back.Code)
	}
}

func TestTheConsoleCannotSwitchOffTheTenantItIsServedFrom(t *testing.T) {
	// There would be no way back. Disabling the default tenant refuses the
	// next sign-in of the person who just did it, and no screen anywhere can
	// undo it — the way back is the command line, on the machine, which is
	// not where somebody who clicked a button in a browser is.
	api := newConsoleTest(t, true)
	token := api.login(adminUsername, adminPassword)

	got := api.do(http.MethodPut, "/api/v1/tenants/default/status", token,
		map[string]string{"status": "DISABLED", "confirm": "default"})
	if got.Status != http.StatusUnprocessableEntity || got.Code != "TENANT_CANNOT_DISABLE_DEFAULT" {
		t.Errorf("answered %d %s, want 422 TENANT_CANNOT_DISABLE_DEFAULT",
			got.Status, got.Code)
	}

	// Still working, which is the point of refusing.
	if again := api.login(adminUsername, adminPassword); again == "" {
		t.Error("the default tenant was disabled anyway")
	}
}

func TestAnOrdinaryUserOfTheDefaultTenantIsRefused(t *testing.T) {
	api := newConsoleTest(t, true)
	admin := api.login(adminUsername, adminPassword)

	created := api.do(http.MethodPost, "/api/v1/users", admin, map[string]any{
		"username": "ordinary", "password": "Ordinary@12345",
		"displayName": "Ordinary Person", "role": "USER",
	})
	if created.Status != http.StatusOK {
		t.Fatalf("create the ordinary account: %d %s", created.Status, created.Code)
	}

	token := api.login("ordinary", "Ordinary@12345")
	got := api.do(http.MethodGet, "/api/v1/tenants", token, nil)
	if got.Status == http.StatusOK {
		t.Fatal("an ordinary user read the tenant list")
	}

	var me struct {
		MayManageTenants bool `json:"mayManageTenants"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", token, nil).into(t, &me)
	if me.MayManageTenants {
		t.Error("an ordinary user is told they may manage tenants")
	}
}

func TestTheDefaultAdministratorIsTold(t *testing.T) {
	api := newConsoleTest(t, true)
	token := api.login(adminUsername, adminPassword)

	var me struct {
		MayManageTenants bool `json:"mayManageTenants"`
	}
	body := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	body.into(t, &me)
	if !me.MayManageTenants {
		t.Fatalf("the default tenant's administrator is not offered the console:\n%s",
			body.Data)
	}

	// The field is a boolean and not an object: the console decides what to
	// draw from it, and anything richer here would be a second place where
	// what the console may do is decided.
	var raw map[string]json.RawMessage
	body.into(t, &raw)
	if string(raw["mayManageTenants"]) != "true" {
		t.Errorf("mayManageTenants is %s", raw["mayManageTenants"])
	}
}

// A tenant nobody has touched still appears, with zeros and no last activity.
func TestATenantNobodyEverUsedIsStillListed(t *testing.T) {
	api := newConsoleTest(t, true)
	token := api.login(adminUsername, adminPassword)

	api.execSQL(t, `INSERT INTO tenants (id, code, name, status, created_at, updated_at)
		VALUES ('t-empty', 'empty', 'Empty', 'ACTIVE', now(), now())`)

	var tenants []struct {
		tenantOverview
		LastActivity *string `json:"lastActivity"`
	}
	api.do(http.MethodGet, "/api/v1/tenants", token, nil).into(t, &tenants)

	for _, tenant := range tenants {
		if tenant.Code != "empty" {
			continue
		}
		if tenant.Users != 0 || tenant.Organizations != 0 || tenant.Applications != 0 {
			t.Errorf("an empty tenant reports %+v", tenant)
		}
		if tenant.LastActivity != nil {
			t.Errorf("a tenant nothing has happened in reports activity at %s",
				*tenant.LastActivity)
		}
		return
	}
	t.Error("a tenant with nothing in it was left out of the list, which is " +
		"the row an operator is most often looking for")
}

// Somebody signed in can find out which tenant they are in.
//
// After sign-in the tenant is in the token and nowhere else: the address bar
// does not carry it, and the server ignores a tenant named in a header on an
// authenticated request — TestAuthenticatedRequestsIgnoreTenantHeader holds
// that shut, and it is the rule that keeps one tenant's administrator out of
// another's data.
//
// The cost of that rule is that the console had nothing to display. It did
// not matter while a deployment had one tenant. It does now: a person can own
// several, the sign-in form pre-fills the last tenant code used, and signing
// out does not clear it — so signing back in can land somewhere other than
// where they think, with nothing on screen to disagree.
func TestYourOwnProfileSaysWhichTenantYouAreIn(t *testing.T) {
	api := newConsoleTest(t, false)
	token := api.login(adminUsername, adminPassword)

	var me struct {
		TenantCode string `json:"tenantCode"`
		TenantName string `json:"tenantName"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", token, nil).into(t, &me)

	if me.TenantCode != "default" {
		t.Errorf("tenantCode is %q, want the tenant this session belongs to", me.TenantCode)
	}
	if me.TenantName == "" {
		t.Error("tenantName is empty; the console has nothing to draw")
	}

	// And it is the caller's own tenant rather than whichever one they name.
	// The same request with another tenant's code in the header must answer
	// the same way — this is the header rule, asked of the field that would
	// be the most convenient place to break it.
	api.execSQL(t, `INSERT INTO tenants (id, code, name, status, created_at, updated_at)
		VALUES ('t-elsewhere', 'elsewhere', 'Elsewhere', 'ACTIVE', now(), now())`)

	var claimed struct {
		TenantCode string `json:"tenantCode"`
	}
	api.doWithHeaders(http.MethodGet, "/api/v1/users/me", token, nil,
		map[string]string{"X-Portico-Tenant": "elsewhere"}).into(t, &claimed)

	if claimed.TenantCode != "default" {
		t.Errorf("naming a tenant in a header changed the answer to %q; this "+
			"field must report the session's tenant, not the caller's claim",
			claimed.TenantCode)
	}
}
