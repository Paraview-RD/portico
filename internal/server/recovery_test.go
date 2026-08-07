package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/notify"
	"github.com/paraview/portico/internal/server"
	"github.com/paraview/portico/internal/testdb"
)

// recordingMailer captures what would have been sent, which is the only way
// to assert the property that matters here: a reset token goes to the
// account's own bound address and to nothing else.
type recordingMailer struct {
	mu       sync.Mutex
	messages []notify.Message
}

func (m *recordingMailer) Send(_ context.Context, msg notify.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *recordingMailer) sent() []notify.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]notify.Message(nil), m.messages...)
}

// last returns the most recent message, failing if none was sent.
func (m *recordingMailer) last(t *testing.T) notify.Message {
	t.Helper()
	sent := m.sent()
	if len(sent) == 0 {
		t.Fatal("no message was sent")
	}
	return sent[len(sent)-1]
}

// tokenFrom pulls the reset token out of the link in a message body.
func tokenFrom(t *testing.T, msg notify.Message) string {
	t.Helper()
	for _, field := range strings.Fields(msg.Body) {
		if !strings.Contains(field, "/reset-password?") {
			continue
		}
		parsed, err := url.Parse(field)
		if err != nil {
			t.Fatalf("parse reset link %q: %v", field, err)
		}
		token := parsed.Query().Get("token")
		if token == "" {
			t.Fatalf("reset link carries no token: %s", field)
		}
		return token
	}
	t.Fatalf("no reset link in message body:\n%s", msg.Body)
	return ""
}

// newRecoveryTest builds an instance whose mail goes to a recorder.
func newRecoveryTest(t *testing.T) (*apiTest, *recordingMailer) {
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

	mailer := &recordingMailer{}
	srv, err := server.New(cfg, server.WithMailer(mailer))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	return &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}, mailer
}

// requestRecovery asks for a reset link and asserts the neutral response.
func (a *apiTest) requestRecovery(channel, destination string) response {
	a.t.Helper()
	return a.do(http.MethodPost, "/api/v1/auth/password-recovery", "", map[string]string{
		"channel": channel, "destination": destination,
	})
}

func TestPasswordRecoveryEndToEnd(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	const oldPassword = "recovery-old-pass-1"
	const newPassword = "recovery-new-pass-2"

	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "frank", "displayName": "Frank", "password": oldPassword,
		"email": "frank@example.com",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create user: %d %s", res.Status, res.Message)
	}

	if res := api.requestRecovery("EMAIL", "frank@example.com"); res.Status != http.StatusOK {
		t.Fatalf("request recovery: %d %s %s", res.Status, res.Code, res.Message)
	}

	msg := mailer.last(t)
	if msg.To != "frank@example.com" {
		t.Fatalf("message went to %q, want the account's bound address", msg.To)
	}
	if !strings.Contains(msg.Body, "https://portico.example.com/reset-password?") {
		t.Errorf("link does not use the configured public URL:\n%s", msg.Body)
	}

	token := tokenFrom(t, msg)

	confirm := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": newPassword,
	})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s %s", confirm.Status, confirm.Code, confirm.Message)
	}

	// The new password works and the old one does not.
	api.loginTo("", "frank", newPassword)

	old := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "frank", "password": oldPassword,
	})
	if old.Status != http.StatusUnauthorized {
		t.Errorf("the old password still works: %d", old.Status)
	}
}

// Completing a recovery has to end every session the account had. If the
// reason for recovering was that someone else knew the password, leaving
// their session alive defeats the exercise.
func TestCompletingRecoveryRevokesLiveSessions(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "grace", "displayName": "Grace", "password": "grace-old-pass-1",
		"email": "grace@example.com",
	})

	stolen := api.loginTo("", "grace", "grace-old-pass-1")
	if res := api.do(http.MethodGet, "/api/v1/users/me", stolen, nil); res.Status != http.StatusOK {
		t.Fatalf("the session should work before recovery: %d", res.Status)
	}

	api.requestRecovery("EMAIL", "grace@example.com")
	confirm := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": tokenFrom(t, mailer.last(t)), "newPassword": "grace-new-pass-2",
	})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s", confirm.Status, confirm.Message)
	}

	res := api.do(http.MethodGet, "/api/v1/users/me", stolen, nil)
	if res.Status != http.StatusUnauthorized {
		t.Errorf("a session issued before the recovery survived it: %d", res.Status)
	}
}

