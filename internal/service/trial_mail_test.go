package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/i18n"
)

// What a trial applicant actually receives, in both languages and both
// renderings.
//
// The catalogue test next door checks that every locale has every key and
// that each renders; it cannot check that the assembled message says what it
// has to, because the message is assembled here. That matters most for the
// address: it used to be pasted into a translated sentence, where
// internal/i18n's own guard could see it, and it is now placed by the layout
// — so this is where "the link is in the message" is held.

func trialMailer(locale string) *TrialService {
	return &TrialService{messages: i18n.MustLoad(), locale: locale}
}

func TestTheConfirmationMailCarriesItsLinkInBothParts(t *testing.T) {
	const link = "https://demo.example.com/trial/confirm?token=abc123"

	for _, locale := range []string{"en-US", "zh-CN"} {
		msg, err := trialMailer(locale).confirmMail("Acme Ltd", link, locale)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}

		if msg.Subject == "" {
			t.Errorf("%s: no subject", locale)
		}
		if msg.HTML == "" {
			t.Fatalf("%s: no html part", locale)
		}
		// The one thing this message exists to deliver. A translation or a
		// layout change that loses it sends a polite note about a trial with
		// no way to have one.
		if !strings.Contains(msg.Body, link) {
			t.Errorf("%s: the text part has no link:\n%s", locale, msg.Body)
		}
		if !strings.Contains(msg.HTML, link) {
			t.Errorf("%s: the html part has no link", locale)
		}
		// And the tenant somebody asked for, so the message is about
		// something rather than about trials in general.
		if !strings.Contains(msg.Body, "Acme Ltd") {
			t.Errorf("%s: the text part does not name the tenant:\n%s", locale, msg.Body)
		}
	}
}

func TestTheCredentialsMailCarriesEveryCredentialInBothParts(t *testing.T) {
	out := TrialTenant{
		TenantCode:    "mytrial",
		TenantName:    "My Trial",
		AdminUsername: "admin",
		AdminPassword: "P0T5zLoMbaVwccd8XKb8H7my",
		SignInURL:     "https://demo.example.com/login?tenant=mytrial",
		DemoPassword:  "j9N-VRFFri0M2rR2xkySiWUo",
	}

	for _, locale := range []string{"en-US", "zh-CN"} {
		msg, err := trialMailer(locale).readyMail(out, locale)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}

		for _, needed := range []string{
			out.TenantCode, out.AdminUsername, out.AdminPassword,
			out.SignInURL, out.DemoPassword,
		} {
			if !strings.Contains(msg.Body, needed) {
				t.Errorf("%s: the text part is missing %q", locale, needed)
			}
			if !strings.Contains(msg.HTML, needed) {
				t.Errorf("%s: the html part is missing %q", locale, needed)
			}
		}

		// Both passwords are in there, and they are not both called the same
		// thing. They were, and reading the second one meant going back to
		// work out which account it opened.
		if strings.Count(msg.Body, "Password:") > 1 {
			t.Errorf("%s: two different passwords share a label:\n%s", locale, msg.Body)
		}
	}
}

// A fill that failed leaves a tenant that works and no example accounts. The
// message then says nothing about them, rather than offering a password for
// accounts that were never created — which reads as a bug in the product
// rather than as a fill that did not happen.
func TestNoExampleAccountsMeansNoSectionAboutThem(t *testing.T) {
	out := TrialTenant{
		TenantCode: "empty", TenantName: "Empty", AdminUsername: "admin",
		AdminPassword: "x", SignInURL: "https://demo.example.com/login?tenant=empty",
	}
	msg, err := trialMailer("en-US").readyMail(out, "en-US")
	if err != nil {
		t.Fatal(err)
	}

	heading, err := i18n.MustLoad().Render(i18n.Default, i18n.KeyTrialReadyDemoHeading, i18n.TrialData{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Body, heading) || strings.Contains(msg.HTML, heading) {
		t.Errorf("the message offers example accounts that were never created:\n%s", msg.Body)
	}
}

// An unknown locale is English rather than an error or an empty message.
func TestAnUnsetLocaleIsEnglish(t *testing.T) {
	for _, locale := range []string{"", "kl-GL"} {
		msg, err := trialMailer(locale).confirmMail("Acme", "https://demo.example.com/x", locale)
		if err != nil {
			t.Fatalf("%q: %v", locale, err)
		}
		if !strings.Contains(msg.Subject, "Portico") {
			t.Errorf("%q: subject is %q", locale, msg.Subject)
		}
	}
}

// The language the visitor was reading, not the one the deployment prefers.
//
// This is the bug this parameter exists for: the demonstration runs with
// PORTICO_DEFAULT_LOCALE=en-US, somebody filled in the Chinese form, and the
// confirmation arrived in English. There was no signal to resolve a language
// from — a trial applicant has no account and no tenant — except the one
// nobody was reading: they had the page in Chinese in front of them.
func TestTheApplicantsOwnLanguageBeatsTheDeploymentDefault(t *testing.T) {
	// A deployment whose own default is English.
	service := trialMailer("en-US")

	msg, err := service.confirmMail("Acme", "https://demo.example.com/x", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}

	want, err := i18n.MustLoad().Render(mustParse(t, "zh-CN"),
		i18n.KeyTrialConfirmTitle, i18n.TrialData{Tenant: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Body, want) {
		t.Errorf("an applicant reading zh-CN got:\n%s", msg.Body)
	}
}

// And a tag this build does not have falls back rather than rendering nothing.
//
// It arrives in a request body, so it is a claim: anybody can post
// `"locale": "kl-GL"`, or a browser can report a language with no catalogue
// here. Neither may produce an empty message.
func TestAnUnknownLanguageFallsBackToTheDeployment(t *testing.T) {
	for _, requested := range []string{"", "kl-GL", "../../etc/passwd"} {
		msg, err := trialMailer("zh-CN").confirmMail("Acme", "https://demo.example.com/x", requested)
		if err != nil {
			t.Fatalf("%q: %v", requested, err)
		}
		want, err := i18n.MustLoad().Render(mustParse(t, "zh-CN"),
			i18n.KeyTrialConfirmTitle, i18n.TrialData{Tenant: "Acme"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Body, want) {
			t.Errorf("%q did not fall back to the deployment's zh-CN:\n%s", requested, msg.Body)
		}
	}
}

func mustParse(t *testing.T, tag string) i18n.Locale {
	t.Helper()
	locale, ok := i18n.Parse(tag)
	if !ok {
		t.Fatalf("%q is not a locale this build has", tag)
	}
	return locale
}

// The fortnight in the credentials mail is the fortnight the sweep keeps.
//
// The wording is translated copy and belongs to whoever writes it; the number
// in it has to come from TrialTenantTTL, or the message is telling somebody a
// deadline the server does not keep. The link's own expiry is held the same way
// in internal/server/trial_test.go — this is the tenant's.
func TestTheCredentialsMailStatesTheLifeTheSweepEnforces(t *testing.T) {
	out := TrialTenant{
		TenantCode: "mytrial", TenantName: "My Trial", AdminUsername: "admin",
		AdminPassword: "x", SignInURL: "https://demo.example.com/login?tenant=mytrial",
	}
	msg, err := trialMailer("en-US").readyMail(out, "en-US")
	if err != nil {
		t.Fatal(err)
	}

	days := fmt.Sprintf("%d days", int(TrialTenantTTL.Hours()/24))
	if !strings.Contains(msg.Body, days) {
		t.Errorf("the message does not state how long the tenant lasts (%q):\n%s",
			days, msg.Body)
	}
}
