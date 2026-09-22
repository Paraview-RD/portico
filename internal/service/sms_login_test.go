package service

// Coverage for SMSLoginService, in particular the anti-enumeration split
// between RequestCode (phone-blind) and completeRequest (phone-scoped, runs
// after the response). The fixtures below mirror newUserTestFixture in
// user_test.go -- the closest existing pattern in this package for a real
// store + tenant + UserService -- extended with a SettingsService that has
// SMS login turned on (mirroring settings_test.go's WithDeliveryChannels use)
// and a recordingSMS in place of the real notify.SMSSender.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// recordingSMS captures every Send call, for asserting what a code request
// actually sent -- and how many times. Guarded by a mutex because
// SMSLoginService.completeRequest sends from a goroutine while the test
// polls from the main one (same reasoning as recordingMailer in
// internal/server/recovery_test.go).
type recordingSMS struct {
	mu   sync.Mutex
	sent []struct {
		phone  string
		kind   notify.SMSKind
		params map[string]string
	}
}

func (r *recordingSMS) Send(_ context.Context, phone string, kind notify.SMSKind, params map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, struct {
		phone  string
		kind   notify.SMSKind
		params map[string]string
	}{phone, kind, params})
	return nil
}

func (r *recordingSMS) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// waitForSMS blocks until at least count messages have been recorded, the
// same shape as recordingMailer.waitFor in internal/server/recovery_test.go:
// delivery happens after RequestCode's response, so a test cannot read the
// mailbox the instant the call returns.
func waitForSMS(t *testing.T, sms *recordingSMS, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if sms.len() >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d SMS message(s); only %d were sent", count, sms.len())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// newSMSLoginTestService opens a throwaway database with one tenant, one
// user with a bound phone number, SMS login turned on for that tenant, and
// an SMSLoginService wired to a recordingSMS instead of a real sender.
func newSMSLoginTestService(t *testing.T) (svc *SMSLoginService, tenant model.Tenant, sms *recordingSMS, existingPhone string) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-sms-login"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "smslogin", Name: "SMS Login", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	tenant = model.Tenant{ID: tenantID, Code: "smslogin", Name: "SMS Login", Status: model.StatusActive}

	audit := NewAuditService(st)
	settings := NewSettingsService(st, 0)
	settings.WithDeliveryChannels(func() []model.RecoveryChannel { return []model.RecoveryChannel{model.RecoverySMS} })

	current, err := settings.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	current.SMSLoginEnabled = true
	if _, err := settings.Update(ctx, tenantID, current); err != nil {
		t.Fatalf("enable sms login: %v", err)
	}

	users := NewUserService(st, audit, settings,
		auth.NewTokenService([]byte("0123456789abcdef0123456789abcdef")), metrics.New())

	existingPhone = "+8613800001111"
	if _, err := users.Create(ctx, tenantID, CreateUserInput{
		Username: "smsuser", DisplayName: "SMS User", Password: "Str0ng!Passw0rd",
		Phone: existingPhone, Role: model.RoleUser, Source: model.SourceAdmin,
	}); err != nil {
		t.Fatalf("create user with phone: %v", err)
	}

	sms = &recordingSMS{}
	svc = NewSMSLoginService(st, users, settings, audit, metrics.New(), sms, 0)
	return svc, tenant, sms, existingPhone
}

// newSMSLoginTestSecondTenant adds a second tenant sharing svc's store, with
// its own account bound to the same phone number newSMSLoginTestService used
// -- for proving the cooldown is keyed on the phone alone, not phone+tenant.
func newSMSLoginTestSecondTenant(t *testing.T, svc *SMSLoginService) model.Tenant {
	t.Helper()
	ctx := context.Background()

	now := store.Now()
	tenantID := "tenant-sms-login-2"
	if err := svc.store.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "smslogin2", Name: "SMS Login 2", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create second tenant: %v", err)
	}

	current, err := svc.settings.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("get settings for second tenant: %v", err)
	}
	current.SMSLoginEnabled = true
	if _, err := svc.settings.Update(ctx, tenantID, current); err != nil {
		t.Fatalf("enable sms login for second tenant: %v", err)
	}

	if _, err := svc.users.Create(ctx, tenantID, CreateUserInput{
		Username: "smsuser2", DisplayName: "SMS User 2", Password: "Str0ng!Passw0rd",
		Phone: "+8613800001111", Role: model.RoleUser, Source: model.SourceAdmin,
	}); err != nil {
		t.Fatalf("create second tenant's user with phone: %v", err)
	}

	return model.Tenant{ID: tenantID, Code: "smslogin2", Name: "SMS Login 2", Status: model.StatusActive}
}

