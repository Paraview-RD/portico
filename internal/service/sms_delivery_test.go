package service

// What a password reset or an address verification sent over SMS actually
// carries.
//
// Before the SMSSender interface changed to take an Aliyun template kind and
// a params map, the link lived in a rendered i18n string, and
// TestEveryMessageKeepsTheLink in internal/i18n covered it. Rendering an SMS
// body is no longer this codebase's job — Aliyun's approved template is — so
// that coverage has nowhere left to check, and it moved here to check what is
// actually sent: the right SMSKind, and the link/TTL as named params.

import (
	"context"
	"strconv"
	"testing"

	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// fakeSMSSender records the last call instead of sending anything.
type fakeSMSSender struct {
	phone  string
	kind   notify.SMSKind
	params map[string]string
}

func (f *fakeSMSSender) Send(_ context.Context, phone string, kind notify.SMSKind, params map[string]string) error {
	f.phone, f.kind, f.params = phone, kind, params
	return nil
}

// settingsWithCachedTenant is a SettingsService that answers MessageLocale
// from a pre-filled cache rather than a store, so a delivery test does not
// need a database to pick a language.
func settingsWithCachedTenant(tenantID string) *SettingsService {
	return &SettingsService{cache: map[string]Settings{tenantID: {}}}
}

func TestRecoveryOverSMSSendsTheLinkAndTTLAsTemplateParams(t *testing.T) {
	sms := &fakeSMSSender{}
	tenant := model.Tenant{ID: "t1", Code: "acme", Name: "Acme Ltd"}
	row := sqlcgen.User{Phone: "+8613800138000", DisplayName: "Sam", Username: "sam"}

	service := &RecoveryService{
		settings:  settingsWithCachedTenant(tenant.ID),
		messages:  i18n.MustLoad(),
		sms:       sms,
		publicURL: "https://portico.example",
	}

	if err := service.deliver(context.Background(), tenant, model.RecoverySMS, row, "abc123"); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if sms.phone != row.Phone {
		t.Errorf("sent to %q, want %q", sms.phone, row.Phone)
	}
	if sms.kind != notify.SMSKindRecovery {
		t.Errorf("kind = %q, want %q", sms.kind, notify.SMSKindRecovery)
	}
	wantLink := service.resetLink(tenant.Code, "abc123")
	if sms.params["Link"] != wantLink {
		t.Errorf("Link param = %q, want %q", sms.params["Link"], wantLink)
	}
	wantMinutes := strconv.Itoa(int(RecoveryTokenTTL.Minutes()))
	if sms.params["Minutes"] != wantMinutes {
		t.Errorf("Minutes param = %q, want %q", sms.params["Minutes"], wantMinutes)
	}
}

func TestVerificationOverSMSSendsTheLinkAndTTLAsTemplateParams(t *testing.T) {
	sms := &fakeSMSSender{}
	tenant := model.Tenant{ID: "t1", Code: "acme", Name: "Acme Ltd"}
	row := sqlcgen.User{Phone: "+8613800138000", DisplayName: "Sam", Username: "sam"}

	service := &VerificationService{
		settings:  settingsWithCachedTenant(tenant.ID),
		messages:  i18n.MustLoad(),
		sms:       sms,
		publicURL: "https://portico.example",
	}

	if err := service.deliver(context.Background(), tenant, model.RecoverySMS, row, "abc123"); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if sms.phone != row.Phone {
		t.Errorf("sent to %q, want %q", sms.phone, row.Phone)
	}
	if sms.kind != notify.SMSKindVerification {
		t.Errorf("kind = %q, want %q", sms.kind, notify.SMSKindVerification)
	}
	wantLink := service.verifyLink(tenant.Code, "abc123")
	if sms.params["Link"] != wantLink {
		t.Errorf("Link param = %q, want %q", sms.params["Link"], wantLink)
	}
	wantHours := strconv.Itoa(int(VerificationTokenTTL.Hours()))
	if sms.params["Hours"] != wantHours {
		t.Errorf("Hours param = %q, want %q", sms.params["Hours"], wantHours)
	}
}
