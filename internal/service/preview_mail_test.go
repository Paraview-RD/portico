package service

import (
	"os"
	"testing"
)

// A way to look at the mail rather than assert about it. Skipped unless
// asked: PORTICO_MAIL_PREVIEW=text|html go test ./internal/service -run Preview
func TestPreviewTrialMail(t *testing.T) {
	form := os.Getenv("PORTICO_MAIL_PREVIEW")
	if form == "" {
		t.Skip("set PORTICO_MAIL_PREVIEW=text or html")
	}
	locale := os.Getenv("PORTICO_MAIL_PREVIEW_LOCALE")
	if locale == "" {
		locale = "en-US"
	}
	msg, err := trialMailer(locale).readyMail(TrialTenant{
		TenantCode: "mytrial", TenantName: "My Trial", AdminUsername: "admin",
		AdminPassword: "P0T5zLoMbaVwccd8XKb8H7my",
		SignInURL:     "https://demo.example.com/login?tenant=mytrial",
		DemoPassword:  "j9N-VRFFri0M2rR2xkySiWUo",
	}, locale)
	if err != nil {
		t.Fatal(err)
	}
	if form == "html" {
		_ = os.WriteFile("/tmp/mail-preview.html", []byte(msg.HTML), 0o600)
		t.Log("wrote /tmp/mail-preview.html")
		return
	}
	t.Logf("\nSubject: %s\n\n%s", msg.Subject, msg.Body)
}