func TestSMSLoginRequestCodeIsSilentWhetherOrNotThePhoneExists(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode (existing phone): %v", err)
	}
	if err := svc.RequestCode(context.Background(), tenant, "+8619999999999", "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode (unknown phone): %v", err)
	}

	waitForSMS(t, sms, 1)
	time.Sleep(300 * time.Millisecond) // give a wrongly-sent second message a chance to land
	if got := sms.len(); got != 1 {
		t.Fatalf("sent %d messages, want exactly 1 (only the existing phone)", got)
	}
	if sms.sent[0].phone != existingPhone {
		t.Errorf("sent to %q, want %q", sms.sent[0].phone, existingPhone)
	}
	if sms.sent[0].kind != notify.SMSKindLoginCode {
		t.Errorf("kind = %q, want SMSKindLoginCode", sms.sent[0].kind)
	}
}

func TestSMSLoginVerifyRefusesWithOneGenericErrorWhateverTheReason(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	waitForSMS(t, sms, 1)
	code := sms.sent[0].params["Code"]

	cases := []struct {
		name  string
		phone string
		code  string
	}{
		{"wrong code", existingPhone, "000000"},
		{"unknown phone", "+8619999999999", code},
	}
	for _, c := range cases {
		if c.code == code && c.phone == existingPhone {
			t.Fatalf("test bug: %q accidentally uses the real code", c.name)
		}
		_, err := svc.LoginWithCode(context.Background(), tenant, c.phone, c.code, "1.2.3.4", "test-agent")
		if !errors.Is(err, ErrInvalidSMSCode) {
			t.Errorf("%s: err = %v, want ErrInvalidSMSCode", c.name, err)
		}
	}

	// The real code still works afterwards -- the wrong attempts above must
	// not have consumed or blocked it.
	session, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent")
	if err != nil {
		t.Fatalf("LoginWithCode with the real code: %v", err)
	}
	if session.Token == "" {
		t.Error("want a session token")
	}

	// And it is single-use: asking again with the same code fails now that
	// it is consumed.
	_, err = svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent")
	if !errors.Is(err, ErrInvalidSMSCode) {
		t.Errorf("reusing a consumed code: err = %v, want ErrInvalidSMSCode", err)
	}
}

func TestSMSLoginLocksOutAfterFiveWrongAttempts(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)
	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	waitForSMS(t, sms, 1)
	code := sms.sent[0].params["Code"]

	for i := 0; i < 5; i++ {
		if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, "000000", "1.2.3.4", "test-agent"); !errors.Is(err, ErrInvalidSMSCode) {
			t.Fatalf("wrong attempt %d: err = %v", i, err)
		}
	}
	// The 6th attempt uses the *real* code, but the code should already be
	// dead from too many wrong guesses.
	if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent"); !errors.Is(err, ErrInvalidSMSCode) {
		t.Errorf("real code after 5 wrong attempts: err = %v, want ErrInvalidSMSCode (code should be dead)", err)
	}
}

func TestSMSLoginCooldownIsPerPhoneAcrossTenants(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)
	otherTenant := newSMSLoginTestSecondTenant(t, svc)

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("first tenant's request: %v", err)
	}
	waitForSMS(t, sms, 1)

	// Same phone number, a different tenant's account, immediately after:
	// must not get a second message out of the shared cooldown.
	if err := svc.RequestCode(context.Background(), otherTenant, existingPhone, "5.6.7.8"); err != nil {
		t.Fatalf("second tenant's request: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // give any (incorrect) async send a chance to land
	if got := sms.len(); got != 1 {
		t.Errorf("sent %d messages, want still 1 -- cooldown must be shared across tenants for the same phone", got)
	}
}

// wantTooManyAttempts asserts err is an httpx.Error carrying the
// TOO_MANY_ATTEMPTS code -- the synchronous refusal RequestCode gives for
// the two checks that are safe to refuse distinctly (deployment/day,
// IP/day; see RequestCode's doc comment).
func wantTooManyAttempts(t *testing.T, err error) {
	t.Helper()
	var herr *httpx.Error
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v (%T), want an *httpx.Error", err, err)
	}
	if herr.Code != "TOO_MANY_ATTEMPTS" {
		t.Errorf("err code = %q, want TOO_MANY_ATTEMPTS", herr.Code)
	}
}

