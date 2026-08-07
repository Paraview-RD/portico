package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/server"
	"github.com/paraview/portico/internal/testdb"
)

// apiTest drives the whole stack — router, middleware, services, database —
// the way a real client would.
type apiTest struct {
	t   *testing.T
	srv *server.Server
}

const (
	adminUsername = "admin"
	adminPassword = "admin-password-123"
)

func newAPITest(t *testing.T) *apiTest {
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

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	return &apiTest{t: t, srv: srv}
}

// silenceLogs suppresses the server's structured logging for the duration of
// a test, so a failure message is not buried in access-log lines.
func silenceLogs(t *testing.T) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
}

// response is a decoded envelope with the raw data left for the caller.
type response struct {
	Status  int
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// into unmarshals the data payload, failing the test if it does not fit.
func (r response) into(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.Data, dst); err != nil {
		t.Fatalf("decode data: %v (data=%s)", err, r.Data)
	}
}

func (a *apiTest) do(method, path, token string, body any) response {
	a.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	a.srv.Handler().ServeHTTP(rec, req)

	var out response
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		a.t.Fatalf("%s %s returned a non-envelope body: %s", method, path, rec.Body.String())
	}
	out.Status = rec.Code
	return out
}

// login returns a token for the given credentials, failing if login fails.
func (a *apiTest) login(username, password string) string {
	a.t.Helper()

	res := a.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if res.Status != http.StatusOK {
		a.t.Fatalf("login as %s failed: %d %s %s", username, res.Status, res.Code, res.Message)
	}

	var session struct {
		Token string `json:"token"`
	}
	res.into(a.t, &session)
	if session.Token == "" {
		a.t.Fatal("login returned an empty token")
	}
	return session.Token
}

// adminToken signs in as the bootstrap administrator.
func (a *apiTest) adminToken() string {
	return a.login(adminUsername, adminPassword)
}

// createUser adds an account as the administrator and returns its id.
func (a *apiTest) createUser(token, username, password string, role string) string {
	a.t.Helper()

	res := a.do(http.MethodPost, "/api/v1/users", token, map[string]string{
		"username":    username,
		"displayName": username,
		"password":    password,
		"role":        role,
	})
	if res.Status != http.StatusOK {
		a.t.Fatalf("create user %s failed: %d %s %s", username, res.Status, res.Code, res.Message)
	}

	var user struct {
		ID string `json:"id"`
	}
	res.into(a.t, &user)
	return user.ID
}

func TestBootstrapCreatesAdministrator(t *testing.T) {
	api := newAPITest(t)

	token := api.adminToken()

	res := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Status)
	}

	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	res.into(t, &me)

	if me.Username != adminUsername {
		t.Errorf("username = %q, want %q", me.Username, adminUsername)
	}
	if me.Role != "SUPER_ADMIN" {
		t.Errorf("role = %q, want SUPER_ADMIN", me.Role)
	}
}

// Bootstrap must not create a second administrator on an existing database.
func TestBootstrapIsIdempotent(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	if err := api.srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}

	res := api.do(http.MethodGet, "/api/v1/users", token, nil)
	var page struct {
		Total int64 `json:"total"`
	}
	res.into(t, &page)

	if page.Total != 1 {
		t.Errorf("user count = %d, want 1", page.Total)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	api := newAPITest(t)

	tests := []struct {
		name     string
		username string
		password string
		wantCode string
	}{
		{"wrong password", adminUsername, "not-the-password", "INVALID_CREDENTIALS"},
		// An unknown user must produce exactly the same code as a wrong
		// password, so the API cannot be used to enumerate accounts.
		{"unknown user", "nobody", "whatever-password", "INVALID_CREDENTIALS"},
		{"empty username", "", "whatever-password", "MISSING_CREDENTIALS"},
		{"empty password", adminUsername, "", "MISSING_CREDENTIALS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
				"username": tt.username,
				"password": tt.password,
			})

			if res.Status == http.StatusOK {
				t.Fatal("login succeeded, want a failure")
			}
			if res.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", res.Code, tt.wantCode)
			}
		})
	}
}

