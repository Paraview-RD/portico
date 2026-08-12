package server_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/casp"
	"github.com/Paraview-RD/portico/internal/model"
)

// CAS attributes are XML elements, so a rename changes the document's shape
// rather than a value in it. That is the thing worth checking over a real
// validation rather than in a unit test: the response is assembled by the
// encoder, and the fixed struct tags this replaced could not have carried a
// name decided at runtime.

// casValidation returns the raw CAS 3.0 validation document for one sign-in.
func casValidation(t *testing.T, f *federationTest, serviceURL, username, password string) string {
	t.Helper()

	ticket := f.casSignIn("", serviceURL, model.DefaultTenantCode, username, password)

	target := f.publicURL + casp.ValidatePath3 +
		"?service=" + url.QueryEscape(serviceURL) +
		"&ticket=" + url.QueryEscape(ticket)
	res, err := http.Get(target)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the validation response: %v", err)
	}
	return string(body)
}

// A CAS service receives the element names it was given.
func TestAConfiguredCASServiceReceivesTheNamesItWasGiven(t *testing.T) {
	f := newFederationTest(t)
	admin := f.api.adminToken()

	var org struct {
		ID string `json:"id"`
	}
	res := f.api.do(http.MethodPost, "/api/v1/organizations", admin, map[string]any{
		"name": "CAS Mapped Org", "code": "CASORG",
	})
	res.into(t, &org)

	const username = "cas.mapped"
	const password = "cas-mapped-password-1"
	if res := f.api.do(http.MethodPost, "/api/v1/users", admin, map[string]any{
		"username": username, "displayName": "CAS Mapped",
		"email": "cas.mapped@example.test", "phone": "+442079460103",
		"password": password, "role": "USER", "organizationId": org.ID,
	}); res.Status != http.StatusOK {
		t.Fatalf("create the account: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Registered first, so its id is known before the rules are saved.
	const serviceURL = "https://mapped.example.com/cas"
	const prefix = "https://mapped.example.com/"
	f.registerCASService(model.DefaultTenantCode, prefix, "Mapped CAS")

	id := firstID(t, f.api, admin, "/api/v1/applications/cas-services")
	if id == "" {
		t.Fatal("the CAS service was not registered")
	}
	if res := f.api.do(http.MethodPut,
		"/api/v1/applications/cas-services/"+id+"/field-mappings", admin,
		map[string]any{"mappings": []map[string]any{
			{"sourceKey": "email", "targetName": "mail"},
			{"sourceKey": "phone", "suppressed": true},
			{"sourceKey": "organization_code", "targetName": "orgCode"},
		}}); res.Status != http.StatusOK {
		t.Fatalf("save mappings: %d %s %s", res.Status, res.Code, res.Message)
	}

	document := casValidation(t, f, serviceURL, username, password)

	for _, want := range []string{
		"<cas:mail>cas.mapped@example.test</cas:mail>",
		"<cas:orgCode>CASORG</cas:orgCode>",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the validation response does not contain %s:\n%s", want, document)
		}
	}
	for _, unwanted := range []string{
		"<cas:email>", // renamed away
		"<cas:phone>", // suppressed
	} {
		if strings.Contains(document, unwanted) {
			t.Errorf("the validation response still contains %s, so a rule was not "+
				"applied:\n%s", unwanted, document)
		}
	}
	// The username is not an attribute and is not mappable — it is what every
	// CAS client keys its local record on.
	if !strings.Contains(document, "<cas:user>"+username+"</cas:user>") {
		t.Errorf("the username element changed:\n%s", document)
	}
}