// TestSMSLoginPerIPPerDayCapRefusesAfterLimit proves SMSLoginPerIPPerDay is
// the actual threshold wired to RequestCode's synchronous IP check -- not
// just that *a* limiter exists, but that it is configured with the
// documented constant. Each call uses a fresh, never-before-seen phone
// number so the per-phone cooldown/cap (checked asynchronously, and much
// stricter -- SMSLoginCooldown is one per minute) cannot itself explain a
// refusal; only the shared IP is held constant.
func TestSMSLoginPerIPPerDayCapRefusesAfterLimit(t *testing.T) {
	svc, tenant, _, _ := newSMSLoginTestService(t)
	const ip = "203.0.113.9"

	for i := 0; i < SMSLoginPerIPPerDay; i++ {
		phone := fmt.Sprintf("+86138%08d", i)
		if err := svc.RequestCode(context.Background(), tenant, phone, ip); err != nil {
			t.Fatalf("request %d (within the per-IP limit): %v", i, err)
		}
	}

	err := svc.RequestCode(context.Background(), tenant, "+8613800099999", ip)
	wantTooManyAttempts(t, err)
}

// TestSMSLoginPerDeploymentPerDayCapRefusesAfterLimit proves
// SMSLoginPerDeploymentPerDay is wired to RequestCode's synchronous
// deployment-wide check. The default (1000) is too large to exercise
// directly in a fast test, so this builds a second SMSLoginService sharing
// the first one's store/users/settings/sms but constructed with a small
// perDeploymentPerDay via NewSMSLoginService's own configuration knob --
// exactly the mechanism a real deployment would use to tune it, which is
// what makes this a wiring test rather than a reimplementation of
// DayLimiter's own tests.
func TestSMSLoginPerDeploymentPerDayCapRefusesAfterLimit(t *testing.T) {
	svc, tenant, sms, _ := newSMSLoginTestService(t)
	const deploymentCap = 3
	capped := NewSMSLoginService(svc.store, svc.users, svc.settings, svc.audit, metrics.New(), sms, deploymentCap)

	for i := 0; i < deploymentCap; i++ {
		phone := fmt.Sprintf("+86139%08d", i)
		ip := fmt.Sprintf("198.51.100.%d", i+1)
		if err := capped.RequestCode(context.Background(), tenant, phone, ip); err != nil {
			t.Fatalf("request %d (within the deployment cap): %v", i, err)
		}
	}

	err := capped.RequestCode(context.Background(), tenant, "+8613900099999", "198.51.100.99")
	wantTooManyAttempts(t, err)
}

// TestSMSLoginPerPhonePerDayCapSilentlyStopsAfterLimit proves
// SMSLoginPerPhonePerDay is the threshold wired to the phone-scoped limiter
// completeRequest checks. Unlike the IP and deployment caps, this one must
// NOT surface as a synchronous refusal -- doing so would tell a caller "this
// phone has received five codes today," which discloses the phone is bound
// to an account (RequestCode's doc comment). So this test cannot drive the
// limit through repeated RequestCode calls to the same phone either: the
// much stricter per-phone cooldown (one per minute) would refuse every call
// after the first, long before the daily cap of five is ever reached, and
// waiting out the cooldown five times over is not a reasonable thing for a
// fast test to do. Instead it exhausts the exact *httpx.DayLimiter instance
// RequestCode's completeRequest checks (svc.perPhonePerDay, an unexported
// field reachable because this test lives in the same package), which
// proves the count and the key without needing the cooldown out of the way
// first -- and then confirms a subsequent RequestCode for that phone still
// answers success (anti-enumeration) while silently delivering nothing.
func TestSMSLoginPerPhonePerDayCapSilentlyStopsAfterLimit(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)

	for i := 0; i < SMSLoginPerPhonePerDay; i++ {
		if !svc.perPhonePerDay.Allow(existingPhone) {
			t.Fatalf("priming call %d unexpectedly refused -- SMSLoginPerPhonePerDay may not be %d", i, SMSLoginPerPhonePerDay)
		}
	}

	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "192.0.2.77"); err != nil {
		t.Fatalf("RequestCode after exhausting the per-phone cap: %v (want nil -- must stay silent)", err)
	}
	time.Sleep(300 * time.Millisecond) // give a wrongly-sent message a chance to land
	if got := sms.len(); got != 0 {
		t.Errorf("sent %d messages, want 0 -- the per-phone daily cap should have silently stopped delivery", got)
	}
}

