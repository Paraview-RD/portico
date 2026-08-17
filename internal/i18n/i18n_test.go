package i18n_test

// What these tests defend is the thing translation always fails at: not
// getting a sentence wrong, but getting one missing.
//
// The console's translations have a compiler doing this — the Chinese error
// messages are declared as Record<keyof typeof errorsEnUS, string>, so
// omitting a code does not build. Messages here are JSON, which no compiler
// reads, so the same guarantee has to be a test.

import (
	"sort"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/i18n"
)

// sampleFor returns data of the type a key is rendered with in production.
// A new message key needs an entry here, which is the point: an unlisted key
// fails the renderability test rather than shipping unrendered.
func sampleFor(key string) any {
	switch {
	case strings.HasPrefix(key, "recovery."):
		return i18n.RecoveryData{
			Tenant: "Acme", Name: "Sam", Username: "sam",
			Link: "https://portico.example/reset?token=x", Minutes: 30,
		}
	case strings.HasPrefix(key, "verification."):
		return i18n.VerificationData{
			Tenant: "Acme", Name: "Sam", Username: "sam",
			Link: "https://portico.example/verify?token=x", Hours: 24,
		}
	case strings.HasPrefix(key, "trial."):
		return i18n.TrialData{Tenant: "Acme", Hours: 24}
	default:
		return nil
	}
}

func TestEveryLocaleHasEveryMessageEnglishHas(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	english := make(map[string]bool)
	for _, key := range catalog.Keys(i18n.Default) {
		english[key] = true
	}
	if len(english) == 0 {
		t.Fatal("the default locale has no messages at all")
	}

	for _, locale := range i18n.Supported() {
		if locale == i18n.Default {
			continue
		}
		have := make(map[string]bool)
		for _, key := range catalog.Keys(locale) {
			have[key] = true
		}

		var missing, extra []string
		for key := range english {
			if !have[key] {
				missing = append(missing, key)
			}
		}
		for key := range have {
			if !english[key] {
				extra = append(extra, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)

		if len(missing) > 0 {
			t.Errorf("%s is missing %v — a reader in that language would get "+
				"English for those, which is the half-translated state this "+
				"test exists to prevent", locale, missing)
		}
		// Extra keys are a failure too, not a curiosity: a key only in a
		// translation is one somebody renamed in English and did not rename
		// here, so the translated message is dead text nothing renders.
		if len(extra) > 0 {
			t.Errorf("%s has %v, which English does not — dead text, or a "+
				"rename that only happened on one side", locale, extra)
		}
	}
}

func TestEveryMessageRendersWithTheDataItIsGiven(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, locale := range i18n.Supported() {
		for _, key := range catalog.Keys(locale) {
			data := sampleFor(key)
			if data == nil {
				t.Errorf("%s: no sample data for %q, so nothing checks that "+
					"it renders", locale, key)
				continue
			}

			out, err := catalog.Render(locale, key, data)
			if err != nil {
				t.Errorf("%s %q: %v", locale, key, err)
				continue
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("%s %q rendered empty", locale, key)
			}
			// A template that still contains its own syntax rendered
			// something it should not have.
			if strings.Contains(out, "{{") {
				t.Errorf("%s %q rendered with template syntax left in: %q",
					locale, key, out)
			}
		}
	}
}

// The link is the entire purpose of the messages that carry one inside a
// sentence. A translation that drops it delivers a polite note about a
// password reset with no way to reset one, and every other assertion here
// would still pass.
//
// Only the `.body` keys, and that is the shape of the rule rather than a
// list: a body is one string holding a whole message, so the link is in it or
// it is nowhere. The trial messages are assembled from parts by
// internal/mailfmt, which places the address itself — as a button and as
// readable text — so no string of theirs contains one, and requiring it would
// mean a label reading "Tenant" had to have a URL in it.
func TestEveryMessageKeepsTheLink(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, locale := range i18n.Supported() {
		for _, key := range catalog.Keys(locale) {
			if !strings.HasSuffix(key, ".body") && !strings.HasSuffix(key, ".sms") {
				continue
			}
			out, err := catalog.Render(locale, key, sampleFor(key))
			if err != nil {
				t.Fatalf("%s %q: %v", locale, key, err)
			}
			if !strings.Contains(out, "https://portico.example/") {
				t.Errorf("%s %q does not contain the link", locale, key)
			}
		}
	}
}

func TestResolveTakesTheMostSpecificPreferenceThatMeansAnything(t *testing.T) {
	cases := []struct {
		name                                      string
		account, tenantDefault, deploymentDefault string
		want                                      i18n.Locale
	}{
		{"the account's own preference wins", "zh-CN", "en-US", "en-US", i18n.ZhCN},
		{"the tenant's default when the account said nothing", "", "zh-CN", "en-US", i18n.ZhCN},
		{"the deployment's when neither did", "", "", "zh-CN", i18n.ZhCN},
		{"English when nobody did", "", "", "", i18n.EnUS},
		// Arrives from a directory or a SCIM push as often as from a form.
		{"a value nothing ships falls through rather than erroring", "tlh", "zh-CN", "", i18n.ZhCN},
		{"and all the way to English if need be", "tlh", "xx", "yy", i18n.EnUS},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := i18n.Resolve(tc.account, tc.tenantDefault, tc.deploymentDefault); got != tc.want {
				t.Errorf("Resolve(%q, %q, %q) = %q, want %q",
					tc.account, tc.tenantDefault, tc.deploymentDefault, got, tc.want)
			}
		})
	}
}

func TestParseAcceptsTheTagsBrowsersAndDirectoriesActuallySend(t *testing.T) {
	cases := map[string]i18n.Locale{
		"en-US":   i18n.EnUS,
		"en":      i18n.EnUS,
		"EN-us":   i18n.EnUS,
		"zh-CN":   i18n.ZhCN,
		"zh":      i18n.ZhCN,
		"zh-Hans": i18n.ZhCN,
		// Simplified text is wrong for a 繁體 reader and much closer than
		// English. Shipping zh-TW is a translation, not a lookup rule.
		"zh-TW": i18n.ZhCN,
	}
	for tag, want := range cases {
		got, ok := i18n.Parse(tag)
		if !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q, true", tag, got, ok, want)
		}
	}

	for _, tag := range []string{"", "  ", "tlh", "de-DE"} {
		if got, ok := i18n.Parse(tag); ok {
			t.Errorf("Parse(%q) = %q, true; want no match", tag, got)
		}
	}
}
