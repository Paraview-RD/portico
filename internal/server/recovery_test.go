package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/server"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/testdb"
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

// last waits for the next message and returns it.
//
// Delivery happens after the response so that a hit and a miss are
// indistinguishable to whoever asked, which means a test cannot read the
// mailbox the instant the call returns. Polling for the arrival is the
// honest way to express "and then a message shows up"; a fixed sleep would
// either be flaky or slow.
func (m *recordingMailer) last(t *testing.T) notify.Message {
	t.Helper()
	return m.waitFor(t, len(m.sent())+1)
}

// waitFor blocks until at least count messages have been recorded.
func (m *recordingMailer) waitFor(t *testing.T, count int) notify.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sent := m.sent()
		if len(sent) >= count {
			return sent[count-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for message %d; only %d were sent", count, len(sent))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// quiet asserts that no further message arrives, which is what a miss must
// look like. It has to wait, since a message that was going to be sent would
// arrive shortly after the response rather than before it.
func (m *recordingMailer) quiet(t *testing.T, was int) {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	if now := len(m.sent()); now != was {
		t.Errorf("a message was sent when none should have been (%d -> %d)", was, now)
	}
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

			if tc.wantSent {
				mailer.waitFor(t, before+1)
			} else {
				mailer.quiet(t, before)
			}
		})
	}

	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Errorf("responses differ between cases:\n  %s\n  %s", bodies[0], bodies[i])
		}
	}
}

// slowMailer takes a noticeable time to send, standing in for a real relay.
type slowMailer struct {
	recordingMailer
	delay time.Duration
}

func (m *slowMailer) Send(ctx context.Context, msg notify.Message) error {
	time.Sleep(m.delay)
	return m.recordingMailer.Send(ctx, msg)
}

// Identical response bodies are not enough. A hit writes two rows and dials
// an SMTP server; a miss does neither. If that work happened before the
// response, the difference would be seconds — measurable from anywhere, and
// a far cheaper oracle than the one the wording is careful to avoid.
//
// So everything past the account lookup is detached, and this is the test
// that says so. It fails if the delivery ever moves back onto the request
// path.
func TestRecoveryAnswersInTheSameTimeWhetherOrNotItMatched(t *testing.T) {
	silenceLogs(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = testdb.DSN(t)
	cfg.InitialAdminUsername = adminUsername
	cfg.InitialAdminPassword = adminPassword

	const sendDelay = 500 * time.Millisecond
	mailer := &slowMailer{delay: sendDelay}

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
		"username": "timed", "displayName": "Timed", "password": "timing-pass-1",
		"email": "timed@example.com",
	})

	measure := func(destination string) time.Duration {
		start := time.Now()
		res := api.requestRecovery("EMAIL", destination)
		elapsed := time.Since(start)
		if res.Status != http.StatusOK {
			t.Fatalf("%s: status = %d", destination, res.Status)
		}
		return elapsed
	}

	// Warm the connection pool so the first call does not carry setup cost.
	measure("warmup@example.com")

	hit := measure("timed@example.com")
	miss := measure("nobody@example.com")

	// Generous, because a loaded CI machine is noisy. The point is that the
	// gap is nothing like the delay a synchronous send would add — with the
	// work back on the request path this is 500ms against ~1ms.
	const tolerance = sendDelay / 2
	if difference := hit - miss; difference > tolerance || -difference > tolerance {
		t.Errorf("hit took %v and miss took %v; a gap of %v tells a caller "+
			"whether the address is registered", hit, miss, difference)
	}

	// And the message really was sent, so the test is not passing because
	// nothing happened at all.
	mailer.waitFor(t, 1)
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

// How much mail one account can be made to receive in a day.
//
// The gap this closes was an asymmetry against a decision already made next
// door. The two self-service trial endpoints were given their own budget and
// their own per-mailbox count on the reasoning — written into routes.go —
// that an endpoint whose job is to send mail to a stranger is not the same
// kind of thing as signing in. Recovery is that same kind of thing, and had
// only the per-address rate limiter that sign-in has: sixty a minute, from
// one address, with nothing counting the mailbox at all.
//
// What that costs is not this deployment's inbox. It is the sending quota
// and the sender reputation, both of which are spent by every message and
// shared by every tenant — and when reputation goes, password recovery stops
// working for the tenants that already exist. The person being flooded is a
// real account holder, since the destination is always the address bound to
// the account and never the one submitted.
func TestRecoveryStopsMailingOneAccountAfterTheDailyCap(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "mira", "displayName": "Mira", "password": "mira-old-pass-1",
		"email": "mira@example.com",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create user: %d %s", res.Status, res.Message)
	}

	// Up to the cap, every request produces a message.
	for i := range service.RecoveryPerAccountPerDay {
		if res := api.requestRecovery("EMAIL", "mira@example.com"); res.Status != http.StatusOK {
			t.Fatalf("request %d: %d %s", i+1, res.Status, res.Message)
		}
		mailer.waitFor(t, i+1)
	}

	// The next one is answered exactly as the others were — a refusal the
	// caller could see would say "this address has an account here", which is
	// the disclosure the whole endpoint is built to avoid.
	sent := len(mailer.sent())
	if res := api.requestRecovery("EMAIL", "mira@example.com"); res.Status != http.StatusOK {
		t.Fatalf("the request past the cap was refused visibly: %d %s %s",
			res.Status, res.Code, res.Message)
	}
	mailer.quiet(t, sent)
}

// An unknown address costs nothing and is not counted.
//
// The count is keyed on the account the lookup resolved, which is the only
// key available on a path that must not record misses. A table of attempted
// destinations would answer "has anybody ever asked about this address",
// which is the enumeration oracle this endpoint refuses to be — so a miss
// leaves nothing behind, and cannot exhaust anybody's allowance either.
func TestRecoveryDoesNotCountRequestsForAnUnknownAddress(t *testing.T) {
	api, mailer := newRecoveryTest(t)
	admin := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "nils", "displayName": "Nils", "password": "nils-old-pass-1",
		"email": "nils@example.com",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create user: %d %s", res.Status, res.Message)
	}

	for range service.RecoveryPerAccountPerDay * 2 {
		if res := api.requestRecovery("EMAIL", "nobody@example.com"); res.Status != http.StatusOK {
			t.Fatalf("request for an unknown address: %d %s", res.Status, res.Message)
		}
	}
	mailer.quiet(t, 0)

	// The account that does exist still has its whole allowance.
	if res := api.requestRecovery("EMAIL", "nils@example.com"); res.Status != http.StatusOK {
		t.Fatalf("request: %d %s", res.Status, res.Message)
	}
	mailer.waitFor(t, 1)
}