// countAuditFailures reads the raw audit_logs table through the store's
// *sql.DB handle rather than a typed Scoped query -- there is no
// ListAuditLogs to read the trail back with, so this hand-writes the SQL,
// the same test-only pattern the tenancy guard's own test file documents
// for tables the generated-query layer has no read path for.
func countAuditFailures(t *testing.T, svc *SMSLoginService, tenantID, actorUsername string) int {
	t.Helper()
	var count int
	row := svc.store.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE tenant_id = $1 AND action = $2 AND actor_username = $3`,
		tenantID, model.ActionLoginFailure, actorUsername)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}
	return count
}

// TestSMSLoginAuditsFailedAttempts proves each of LoginWithCode's three
// logLoginFailure call sites actually reaches the audit trail -- not just
// that *a* call happens somewhere in the function, but that all three
// distinct failure branches are individually covered. An earlier version
// of this test only exercised the "wrong code" branch; deleting either of
// the other two logLoginFailure calls still left the whole suite green,
// which is exactly the false-confidence a bug-fix test is supposed to
// prevent. The three cases below correspond 1:1 to LoginWithCode's three
// failure returns:
//
//  1. "wrong code": a live code exists for the phone, but the submitted
//     code does not match it.
//  2. "no live code": GetLiveSMSLoginCode finds nothing at all for the
//     phone -- covered here by a phone that never had RequestCode called
//     for it, so no row was ever created.
//  3. "code correct but no matching account": the rarer data-integrity
//     case from the brief -- a live sms_login_codes row exists and its
//     hash matches the submitted code, but GetUserByPhone finds no user.
//     Built directly with CreateSMSLoginCode for a phone with no bound
//     account, using a plaintext code this test controls (mirroring what
//     completeRequest itself does, minus the user-creation step).
//
// AuditService.Record (called synchronously by logLoginFailure ->
// audit.Log, no goroutine involved) has committed by the time
// LoginWithCode returns, so no polling is needed for any of the three.
func TestSMSLoginAuditsFailedAttempts(t *testing.T) {
	svc, tenant, sms, existingPhone := newSMSLoginTestService(t)

	// Case 1 setup: a live code for existingPhone, then submit the wrong one.
	if err := svc.RequestCode(context.Background(), tenant, existingPhone, "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	waitForSMS(t, sms, 1)

	// Case 3 setup: a live code for a phone with no bound account at all.
	orphanPhone := "+8613800004444"
	orphanCode := "654321"
	if err := svc.store.ForTenant(tenant.ID).CreateSMSLoginCode(context.Background(), sqlcgen.CreateSMSLoginCodeParams{
		ID: uuid.NewString(), Phone: orphanPhone, CodeHash: hashSMSLoginCode(orphanCode),
		ExpiresAt: store.Now().Add(SMSLoginCodeTTL), CreatedAt: store.Now(), Ip: "9.9.9.9",
	}); err != nil {
		t.Fatalf("seed orphan sms login code: %v", err)
	}

	cases := []struct {
		name  string
		phone string
		code  string
	}{
		{"wrong code", existingPhone, "000000"},
		{"no live code", "+8613800005555", "000000"}, // RequestCode never called for this phone
		{"code correct but no matching account", orphanPhone, orphanCode},
	}
	for _, c := range cases {
		_, err := svc.LoginWithCode(context.Background(), tenant, c.phone, c.code, "1.2.3.4", "test-agent")
		if !errors.Is(err, ErrInvalidSMSCode) {
			t.Fatalf("%s: LoginWithCode err = %v, want ErrInvalidSMSCode", c.name, err)
		}
		if got := countAuditFailures(t, svc, tenant.ID, c.phone); got == 0 {
			t.Errorf("%s: want at least one audit_logs row, found none", c.name)
		}
	}
}
