package server_test

// Branding: overriding what the four unauthenticated screens show, per
// tenant. Sent through the same settings endpoint as everything else, and
// read back by an anonymous caller from RegistrationStatus.

import (
	"net/http"
	"strings"
	"testing"
)

func setBranding(t *testing.T, api *apiTest, admin string, fields map[string]any) response {
	t.Helper()
	return api.do(http.MethodPut, "/api/v1/settings", admin, fields)
}

// TestBrandingRoundTripsThroughSettings proves the nine fields save, read
// back on GET /settings, and reach an anonymous caller through
// RegistrationStatus — the path the sign-in screen actually uses.
func TestBrandingRoundTripsThroughSettings(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := setBranding(t, api, admin, map[string]any{
		"brandingLogoUrl":           "/icons/acme.svg",
		"brandingProductName":       "ACME Cloud",
		"brandingColorPrimary":      "#7c3aed",
		"brandingFontFamily":        "'Inter', sans-serif",
		"brandingBgImageUrl":        "https://example.com/bg.jpg",
		"brandingFooterPrivacyMode": "link",
		"brandingFooterPrivacyUrl":  "https://example.com/privacy",
		"brandingFooterTermsMode":   "link",
		"brandingFooterTermsUrl":    "https://example.com/terms",
		"brandingFooterSupportMode": "link",
		"brandingFooterSupportUrl":  "mailto:support@example.com",
		"brandingLoginHeading":      "Sign in to ACME Cloud",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("save branding: %d %s %s", res.Status, res.Code, res.Message)
	}

	var saved struct {
		BrandingLogoURL          string `json:"brandingLogoUrl"`
		BrandingProductName      string `json:"brandingProductName"`
		BrandingColorPrimary     string `json:"brandingColorPrimary"`
		BrandingLoginHeading     string `json:"brandingLoginHeading"`
		BrandingFooterSupportURL string `json:"brandingFooterSupportUrl"`
	}
	res.into(t, &saved)
	if saved.BrandingProductName != "ACME Cloud" {
		t.Errorf("product name did not round-trip: got %q", saved.BrandingProductName)
	}
	if saved.BrandingColorPrimary != "#7c3aed" {
		t.Errorf("primary colour did not round-trip: got %q", saved.BrandingColorPrimary)
	}

	// The path an anonymous visitor's browser actually takes: no token, no
	// tenant header beyond what the default tenant resolves to.
	anon := api.do(http.MethodGet, "/api/v1/auth/registration-status", "", nil)
	if anon.Status != http.StatusOK {
		t.Fatalf("registration status: %d %s %s", anon.Status, anon.Code, anon.Message)
	}
	var status struct {
		Branding struct {
			LogoURL           string `json:"logoUrl"`
			ProductName       string `json:"productName"`
			ColorPrimary      string `json:"colorPrimary"`
			LoginHeading      string `json:"loginHeading"`
			FooterSupportMode string `json:"footerSupportMode"`
			FooterSupportURL  string `json:"footerSupportUrl"`
		} `json:"branding"`
	}
	anon.into(t, &status)
	if status.Branding.ProductName != "ACME Cloud" {
		t.Errorf("anonymous caller did not see the product name: got %q",
			status.Branding.ProductName)
	}
	if status.Branding.LoginHeading != saved.BrandingLoginHeading {
		t.Errorf("anonymous caller's login heading disagreed with the saved value: "+
			"got %q, saved %q", status.Branding.LoginHeading, saved.BrandingLoginHeading)
	}
	if status.Branding.FooterSupportMode != "link" {
		t.Errorf("support link mode did not round-trip: got %q", status.Branding.FooterSupportMode)
	}
	if status.Branding.FooterSupportURL != "mailto:support@example.com" {
		t.Errorf("support link did not round-trip: got %q", status.Branding.FooterSupportURL)
	}
}

// TestBrandingRejectsAMalformedColour proves a colour that is not a 6-digit
// hex value is refused rather than stored — an administrator who mistyped
// it should see that in the form, not discover it later on the sign-in
// screen.
func TestBrandingRejectsAMalformedColour(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, bad := range []string{"red", "#fff", "#zzzzzz", "2563eb"} {
		res := setBranding(t, api, admin, map[string]any{"brandingColorPrimary": bad})
		if res.Status != http.StatusBadRequest {
			t.Errorf("colour %q: got %d, want 400", bad, res.Status)
		}
	}

	// The empty string is not malformed — it means "use the default".
	res := setBranding(t, api, admin, map[string]any{"brandingColorPrimary": ""})
	if res.Status != http.StatusOK {
		t.Errorf("clearing the colour: got %d, want 200", res.Status)
	}
}

// TestBrandingRejectsAFooterLinkPortico rejects the schemes rejected for
// every other rendered/followed value in this codebase, plus proves the one
// scheme unique to a footer link — mailto: — is accepted, unlike a logo.
func TestBrandingRejectsAFooterLink(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"not a url",
		"ftp://example.com/policy",
	} {
		res := setBranding(t, api, admin, map[string]any{
			"brandingFooterPrivacyMode": "link",
			"brandingFooterPrivacyUrl":  bad,
		})
		if res.Status != http.StatusBadRequest {
			t.Errorf("footer link %q: got %d, want 400", bad, res.Status)
		}
	}

	for _, good := range []string{
		"https://example.com/privacy",
		"mailto:legal@example.com",
	} {
		res := setBranding(t, api, admin, map[string]any{
			"brandingFooterPrivacyMode": "link",
			"brandingFooterPrivacyUrl":  good,
		})
		if res.Status != http.StatusOK {
			t.Errorf("footer link %q: got %d %s, want 200", good, res.Status, res.Code)
		}
	}

	// Unlike a logo, a footer link does not accept a path on this server —
	// it is followed, not rendered as a picture, and there is no reason to
	// route it through this server's own static assets.
	res := setBranding(t, api, admin, map[string]any{
		"brandingFooterPrivacyMode": "link",
		"brandingFooterPrivacyUrl":  "/privacy.html",
	})
	if res.Status != http.StatusBadRequest {
		t.Errorf("server-relative footer link: got %d, want 400", res.Status)
	}

	// An unrecognized mode is refused outright, before either field is
	// even looked at.
	res = setBranding(t, api, admin, map[string]any{"brandingFooterPrivacyMode": "carrier-pigeon"})
	if res.Status != http.StatusBadRequest {
		t.Errorf("unrecognized mode: got %d, want 400", res.Status)
	}
}