func TestAdminOnlyRoutesRejectNormalUsers(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	api.createUser(adminToken, "regular", "regular-password-1", "USER")
	userToken := api.login("regular", "regular-password-1")

	adminOnly := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/users", nil},
		{http.MethodPost, "/api/v1/users", map[string]string{
			"username": "sneaky", "displayName": "S", "password": "password-123",
		}},
		{http.MethodGet, "/api/v1/audit-logs", nil},
		{http.MethodGet, "/api/v1/settings", nil},
		{http.MethodPost, "/api/v1/organizations", map[string]string{
			"name": "Sneaky", "code": "SNEAK",
		}},
	}

	for _, route := range adminOnly {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			res := api.do(route.method, route.path, userToken, route.body)

			if res.Status != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (code=%s)", res.Status, res.Code)
			}
			if res.Code != "ADMIN_REQUIRED" {
				t.Errorf("code = %q, want ADMIN_REQUIRED", res.Code)
			}
		})
	}
}

func TestProtectedRoutesRejectAnonymousCallers(t *testing.T) {
	api := newAPITest(t)

	for _, path := range []string{
		"/api/v1/users/me",
		"/api/v1/users",
		"/api/v1/settings",
		"/api/v1/audit-logs",
		"/api/v1/organizations",
	} {
		t.Run(path, func(t *testing.T) {
			res := api.do(http.MethodGet, path, "", nil)

			if res.Status != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", res.Status)
			}
		})
	}
}

// Logging out must invalidate the token that was used, immediately.
func TestLogoutRevokesTheToken(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	if res := api.do(http.MethodGet, "/api/v1/users/me", token, nil); res.Status != http.StatusOK {
		t.Fatalf("token should work before logout: %d", res.Status)
	}

	if res := api.do(http.MethodPost, "/api/v1/auth/logout", token, nil); res.Status != http.StatusOK {
		t.Fatalf("logout failed: %d %s", res.Status, res.Code)
	}

	res := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 after logout", res.Status)
	}
	if res.Code != "TOKEN_REVOKED" {
		t.Errorf("code = %q, want TOKEN_REVOKED", res.Code)
	}
}

// Disabling an account must cut off its live session, not wait for expiry.
func TestDisablingUserRevokesTheirSession(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	userID := api.createUser(adminToken, "victim", "victim-password-1", "USER")
	userToken := api.login("victim", "victim-password-1")

	if res := api.do(http.MethodGet, "/api/v1/users/me", userToken, nil); res.Status != http.StatusOK {
		t.Fatalf("token should work before disable: %d", res.Status)
	}

	res := api.do(http.MethodPost, "/api/v1/users/"+userID+"/disable", adminToken, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("disable failed: %d %s", res.Status, res.Code)
	}

	res = api.do(http.MethodGet, "/api/v1/users/me", userToken, nil)
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 after the account was disabled", res.Status)
	}

	// And they must not be able to sign in again.
	res = api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "victim", "password": "victim-password-1",
	})
	if res.Code != "ACCOUNT_DISABLED" {
		t.Errorf("login code = %q, want ACCOUNT_DISABLED", res.Code)
	}
}

func TestChangingPasswordRevokesExistingSessions(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	api.createUser(adminToken, "changer", "old-password-123", "USER")
	userToken := api.login("changer", "old-password-123")

	res := api.do(http.MethodPost, "/api/v1/users/me/password", userToken, map[string]string{
		"currentPassword": "old-password-123",
		"newPassword":     "new-password-456",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("change password failed: %d %s %s", res.Status, res.Code, res.Message)
	}

	// The old token is dead...
	if res := api.do(http.MethodGet, "/api/v1/users/me", userToken, nil); res.Status != http.StatusUnauthorized {
		t.Errorf("old token still works after a password change: %d", res.Status)
	}
	// ...and the new password works.
	api.login("changer", "new-password-456")
}

func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	api := newAPITest(t)
	adminToken := api.adminToken()

	api.createUser(adminToken, "careful", "old-password-123", "USER")
	userToken := api.login("careful", "old-password-123")

	res := api.do(http.MethodPost, "/api/v1/users/me/password", userToken, map[string]string{
		"currentPassword": "wrong-password",
		"newPassword":     "new-password-456",
	})

	if res.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", res.Status)
	}
	if res.Code != "CURRENT_PASSWORD_MISMATCH" {
		t.Errorf("code = %q, want CURRENT_PASSWORD_MISMATCH", res.Code)
	}

	// The old password must still work, i.e. nothing was changed.
	api.login("careful", "old-password-123")
}

