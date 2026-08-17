package service

import (
	"html"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/notify"
)

// What somebody asking to reset a password, or to confirm an address,
// actually receives — in every locale and in both renderings.
//
// This is where the coverage went when the `.body` keys were split into
// parts. internal/i18n used to hold "the message contains its link", because
// the message was one string it could see; the message is now assembled here,
// so the rule has to be here too. Without it, a layout change that dropped
// the address would leave both the catalogue tests and the delivery tests
// green and every reset mail useless.

func locales(t *testing.T) []i18n.Locale {
	t.Helper()
	supported := i18n.Supported()
	if len(supported) < 2 {
		t.Fatalf("only %d locale(s) — this test is about translations", len(supported))
	}
	return supported
}

func assertCarriesLink(t *testing.T, locale i18n.Locale, msg notify.Message, link, tenant string) {
	t.Helper()

	if msg.Subject == "" {
		t.Errorf("%s: no subject", locale)
	}
	if msg.HTML == "" {
		t.Fatalf("%s: no html part", locale)
	}
	if !strings.Contains(msg.Body, link) {
		t.Errorf("%s: the text part has no link:\n%s", locale, msg.Body)
	}
	// The same address, spelled differently on purpose. A reset link carries
	// two query parameters, so it has an & in it, and an & in HTML is
	// &amp; — in the href and in the line of text underneath. Asserting the
	// raw form here would be asserting that the escaping is broken.
	if escaped := html.EscapeString(link); !strings.Contains(msg.HTML, escaped) {
		t.Errorf("%s: the html part has no link", locale)
	}
	// The tenant's name is the wordmark on these two: they are from the
	// organization the account belongs to, not from the software it runs.
	if !strings.Contains(msg.Body, tenant) {
		t.Errorf("%s: the text part does not name the tenant:\n%s", locale, msg.Body)
	}
	if !strings.Contains(msg.HTML, tenant) {
		t.Errorf("%s: the html part does not name the tenant", locale)
	}
}

func TestTheResetMailCarriesItsLinkInBothParts(t *testing.T) {
	const link = "https://portico.example/reset-password?tenant=acme&token=abc123"

	for _, locale := range locales(t) {
		service := &RecoveryService{messages: i18n.MustLoad()}
		msg, err := service.email(locale, "Acme Ltd", link, i18n.RecoveryData{
			Tenant: "Acme Ltd", Name: "Sam", Username: "sam",
			Link: link, Minutes: 30,
		})
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		assertCarriesLink(t, locale, msg, link, "Acme Ltd")
	}
}

func TestTheConfirmAddressMailCarriesItsLinkInBothParts(t *testing.T) {
	const link = "https://portico.example/verify?tenant=acme&token=abc123"

	for _, locale := range locales(t) {
		service := &VerificationService{messages: i18n.MustLoad()}
		msg, err := service.email(locale, "Acme Ltd", link, i18n.VerificationData{
			Tenant: "Acme Ltd", Name: "Sam", Username: "sam",
			Link: link, Hours: 24,
		})
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		assertCarriesLink(t, locale, msg, link, "Acme Ltd")
	}
}

// A tenant names itself, and what it types is not markup.
//
// The name reaches these messages as the wordmark at the top, which is a
// position the trial mail's escaping tests do not cover — they exercise it as
// a value in a sentence. Same escaping, different hole to leave open.
func TestATenantsNameCannotWriteMarkupIntoAReset(t *testing.T) {
	service := &RecoveryService{messages: i18n.MustLoad()}
	msg, err := service.email(i18n.Default, `<script>alert(1)</script>`,
		"https://portico.example/reset-password?token=x", i18n.RecoveryData{
			Tenant: "x", Name: "Sam", Username: "sam",
			Link: "https://portico.example/reset-password?token=x", Minutes: 30,
		})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "<script") {
		t.Errorf("a tenant's name became markup:\n%s", msg.HTML)
	}
}
