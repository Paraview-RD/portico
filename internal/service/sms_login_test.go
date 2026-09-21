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
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
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
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: err = %v, want ErrInvalidCredentials", c.name, err)
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
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("reusing a consumed code: err = %v, want ErrInvalidCredentials", err)
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
		if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, "000000", "1.2.3.4", "test-agent"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("wrong attempt %d: err = %v", i, err)
		}
	}
	// The 6th attempt uses the *real* code, but the code should already be
	// dead from too many wrong guesses.
	if _, err := svc.LoginWithCode(context.Background(), tenant, existingPhone, code, "1.2.3.4", "test-agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("real code after 5 wrong attempts: err = %v, want ErrInvalidCredentials (code should be dead)", err)
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