func TestResetTokenIsSingleUse(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "heidi", "displayName": "Heidi", "password": "heidi-old-pass-1",
		"email": "heidi@example.com",
	})

	api.requestRecovery("EMAIL", "heidi@example.com")
	token := tokenFrom(t, mailer.last(t))

	first := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": "heidi-new-pass-2",
	})
	if first.Status != http.StatusOK {
		t.Fatalf("first use: %d %s", first.Status, first.Message)
	}

	second := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": "attacker-chosen-3",
	})
	if second.Code != "INVALID_RESET_TOKEN" {
		t.Errorf("code = %q, want INVALID_RESET_TOKEN — the token was reusable", second.Code)
	}

	// And the password the first use set is still in force.
	api.loginTo("", "heidi", "heidi-new-pass-2")
}

// Asking again has to invalidate the earlier link. Otherwise every message
// a user ever received stays live until it expires, and someone who read an
// old one can still use it.
func TestRequestingAgainInvalidatesTheEarlierLink(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "ivan", "displayName": "Ivan", "password": "ivan-old-pass-1",
		"email": "ivan@example.com",
	})

	api.requestRecovery("EMAIL", "ivan@example.com")
	first := tokenFrom(t, mailer.last(t))

	api.requestRecovery("EMAIL", "ivan@example.com")
	second := tokenFrom(t, mailer.last(t))

	if first == second {
		t.Fatal("the second request reissued the same token")
	}

	stale := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": first, "newPassword": "should-not-work-3",
	})
	if stale.Code != "INVALID_RESET_TOKEN" {
		t.Errorf("code = %q, want INVALID_RESET_TOKEN — the superseded link still worked", stale.Code)
	}

	fresh := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": second, "newPassword": "ivan-new-pass-2",
	})
	if fresh.Status != http.StatusOK {
		t.Errorf("the newest link did not work: %d %s", fresh.Status, fresh.Message)
	}
}

// The response must not say whether an account exists — that is the whole
// reason this endpoint answers the way it does. All three misses have to be
// indistinguishable from a success.
func TestRecoveryResponseRevealsNothing(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "judy", "displayName": "Judy", "password": "judy-pass-1",
		"email": "judy@example.com",
	})
	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "no.email", "displayName": "No Email", "password": "no-email-pass-1",
	})

	cases := []struct {
		name        string
		destination string
		wantSent    bool
	}{
		{"a bound address", "judy@example.com", true},
		{"an address nobody has", "nobody@example.com", false},
		{"an account with nothing bound", "no.email", false},
	}

	var bodies []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(mailer.sent())
			res := api.requestRecovery("EMAIL", tc.destination)

			if res.Status != http.StatusOK {
				t.Fatalf("status = %d (%s), want 200", res.Status, res.Code)
			}
			bodies = append(bodies, string(res.Data))

			if sentNow := len(mailer.sent()) > before; sentNow != tc.wantSent {
				t.Errorf("message sent = %v, want %v", sentNow, tc.wantSent)
			}
		})
	}

	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Errorf("responses differ between cases:\n  %s\n  %s", bodies[0], bodies[i])
		}
	}
}

// The account is resolved against the channel's own column, never the
// union sign-in uses. If it used the union, an account whose username equals
// another's email address would have its reset token mailed to whoever typed
// that address.
func TestRecoveryDoesNotResolveAcrossColumns(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	// A username that looks exactly like an email address.
	victim := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "target@example.com", "displayName": "Victim",
		"password": "victim-pass-1", "email": "victim-real@example.com",
	})
	if victim.Status != http.StatusOK {
		t.Fatalf("create victim: %d %s", victim.Status, victim.Message)
	}

	// Somebody else holds that string as their email address.
	attacker := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "attacker", "displayName": "Attacker",
		"password": "attacker-pass-1", "email": "target@example.com",
	})
	if attacker.Status != http.StatusOK {
		t.Fatalf("create attacker: %d %s", attacker.Status, attacker.Message)
	}

	// Sign-in resolves the string to the username holder — that is the
	// declared precedence, and it is safe because the password is the gate.
	api.loginTo("", "target@example.com", "victim-pass-1")

	// Recovery must resolve it the other way: to the account that has it as
	// an email address, and deliver there.
	if res := api.requestRecovery("EMAIL", "target@example.com"); res.Status != http.StatusOK {
		t.Fatalf("request recovery: %d %s", res.Status, res.Message)
	}

	msg := mailer.last(t)
	if msg.To != "target@example.com" {
		t.Fatalf("message went to %q; a reset for one account must not be "+
			"delivered on another account's identifier", msg.To)
	}

	// And redeeming it changes the attacker's own password, not the
	// victim's.
	confirm := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": tokenFrom(t, msg), "newPassword": "attacker-new-pass-2",
	})
	if confirm.Status != http.StatusOK {
		t.Fatalf("confirm: %d %s", confirm.Status, confirm.Message)
	}

	api.loginTo("", "attacker", "attacker-new-pass-2")
	// The victim is untouched.
	api.loginTo("", "target@example.com", "victim-pass-1")
}

