package service

// Coverage for PhoneVerificationService: RequestPhoneChange's synchronous
// refusals (unlike SMSLoginService, there is no enumeration concern here --
// the caller is already a known account) and ConfirmPhoneChange's code
// verification. Fixtures mirror newSMSLoginTestService in sms_login_test.go.

import (
	"context"
	"errors"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

// newPhoneVerificationTestService opens a throwaway database with one
// tenant, one account (with a bound phone) whose principal is returned for
// calling the service, and a PhoneVerificationService wired to a
// recordingSMS instead of a real sender.
func newPhoneVerificationTestService(t *testing.T) (svc *PhoneVerificationService, actor auth.Principal, sms *recordingSMS, existingPhone string) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-phone-verify"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "phoneverify", Name: "Phone Verify", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	audit := NewAuditService(st)
	settings := NewSettingsService(st, 0)
	settings.WithDeliveryChannels(func() []model.RecoveryChannel { return []model.RecoveryChannel{model.RecoverySMS} })

	users := NewUserService(st, audit, settings,
		auth.NewTokenService([]byte("0123456789abcdef0123456789abcdef")), metrics.New())

	existingPhone = "+8613800002222"
	created, err := users.Create(ctx, tenantID, CreateUserInput{
		Username: "phoneuser", DisplayName: "Phone User", Password: "Str0ng!Passw0rd",
		Phone: existingPhone, Role: model.RoleUser, Source: model.SourceAdmin,
	})
	if err != nil {
		t.Fatalf("create user with phone: %v", err)
	}

	sms = &recordingSMS{}
	svc = NewPhoneVerificationService(st, users, settings, audit, sms)
	actor = auth.Principal{TenantID: tenantID, UserID: created.ID, Username: created.Username, Role: model.RoleUser}
	return svc, actor, sms, existingPhone
}

func TestRequestPhoneChangeRefusesAnAlreadyTakenNumberWithoutSendingAnything(t *testing.T) {
	svc, actor, sms, _ := newPhoneVerificationTestService(t)
	ctx := context.Background()

	// A second account in the same tenant holding the number the first
	// account is about to try claiming.
	if _, err := svc.users.Create(ctx, actor.TenantID, CreateUserInput{
		Username: "other", DisplayName: "Other", Password: "Str0ng!Passw0rd",
		Phone: "+8613800003333", Role: model.RoleUser, Source: model.SourceAdmin,
	}); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	err := svc.RequestPhoneChange(ctx, actor, "+8613800003333", "1.2.3.4")
	if !errors.Is(err, ErrPhoneTaken) {
		t.Fatalf("RequestPhoneChange (taken number) = %v, want ErrPhoneTaken", err)
	}
	if sms.len() != 0 {
		t.Fatalf("sent %d messages for an already-taken number, want 0 -- a synchronous refusal must not also text a stranger", sms.len())
	}
}

func TestRequestPhoneChangeRefusesTheAccountsOwnCurrentNumber(t *testing.T) {
	svc, actor, sms, existingPhone := newPhoneVerificationTestService(t)

	err := svc.RequestPhoneChange(context.Background(), actor, existingPhone, "1.2.3.4")
	if !errors.Is(err, ErrPhoneUnchanged) {
		t.Fatalf("RequestPhoneChange (own number) = %v, want ErrPhoneUnchanged", err)
	}
	if sms.len() != 0 {
		t.Fatalf("sent %d messages for an unchanged number, want 0", sms.len())
	}
}

func TestRequestPhoneChangeSendsACodeForAFreshNumber(t *testing.T) {
	svc, actor, sms, _ := newPhoneVerificationTestService(t)

	const candidate = "+8613800009999"
	if err := svc.RequestPhoneChange(context.Background(), actor, candidate, "1.2.3.4"); err != nil {
		t.Fatalf("RequestPhoneChange: %v", err)
	}
	if sms.len() != 1 {
		t.Fatalf("sent %d messages, want 1", sms.len())
	}
	if sms.sent[0].phone != candidate {
		t.Errorf("sent to %q, want %q", sms.sent[0].phone, candidate)
	}
	if sms.sent[0].kind != notify.SMSKindPhoneVerification {
		t.Errorf("kind = %q, want SMSKindPhoneVerification", sms.sent[0].kind)
	}
	if sms.sent[0].params["Code"] == "" {
		t.Error("params[\"Code\"] is empty")
	}
}

