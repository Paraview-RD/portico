package server_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/crewjam/saml"
	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/Paraview-RD/portico/internal/casp"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/samlp"
	"github.com/Paraview-RD/portico/internal/service"
)

// The three protocols send the same person's details under three sets of
// names, and docs/federation.md is where an integrator reads which.
//
// That page said CAS used "the same names the other protocols use". It does
// not and cannot: OpenID Connect's names are its specification's, so CAS
// sends `phone` where OpenID Connect sends `phone_number`, and SAML sends
// `telephoneNumber`. An integrator mapping a CAS attribute from the OpenID
// Connect column got an empty field and no error — the fact simply never
// arrives, which reads as an account with no phone number rather than as a
// mapping that was never right.
//
// The tables cannot be checked by reading them, so they are checked by
// sending an assertion and a userinfo document and a validation response for
// one account that has every fact, and comparing the names that come back
// with the names the page lists. Both directions: a name emitted and not
// listed is what an integrator cannot find, and a name listed and not
// emitted is what they map and never receive.
func TestEachProtocolSendsTheNamesTheManualLists(t *testing.T) {
	f := newFederationTest(t)
	adminToken := f.api.adminToken()

	// An account with every fact this system has about anybody: an
	// organization, an email address, and a phone number. Anything left
	// unset is a name that would be legitimately absent, and would make the
	// comparison weaker exactly where it is meant to be strict.
	var org struct {
		ID string `json:"id"`
	}
	res := f.api.do(http.MethodPost, "/api/v1/organizations", adminToken, map[string]any{
		"name": "Attribute Names", "code": "ATTRNAMES",
	})
	res.into(t, &org)

	const username = "attribute.names"
	const password = "attribute-password-1"
	if res := f.api.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{
		"username":       username,
		"displayName":    "Attribute Names",
		"email":          "attribute.names@example.test",
		"phone":          "+442079460101",
		"password":       password,
		"role":           "USER",
		"organizationId": org.ID,
	}); res.Status != http.StatusOK {
		t.Fatalf("create the account: %d %s %s", res.Status, res.Code, res.Message)
	}

	emitted := map[string]map[string]bool{
		"OpenID Connect": f.oidcClaimNames(username, password),
		"CAS":            f.casAttributeNames(username, password),
	}
	samlNames, samlPairs := f.samlAttributeNames(username, password)
	emitted["SAML"] = samlNames

	// Both translations, because a table is exactly the kind of thing that
	// gets corrected in one language and left in the other, and an integrator
	// reading the Chinese page is configuring the same service provider.
	for _, doc := range federationDocs {
		t.Run(doc.path, func(t *testing.T) {
			for protocol, documented := range crossProtocolTable(t, doc) {
				compareNames(t, doc.path, protocol, documented, emitted[protocol])
			}

			// And the second table, which is the one a service provider's
			// configuration actually keys on: the friendly name is a label,
			// the Name is what gets mapped.
			for friendly, name := range samlAttributeNameTable(t, doc) {
				got, present := samlPairs[friendly]
				if !present {
					t.Errorf("%s lists the SAML attribute %s, which no "+
						"assertion carries", doc.path, friendly)
					continue
				}
				if got != name {
					t.Errorf("SAML attribute %s is sent with Name %q; %s says "+
						"%q, and that is the string a service provider maps on",
						friendly, got, doc.path, name)
				}
			}
		})
	}
}

// The two documents, and the header text that identifies each table in each.
// The headings are translated; the names in the cells are not, and that is
// the point of checking both.
type attributeDoc struct {
	path        string
	crossMarker string
	samlMarker  string
}

var federationDocs = []attributeDoc{
	{
		path:        "docs/federation.md",
		crossMarker: "OpenID Connect claim",
		samlMarker:  "Attribute Name a service provider maps on",
	},
	{
		path:        "docs/federation.zh.md",
		crossMarker: "OpenID Connect 声明",
		samlMarker:  "服务提供方实际映射的 Attribute Name",
	},
}

func compareNames(t *testing.T, path, protocol string, documented, emitted map[string]bool) {
	t.Helper()

	if len(documented) == 0 {
		t.Fatalf("%s lists no %s names at all", path, protocol)
	}
	for name := range emitted {
		if !documented[name] {
			t.Errorf("%s sends %s, which %s does not list; an integrator "+
				"cannot map what the page does not mention",
				protocol, name, path)
		}
	}
	for name := range documented {
		if !emitted[name] {
			t.Errorf("%s lists the %s name %s, which is not sent for an "+
				"account that has every fact; somebody will map it and "+
				"receive nothing", path, protocol, name)
		}
	}
}

// oidcClaimNames signs in over OpenID Connect asking for every scope the
// provider offers, and returns the keys of the userinfo document.
//
// Every scope on purpose: a claim only appears for a scope that was asked
// for, so a narrower request would make a missing name look correct.
func (f *federationTest) oidcClaimNames(username, password string) map[string]bool {
	f.t.Helper()

	tenant, err := f.tenants.Resolve(context.Background(), model.DefaultTenantCode)
	if err != nil {
		f.t.Fatalf("resolve tenant: %v", err)
	}
	registered, err := f.clients.Register(context.Background(),
		service.CommandLineActor(tenant.ID), service.RegisterClientInput{
			ClientID:     "attribute-names",
			Name:         "Attribute Names",
			RedirectURIs: []string{"http://127.0.0.1:9999/callback"},
			Scopes:       []string{"openid", "profile", "email", "phone"},
		})
	if err != nil {
		f.t.Fatalf("register client: %v", err)
	}

	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party, err := rp.NewRelyingPartyOIDC(context.Background(), issuer,
		registered.Client.ClientID, registered.Secret,
		"http://127.0.0.1:9999/callback",
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone})
	if err != nil {
		f.t.Fatalf("discovery: %v", err)
	}

	verifier := "a-code-verifier-long-enough-to-be-respectable"
	authURL := rp.AuthURL("state-attribute-names", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)))
	code, _ := f.signIn(authURL, model.DefaultTenantCode, username, password)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		f.t.Fatalf("code exchange: %v", err)
	}

	names := map[string]bool{}
	for name := range f.rawUserinfo(issuer, tokens.AccessToken) {
		names[name] = true
	}
	return names
}