// A reset link carries its tenant, so redeeming it is a tenant-scoped
// lookup. A token from one tenant must not be redeemable in another.
func TestResetTokensDoNotCrossTenants(t *testing.T) {
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

	mailer := &recordingMailer{}
	srv, err := server.New(cfg, server.WithMailer(mailer))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := srv.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	api := &apiTest{t: t, srv: srv, dsn: cfg.DatabaseDSN}

	admin := api.adminToken()
	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "kelly", "displayName": "Kelly", "password": "kelly-pass-1",
		"email": "kelly@example.com",
	})

	api.requestRecovery("EMAIL", "kelly@example.com")
	token := tokenFrom(t, mailer.last(t))

	// The link says which tenant it belongs to; claiming a different one
	// must not redeem it.
	res := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"tenant": "no-such-tenant", "token": token, "newPassword": "elsewhere-pass-2",
	})
	if res.Code != "TENANT_NOT_FOUND" {
		t.Errorf("code = %q, want TENANT_NOT_FOUND", res.Code)
	}

	// And the original still works in its own tenant.
	ok := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": "kelly-new-pass-2",
	})
	if ok.Status != http.StatusOK {
		t.Errorf("the token did not work in its own tenant: %d %s", ok.Status, ok.Message)
	}
}

// A deployment with no mail relay says so, rather than accepting a request
// it cannot fulfil and leaving someone waiting for a message.
func TestRecoveryReportsAnUnconfiguredChannel(t *testing.T) {
	api := newAPITest(t)

	for _, channel := range []string{"EMAIL", "SMS"} {
		res := api.requestRecovery(channel, "somebody@example.com")
		if res.Status != http.StatusServiceUnavailable || res.Code != "RECOVERY_UNAVAILABLE" {
			t.Errorf("%s: %d %s, want 503 RECOVERY_UNAVAILABLE", channel, res.Status, res.Code)
		}
	}

	res := api.do(http.MethodGet, "/api/v1/auth/recovery-channels", "", nil)
	var payload struct {
		Channels []string `json:"channels"`
	}
	res.into(t, &payload)
	if len(payload.Channels) != 0 {
		t.Errorf("channels = %v, want none on a deployment with no providers", payload.Channels)
	}
}

func TestRecoveryChannelsListsWhatIsConfigured(t *testing.T) {
	api, _ := newRecoveryTest(t)

	res := api.do(http.MethodGet, "/api/v1/auth/recovery-channels", "", nil)
	var payload struct {
		Channels []string `json:"channels"`
	}
	res.into(t, &payload)

	if len(payload.Channels) != 1 || payload.Channels[0] != "EMAIL" {
		t.Errorf("channels = %v, want [EMAIL]", payload.Channels)
	}
}

func TestRecoveryRejectsAWeakNewPassword(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "leo", "displayName": "Leo", "password": "leo-old-pass-1",
		"email": "leo@example.com",
	})
	api.requestRecovery("EMAIL", "leo@example.com")
	token := tokenFrom(t, mailer.last(t))

	res := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": "short",
	})
	if res.Code != "WEAK_PASSWORD" {
		t.Fatalf("code = %q, want WEAK_PASSWORD", res.Code)
	}

	// The rejected attempt must not have spent the token — otherwise a typo
	// costs the user another round trip through their mailbox.
	ok := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
		"token": token, "newPassword": "leo-new-pass-2",
	})
	if ok.Status != http.StatusOK {
		t.Errorf("a rejected password spent the token: %d %s", ok.Status, ok.Message)
	}
}

func TestRecoveryRejectsAGarbageToken(t *testing.T) {
	api, _ := newRecoveryTest(t)

	for _, token := range []string{"", "not-a-token", strings.Repeat("a", 43)} {
		res := api.do(http.MethodPost, "/api/v1/auth/password-recovery/confirm", "", map[string]string{
			"token": token, "newPassword": "whatever-pass-1",
		})
		if res.Code != "INVALID_RESET_TOKEN" {
			t.Errorf("token %q: code = %q, want INVALID_RESET_TOKEN", token, res.Code)
		}
	}
}