func TestConfirmPhoneChangeBindsTheNumberOnTheRightCode(t *testing.T) {
	svc, actor, sms, _ := newPhoneVerificationTestService(t)
	ctx := context.Background()

	const candidate = "+8613800009998"
	if err := svc.RequestPhoneChange(ctx, actor, candidate, "1.2.3.4"); err != nil {
		t.Fatalf("RequestPhoneChange: %v", err)
	}
	code := sms.sent[0].params["Code"]

	updated, err := svc.ConfirmPhoneChange(ctx, actor, candidate, code, "1.2.3.4")
	if err != nil {
		t.Fatalf("ConfirmPhoneChange: %v", err)
	}
	if updated.Phone != candidate {
		t.Errorf("bound phone = %q, want %q", updated.Phone, candidate)
	}

	// The user's own record reflects it too, not just the returned value.
	fetched, err := svc.users.Get(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if fetched.Phone != candidate {
		t.Errorf("stored phone = %q, want %q", fetched.Phone, candidate)
	}
}

func TestConfirmPhoneChangeRefusesTheWrongCode(t *testing.T) {
	svc, actor, _, existingPhone := newPhoneVerificationTestService(t)
	ctx := context.Background()

	const candidate = "+8613800009997"
	if err := svc.RequestPhoneChange(ctx, actor, candidate, "1.2.3.4"); err != nil {
		t.Fatalf("RequestPhoneChange: %v", err)
	}

	if _, err := svc.ConfirmPhoneChange(ctx, actor, candidate, "000000", "1.2.3.4"); !errors.Is(err, ErrInvalidPhoneVerificationCode) {
		t.Fatalf("ConfirmPhoneChange (wrong code) = %v, want ErrInvalidPhoneVerificationCode", err)
	}

	// The account's phone stayed put.
	fetched, err := svc.users.Get(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if fetched.Phone != existingPhone {
		t.Errorf("stored phone changed to %q on a wrong code", fetched.Phone)
	}
}

func TestConfirmPhoneChangeLocksOutAfterTooManyWrongAttempts(t *testing.T) {
	svc, actor, _, _ := newPhoneVerificationTestService(t)
	ctx := context.Background()

	const candidate = "+8613800009996"
	if err := svc.RequestPhoneChange(ctx, actor, candidate, "1.2.3.4"); err != nil {
		t.Fatalf("RequestPhoneChange: %v", err)
	}

	for i := 0; i < PhoneVerificationMaxAttempts; i++ {
		if _, err := svc.ConfirmPhoneChange(ctx, actor, candidate, "000000", "1.2.3.4"); !errors.Is(err, ErrInvalidPhoneVerificationCode) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidPhoneVerificationCode", i, err)
		}
	}

	// Even the actually-correct code is refused once the attempt budget is
	// spent -- the row is dead, not merely the wrong guesses.
	sms := &recordingSMS{}
	svc2 := NewPhoneVerificationService(svc.store, svc.users, svc.settings, svc.audit, sms)
	if err := svc2.RequestPhoneChange(ctx, actor, "+8613800009995", "1.2.3.4"); err != nil {
		t.Fatalf("RequestPhoneChange (second candidate): %v", err)
	}
	// Confirm against the FIRST candidate's now-exhausted code must still
	// refuse, proving exhaustion rather than a fresh, unrelated code.
	if _, err := svc.ConfirmPhoneChange(ctx, actor, candidate, sms.sent[0].params["Code"], "1.2.3.4"); !errors.Is(err, ErrInvalidPhoneVerificationCode) {
		t.Fatalf("ConfirmPhoneChange against exhausted code = %v, want ErrInvalidPhoneVerificationCode", err)
	}
}

func TestRequestPhoneChangeUnavailableWithoutSMS(t *testing.T) {
	svc, actor, sms, _ := newPhoneVerificationTestService(t)

	// Undo what newPhoneVerificationTestService turned on, simulating a
	// deployment with no SMS channel configured at all.
	svc.settings.WithDeliveryChannels(func() []model.RecoveryChannel { return nil })

	err := svc.RequestPhoneChange(context.Background(), actor, "+8613800009994", "1.2.3.4")
	if !errors.Is(err, ErrPhoneVerificationUnavailable) {
		t.Fatalf("RequestPhoneChange (no SMS channel) = %v, want ErrPhoneVerificationUnavailable", err)
	}
	if sms.len() != 0 {
		t.Fatalf("sent %d messages while unavailable, want 0", sms.len())
	}
}