func TestDuplicateUsernameIsRejected(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	api.createUser(token, "duplicate", "password-12345", "USER")

	res := api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
		"username": "duplicate", "displayName": "Another", "password": "password-12345",
	})

	// 409, not 400: the request is well-formed, it conflicts with state.
	if res.Status != http.StatusConflict {
		t.Errorf("status = %d, want 409", res.Status)
	}
	if res.Code != "USERNAME_TAKEN" {
		t.Errorf("code = %q, want USERNAME_TAKEN", res.Code)
	}
}

// The system must never be left without an administrator.
func TestLastAdminCannotBeDisabledOrDemoted(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	var me struct {
		ID string `json:"id"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", token, nil).into(t, &me)

	t.Run("cannot disable self", func(t *testing.T) {
		res := api.do(http.MethodPost, "/api/v1/users/"+me.ID+"/disable", token, nil)
		if res.Status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", res.Status)
		}
		if res.Code != "CANNOT_DISABLE_SELF" {
			t.Errorf("code = %q, want CANNOT_DISABLE_SELF", res.Code)
		}
	})

	t.Run("cannot demote the only admin", func(t *testing.T) {
		res := api.do(http.MethodPut, "/api/v1/users/"+me.ID, token, map[string]string{
			"displayName": "Administrator",
			"role":        "USER",
		})
		if res.Status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", res.Status)
		}
		if res.Code != "LAST_ADMIN" {
			t.Errorf("code = %q, want LAST_ADMIN", res.Code)
		}
	})
}

func TestRegistrationRespectsTheToggle(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	signUp := map[string]string{
		"username": "newcomer", "displayName": "Newcomer", "password": "password-12345",
	}

	// Registration defaults to closed, so an exposed instance does not
	// accept sign-ups before anyone configures it.
	res := api.do(http.MethodPost, "/api/v1/auth/register", "", signUp)
	if res.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 while registration is closed", res.Status)
	}
	if res.Code != "REGISTRATION_DISABLED" {
		t.Errorf("code = %q, want REGISTRATION_DISABLED", res.Code)
	}

	// Open it.
	res = api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"tokenTtlMinutes": 120, "registrationEnabled": true, "systemName": "Portico",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update settings failed: %d %s %s", res.Status, res.Code, res.Message)
	}

	res = api.do(http.MethodPost, "/api/v1/auth/register", "", signUp)
	if res.Status != http.StatusOK {
		t.Fatalf("registration failed once enabled: %d %s %s", res.Status, res.Code, res.Message)
	}

	// A self-registered account is always a normal user, never an admin.
	var created struct {
		Role   string `json:"role"`
		Source string `json:"source"`
	}
	res.into(t, &created)
	if created.Role != "USER" {
		t.Errorf("role = %q, want USER", created.Role)
	}
	if created.Source != "REGISTRATION" {
		t.Errorf("source = %q, want REGISTRATION", created.Source)
	}
}

// A sign-up must not be able to grant itself the administrator role by
// including it in the request body.
func TestRegistrationCannotSelfPromote(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"tokenTtlMinutes": 120, "registrationEnabled": true, "systemName": "Portico",
	})

	res := api.do(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "climber", "displayName": "Climber",
		"password": "password-12345", "role": "SUPER_ADMIN",
	})

	// The role field is not part of the registration payload, so a request
	// carrying it is rejected outright rather than silently ignored.
	if res.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown field", res.Status)
	}
}

func TestOrganizationLifecycle(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Engineering", "code": "ENG", "remark": "builds things", "sortOrder": 1,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create organization failed: %d %s %s", res.Status, res.Code, res.Message)
	}
	var org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	res.into(t, &org)

	t.Run("duplicate code is rejected", func(t *testing.T) {
		res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
			"name": "Engineering Again", "code": "ENG",
		})
		if res.Status != http.StatusConflict {
			t.Errorf("status = %d, want 409", res.Status)
		}
		if res.Code != "ORGANIZATION_CODE_TAKEN" {
			t.Errorf("code = %q, want ORGANIZATION_CODE_TAKEN", res.Code)
		}
	})

	t.Run("user can be assigned", func(t *testing.T) {
		res := api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
			"username": "member", "displayName": "Member",
			"password": "password-12345", "organizationId": org.ID,
		})
		if res.Status != http.StatusOK {
			t.Fatalf("create user in organization failed: %d %s", res.Status, res.Code)
		}
		var user struct {
			OrganizationID   string `json:"organizationId"`
			OrganizationName string `json:"organizationName"`
		}
		res.into(t, &user)

		if user.OrganizationID != org.ID {
			t.Errorf("organizationId = %q, want %q", user.OrganizationID, org.ID)
		}
		// The name is resolved server-side so clients never show a bare id.
		if user.OrganizationName != "Engineering" {
			t.Errorf("organizationName = %q, want Engineering", user.OrganizationName)
		}
	})

	t.Run("disabled organization takes no new members", func(t *testing.T) {
		res := api.do(http.MethodPost, "/api/v1/organizations/"+org.ID+"/disable", token, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("disable failed: %d %s", res.Status, res.Code)
		}

		res = api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
			"username": "latecomer", "displayName": "Latecomer",
			"password": "password-12345", "organizationId": org.ID,
		})
		if res.Status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", res.Status)
		}
		if res.Code != "ORGANIZATION_DISABLED" {
			t.Errorf("code = %q, want ORGANIZATION_DISABLED", res.Code)
		}
	})
}

// Every meaningful action has to land in the audit trail (§3.9).
func TestAuditTrailRecordsKeyEvents(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Produce a failed login, which must be recorded as well as successes.
	api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": adminUsername, "password": "wrong",
	})
	api.createUser(token, "audited", "password-12345", "USER")

	res := api.do(http.MethodGet, "/api/v1/audit-logs?pageSize=100", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("list audit logs failed: %d %s", res.Status, res.Code)
	}

	var page struct {
		Items []struct {
			Kind   string `json:"kind"`
			Action string `json:"action"`
			Result string `json:"result"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	res.into(t, &page)

	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.Action] = true
	}

	for _, want := range []string{"LOGIN_SUCCESS", "LOGIN_FAILURE", "USER_CREATE"} {
		if !seen[want] {
			t.Errorf("audit trail is missing a %s entry (saw %v)", want, seen)
		}
	}
}

