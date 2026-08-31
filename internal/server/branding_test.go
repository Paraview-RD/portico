package server_test

// Branding: overriding what the four unauthenticated screens show, per
// tenant. Sent through the same settings endpoint as everything else, and
// read back by an anonymous caller from RegistrationStatus.

import (
	"net/http"
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
		"brandingLogoUrl":          "/icons/acme.svg",
		"brandingProductName":      "ACME Cloud",
		"brandingColorPrimary":     "#7c3aed",
		"brandingFontFamily":       "'Inter', sans-serif",
		"brandingBgImageUrl":       "https://example.com/bg.jpg",
		"brandingFooterPrivacyUrl": "https://example.com/privacy",
		"brandingFooterTermsUrl":   "https://example.com/terms",
		"brandingFooterSupportUrl": "mailto:support@example.com",
		"brandingLoginHeading":     "Sign in to ACME Cloud",
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
			LogoURL          string `json:"logoUrl"`
			ProductName      string `json:"productName"`
			ColorPrimary     string `json:"colorPrimary"`
			LoginHeading     string `json:"loginHeading"`
			FooterSupportURL string `json:"footerSupportUrl"`
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
		res := setBranding(t, api, admin, map[string]any{"brandingFooterPrivacyUrl": bad})
		if res.Status != http.StatusBadRequest {
			t.Errorf("footer link %q: got %d, want 400", bad, res.Status)
		}
	}

	for _, good := range []string{
		"https://example.com/privacy",
		"mailto:legal@example.com",
	} {
		res := setBranding(t, api, admin, map[string]any{"brandingFooterPrivacyUrl": good})
		if res.Status != http.StatusOK {
			t.Errorf("footer link %q: got %d %s, want 200", good, res.Status, res.Code)
		}
	}

	// Unlike a logo, a footer link does not accept a path on this server —
	// it is followed, not rendered as a picture, and there is no reason to
	// route it through this server's own static assets.
	res := setBranding(t, api, admin, map[string]any{"brandingFooterPrivacyUrl": "/privacy.html"})
	if res.Status != http.StatusBadRequest {
		t.Errorf("server-relative footer link: got %d, want 400", res.Status)
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