// samlAttributeNames signs in over SAML and returns the friendly names an
// assertion carries, together with the Name each was sent under.
func (f *federationTest) samlAttributeNames(username, password string) (map[string]bool, map[string]string) {
	f.t.Helper()

	sp := newTestSP(f.t, "https://names.example.com/saml")
	f.registerSP(model.DefaultTenantCode, sp.metadata())
	sp.configure(f, samlp.TenantMount(model.DefaultTenantCode))

	request, err := sp.sp.MakeAuthenticationRequest(
		sp.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		f.t.Fatalf("build authentication request: %v", err)
	}
	authURL, err := request.Redirect("", sp.sp)
	if err != nil {
		f.t.Fatalf("build redirect: %v", err)
	}

	encoded := f.samlSignIn(authURL, model.DefaultTenantCode, username, password)
	assertion, err := sp.parse(encoded, request.ID)
	if err != nil {
		f.t.Fatalf("the service provider rejected the assertion: %v", err)
	}

	names := map[string]bool{}
	pairs := map[string]string{}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			// Keyed by the friendly name, falling back to the Name where
			// there is none. `subject-id` is the one without: it comes from
			// the subject identifier profile, which defines a Name and no
			// label, and a table keyed on friendly names alone would have
			// silently held an entry called "".
			label := attribute.FriendlyName
			if label == "" {
				label = attribute.Name
			}
			names[label] = true
			pairs[label] = attribute.Name
		}
	}
	return names, pairs
}

// casAttributeNames signs in over CAS and returns the element names inside
// `cas:attributes`, read from the XML rather than through a struct — a
// struct only sees the fields somebody remembered to declare on it, which
// is the failure this test exists to catch.
func (f *federationTest) casAttributeNames(username, password string) map[string]bool {
	f.t.Helper()

	const serviceURL = "https://names.example.com/cas"
	f.registerCASService(model.DefaultTenantCode, "https://names.example.com/", "Names")
	ticket := f.casSignIn("", serviceURL, model.DefaultTenantCode, username, password)

	target := f.publicURL + casp.ValidatePath3 +
		"?service=" + url.QueryEscape(serviceURL) +
		"&ticket=" + url.QueryEscape(ticket)
	res, err := http.Get(target)
	if err != nil {
		f.t.Fatalf("validate: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	names := map[string]bool{}
	decoder := xml.NewDecoder(res.Body)
	inside := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			f.t.Fatalf("read the CAS response: %v", err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "attributes" {
				inside = true
				continue
			}
			if inside {
				names[element.Name.Local] = true
			}
		case xml.EndElement:
			if element.Name.Local == "attributes" {
				inside = false
			}
		}
	}
	if len(names) == 0 {
		f.t.Fatal("the CAS 3.0 validation response carried no attributes")
	}
	return names
}

// crossProtocolTable reads the table naming each fact in all three
// protocols, and returns the set of names in each protocol's column.
func crossProtocolTable(t *testing.T, doc attributeDoc) map[string]map[string]bool {
	t.Helper()

	columns := map[int]string{1: "OpenID Connect", 2: "SAML", 3: "CAS"}
	names := map[string]map[string]bool{
		"OpenID Connect": {}, "SAML": {}, "CAS": {},
	}

	for _, row := range tableRows(t, doc.path, doc.crossMarker) {
		for index, protocol := range columns {
			if index >= len(row) {
				continue
			}
			for _, name := range backticked(row[index]) {
				names[protocol][name] = true
			}
		}
	}
	return names
}

// samlAttributeNameTable reads the friendly name to Name mapping.
func samlAttributeNameTable(t *testing.T, doc attributeDoc) map[string]string {
	t.Helper()

	pairs := map[string]string{}
	for _, row := range tableRows(t, doc.path, doc.samlMarker) {
		friendly, name := backticked(row[0]), backticked(row[1])
		if len(friendly) != 1 || len(name) != 1 {
			t.Fatalf("the SAML attribute table in %s has a row this test "+
				"cannot read: %v", doc.path, row)
		}
		pairs[friendly[0]] = name[0]
	}
	return pairs
}

// tableRows finds the markdown table whose header contains marker, and
// returns its body rows split into cells.
func tableRows(t *testing.T, path, marker string) [][]string {
	t.Helper()

	content, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var rows [][]string
	inside := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			if inside {
				break
			}
			continue
		}
		if !inside {
			if strings.Contains(trimmed, marker) {
				inside = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "|---") || strings.HasPrefix(trimmed, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatalf("%s has no table whose header says %q", path, marker)
	}
	return rows
}

var backtickedName = regexp.MustCompile("`([^`]+)`")

// backticked pulls the code-formatted names out of a table cell. A cell
// saying a protocol does not send something states that in prose, without
// backticks, so it contributes nothing.
func backticked(cell string) []string {
	var names []string
	for _, match := range backtickedName.FindAllStringSubmatch(cell, -1) {
		names = append(names, match[1])
	}
	sort.Strings(names)
	return names
}