func TestAuditLogFilters(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()
	api.createUser(token, "filtered", "password-12345", "USER")

	t.Run("by kind", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/audit-logs?kind=LOGIN", token, nil)
		var page struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		}
		res.into(t, &page)

		if len(page.Items) == 0 {
			t.Fatal("no LOGIN entries returned")
		}
		for _, item := range page.Items {
			if item.Kind != "LOGIN" {
				t.Errorf("got a %s entry in a LOGIN-filtered result", item.Kind)
			}
		}
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/audit-logs?kind=BOGUS", token, nil)
		if res.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.Status)
		}
	})
}

func TestUserSearchAndFilters(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	api.createUser(token, "alice.smith", "password-12345", "USER")
	api.createUser(token, "bob.jones", "password-12345", "USER")

	t.Run("keyword matches username", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/users?keyword=alice", token, nil)
		var page struct {
			Items []struct {
				Username string `json:"username"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		res.into(t, &page)

		if page.Total != 1 {
			t.Fatalf("total = %d, want 1", page.Total)
		}
		if page.Items[0].Username != "alice.smith" {
			t.Errorf("username = %q, want alice.smith", page.Items[0].Username)
		}
	})

	// A keyword of "%" must be treated as a literal, not a wildcard that
	// matches everything.
	t.Run("wildcards in the keyword are escaped", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/users?keyword=%25", token, nil)
		var page struct {
			Total int64 `json:"total"`
		}
		res.into(t, &page)

		if page.Total != 0 {
			t.Errorf("total = %d, want 0; a literal %% should match nothing", page.Total)
		}
	})

	t.Run("filter by role", func(t *testing.T) {
		res := api.do(http.MethodGet, "/api/v1/users?role=SUPER_ADMIN", token, nil)
		var page struct {
			Total int64 `json:"total"`
		}
		res.into(t, &page)

		if page.Total != 1 {
			t.Errorf("total = %d, want 1 administrator", page.Total)
		}
	})
}

func TestWeakPasswordIsRejected(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
		"username": "weak", "displayName": "Weak", "password": "short",
	})

	if res.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.Status)
	}
	if res.Code != "WEAK_PASSWORD" {
		t.Errorf("code = %q, want WEAK_PASSWORD", res.Code)
	}
}