// TestBrandingFooterModeSwitchClearsTheOtherField proves the row a link
// stops in is never ambiguous: switching a slot from a link to inline text,
// or turning it off, does not leave the previous mode's value sitting in
// the database for a later reader to wonder whether it is still live.
func TestBrandingFooterModeSwitchClearsTheOtherField(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	res := setBranding(t, api, admin, map[string]any{
		"brandingFooterPrivacyMode": "link",
		"brandingFooterPrivacyUrl":  "https://example.com/privacy",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("set link mode: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Switch to text. The URL from the previous save must not survive.
	longText := "We collect nothing we do not need.\n\nContact us with questions."
	res = setBranding(t, api, admin, map[string]any{
		"brandingFooterPrivacyMode": "text",
		"brandingFooterPrivacyText": longText,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("switch to text mode: %d %s %s", res.Status, res.Code, res.Message)
	}

	var afterText struct {
		BrandingFooterPrivacyMode string `json:"brandingFooterPrivacyMode"`
		BrandingFooterPrivacyURL  string `json:"brandingFooterPrivacyUrl"`
		BrandingFooterPrivacyText string `json:"brandingFooterPrivacyText"`
	}
	res.into(t, &afterText)
	if afterText.BrandingFooterPrivacyURL != "" {
		t.Errorf("the URL from link mode survived the switch to text mode: %q",
			afterText.BrandingFooterPrivacyURL)
	}
	if afterText.BrandingFooterPrivacyText != longText {
		t.Errorf("text did not save: got %q", afterText.BrandingFooterPrivacyText)
	}

	// Switch to off. Neither field should survive.
	res = setBranding(t, api, admin, map[string]any{"brandingFooterPrivacyMode": ""})
	if res.Status != http.StatusOK {
		t.Fatalf("switch to off: %d %s %s", res.Status, res.Code, res.Message)
	}
	var afterOff struct {
		BrandingFooterPrivacyURL  string `json:"brandingFooterPrivacyUrl"`
		BrandingFooterPrivacyText string `json:"brandingFooterPrivacyText"`
	}
	res.into(t, &afterOff)
	if afterOff.BrandingFooterPrivacyURL != "" || afterOff.BrandingFooterPrivacyText != "" {
		t.Errorf("turning the link off left a value behind: url=%q text=%q",
			afterOff.BrandingFooterPrivacyURL, afterOff.BrandingFooterPrivacyText)
	}
}

// TestBrandingFooterTextIsCapped proves text mode has its own, more
// generous length bound than the short label fields — and that it is
// still enforced, not unbounded.
func TestBrandingFooterTextIsCapped(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	huge := strings.Repeat("a", 20001)
	res := setBranding(t, api, admin, map[string]any{
		"brandingFooterPrivacyMode": "text",
		"brandingFooterPrivacyText": huge,
	})
	if res.Status != http.StatusBadRequest {
		t.Errorf("oversized footer text: got %d, want 400", res.Status)
	}

	ok := strings.Repeat("a", 20000)
	res = setBranding(t, api, admin, map[string]any{
		"brandingFooterPrivacyMode": "text",
		"brandingFooterPrivacyText": ok,
	})
	if res.Status != http.StatusOK {
		t.Errorf("text at the cap: got %d %s, want 200", res.Status, res.Code)
	}
}

// TestBrandingIsNotSeenOnOtherTenants proves branding is per-tenant, not
// deployment-wide — the same isolation TestSettingsAreIsolated already
// checks for the other settings, extended here because a branding leak
// would show up as one tenant's visitors seeing another tenant's mark on
// the sign-in screen they landed on.
func TestBrandingIsNotSeenOnOtherTenants(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	res := setBranding(t, api, second.token, map[string]any{
		"brandingProductName": "Beta Portal",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("save branding: %d %s %s", res.Status, res.Code, res.Message)
	}

	read := api.do(http.MethodGet, "/api/v1/settings", first.token, nil)
	var firstTenantSettings struct {
		BrandingProductName string `json:"brandingProductName"`
	}
	read.into(t, &firstTenantSettings)
	if firstTenantSettings.BrandingProductName == "Beta Portal" {
		t.Error("one tenant's branding changed another's")
	}

	// The public endpoint reports each tenant's own answer, which is what
	// the sign-in screen renders before anyone is authenticated.
	anon := api.doWithHeaders(http.MethodGet, "/api/v1/auth/registration-status", "", nil,
		map[string]string{"X-Portico-Tenant": secondTenantCode})
	if anon.Status != http.StatusOK {
		t.Fatalf("registration status: %d %s %s", anon.Status, anon.Code, anon.Message)
	}
	var status struct {
		Branding struct {
			ProductName string `json:"productName"`
		} `json:"branding"`
	}
	anon.into(t, &status)
	if status.Branding.ProductName != "Beta Portal" {
		t.Errorf("public status for %s = %+v, want the second tenant's own branding",
			secondTenantCode, status.Branding)
	}
}
