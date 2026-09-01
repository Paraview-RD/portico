package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// The OpenAPI document is hand-written, and this is what stops it becoming
// fiction.
//
// A generated document cannot say why an endpoint exists, and annotations in
// the handlers would drift from the handlers exactly as readily as a separate
// file does — they would just drift somewhere less visible, at the cost of a
// code generator in the build. So the document is written by hand and the
// drift is made into a test failure instead.
//
// The check is both ways round on purpose. An endpoint added without being
// described leaves integrators with a spec that silently omits it; a path
// described but not served sends them at a 404 with a straight face. The
// second is the one a person writing documentation is more likely to
// produce, and the one nothing else would catch.

const specPath = "../../docs/api/openapi.yaml"

// specRoot is the prefix this document covers.
//
// SCIM, OpenID Connect, SAML, and CAS are defined by their own
// specifications, and restating them here would produce a second description
// able to disagree with the first. /api/v1 is the surface with no external
// definition.
const specRoot = "/api/v1"

// openAPI is as much of the document as this test needs.
//
// gopkg.in/yaml.v3 is already in the module graph — several dependencies use
// it — so parsing properly costs nothing that a line scanner would have
// saved, and a line scanner would quietly under-report on a reformat.
type openAPI struct {
	Paths map[string]map[string]struct {
		OperationID string `yaml:"operationId"`
		Summary     string `yaml:"summary"`
	} `yaml:"paths"`
}

// operation is one method on one path.
type operation struct {
	Method string
	Path   string
}

func (o operation) String() string { return o.Method + " " + o.Path }

// httpMethods are the ones an operation object may describe. Anything else in
// a path item — `parameters`, `summary` — is not an operation.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// normalize removes a trailing slash so that chi's `/api/v1/users/`, which is
// how a sub-router's root registers, compares equal to the `/api/v1/users` a
// client is told to call. Both are served; the document names the canonical
// one, as the URL conventions require.
func normalize(path string) string {
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		return strings.TrimSuffix(path, "/")
	}
	return path
}

func routedOperations(t *testing.T, handler http.Handler) map[operation]bool {
	t.Helper()

	router, ok := handler.(chi.Routes)
	if !ok {
		t.Fatal("the server's handler is not a chi router; this test walks its route tree")
	}

	found := map[operation]bool{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, specRoot) {
			return nil
		}
		found[operation{Method: method, Path: normalize(route)}] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return found
}

func describedOperations(t *testing.T) map[operation]bool {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	var doc openAPI
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	if len(doc.Paths) == 0 {
		t.Fatalf("%s describes no paths at all; the document or this test is broken", specPath)
	}

	described := map[operation]bool{}
	for path, item := range doc.Paths {
		for method, op := range item {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}
			// Not decoration. Without an operationId a generated client
			// names the method after the path, and without a summary the
			// reference is a list of URLs.
			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId", strings.ToUpper(method), path)
			}
			if op.Summary == "" {
				t.Errorf("%s %s has no summary", strings.ToUpper(method), path)
			}
			described[operation{Method: strings.ToUpper(method), Path: normalize(path)}] = true
		}
	}
	return described
}

func sortedList(ops map[operation]bool, keep func(operation) bool) []string {
	var out []string
	for op := range ops {
		if keep(op) {
			out = append(out, op.String())
		}
	}
	sort.Strings(out)
	return out
}

func TestOpenAPIDescribesEveryRoute(t *testing.T) {
	// With every optional feature on, because the document describes them and
	// their endpoints are only routed where a deployment asked for them: two
	// of the three trial addresses, and both of the operator console's.
	//
	// The alternative was a list of exceptions here, which would have to be
	// kept in step with the routing by hand and would quietly grow. Turning
	// the features on instead makes the comparison exact in both directions,
	// and adds a check nothing else makes: that each of them is actually
	// served when it is supposed to be.
	cfg := testConfig(t)
	cfg.TrialSignup = true
	cfg.TenantConsole = true
	api := newAPITestWithConfig(t, cfg)

	routed := routedOperations(t, api.srv.Handler())
	described := describedOperations(t)

	if len(routed) == 0 {
		t.Fatal("walked the router and found no /api/v1 routes; this test is not checking anything")
	}

	undocumented := sortedList(routed, func(op operation) bool { return !described[op] })
	if len(undocumented) > 0 {
		t.Errorf("served but not described in %s — an integrator reading the "+
			"document would not know these exist:\n  %s",
			specPath, strings.Join(undocumented, "\n  "))
	}

	imaginary := sortedList(described, func(op operation) bool { return !routed[op] })
	if len(imaginary) > 0 {
		t.Errorf("described in %s but not served — a client generated from the "+
			"document would call these and get a 404:\n  %s",
			specPath, strings.Join(imaginary, "\n  "))
	}
}

// TestOpenAPIDoesNotRestateOtherSpecifications guards the boundary the
// document draws, which is a decision rather than an omission.
//
// SCIM and the federation endpoints have their own specifications. Describing
// them here would create a second, hand-maintained account of somebody else's
// protocol — one that can disagree with the real one, and that no client
// library reads.
func TestOpenAPIDoesNotRestateOtherSpecifications(t *testing.T) {
	described := describedOperations(t)

	for op := range described {
		if !strings.HasPrefix(op.Path, specRoot) {
			t.Errorf("%s is outside %s. If it is a protocol endpoint it belongs "+
				"in its own specification, not here; see docs/federation.md and "+
				"docs/scim.md.", op, specRoot)
		}
	}
}

// A response schema names exactly the fields its type serializes.
//
// `TestOpenAPIDescribesEveryRoute` above is about routes, and routes were the
// half that stayed honest. The schemas drifted underneath it: `User` gained
// `closedAt` — which is the whole of the account-closure feature, the
// difference between "they left" and "we suspended them" — and `externalId`,
// which is how an administrator sees that a directory owns an account before
// wondering why their edit was overwritten. Neither was in the document, so
// neither exists for anybody generating a client from it.
//
// The other direction is worse and also happened: `SAMLServiceProvider`
// advertised `metadataXml`, a field tagged `json:"-"` that no response has
// ever carried. A generated client has a property that is always empty and
// nothing to say why.
//
// Reflection, not a second list, because a second list is what drifted.
func TestEverySchemaNamesTheFieldsItsTypeSends(t *testing.T) {
	schemas := specSchemas(t)

	for _, subject := range []struct {
		schema string
		value  any
	}{
		{"User", model.User{}},
		{"UserProfile", model.UserProfile{}},
		{"Organization", model.Organization{}},
		{"Group", model.Group{}},
		{"SAMLServiceProvider", model.SAMLServiceProvider{}},
		{"CASService", model.CASService{}},
		{"OAuthClient", model.OAuthClient{}},
		{"Invitation", model.Invitation{}},
		{"LDAPSource", model.LDAPSource{}},
		{"LDAPSyncRun", model.LDAPSyncRun{}},
		// Not every response type is a model type. These are the service
		// layer's, because what a console is shown about a subscription or
		// a credential is deliberately less than what is stored — no
		// secret, no header values — and that decision belongs beside the
		// service that makes it.
		//
		// What this list cannot reach is a response struct declared inside
		// `handler`, which is unexported by design. `RegisteredClient` is
		// one such: the service type it is built from carries no JSON tags
		// at all, because it never goes on the wire itself.
		{"WebhookSubscription", service.Subscription{}},
		{"CreatedWebhookSubscription", service.CreatedSubscription{}},
		{"WebhookDelivery", service.Delivery{}},
		{"SCIMCredential", service.SCIMCredential{}},
		{"IssuedSCIMCredential", service.IssuedSCIMCredential{}},
		{"Settings", service.Settings{}},
		{"ImportResult", service.ImportResult{}},
		{"BulkResult", service.BulkResult{}},
		{"GroupMember", model.GroupMember{}},
		{"GroupRef", model.GroupRef{}},
		// model.Session is the sign-in shown to its owner. The document's
		// `Session` is the login response — a token and a user — which is
		// the service layer's, not this one's.
		{"UserSession", model.Session{}},
		{"AuditLog", model.AuditLog{}},
	} {
		t.Run(subject.schema, func(t *testing.T) {
			described, present := schemas[subject.schema]
			if !present {
				t.Fatalf("%s names no schema called %s", specPath, subject.schema)
			}
			sent := serializedFields(subject.value)

			for name := range sent {
				if !described[name] {
					t.Errorf("%s sends %s and the %s schema does not describe "+
						"it; it does not exist for anybody generating a client",
						subject.schema, name, subject.schema)
				}
			}
			for name := range described {
				if !sent[name] {
					t.Errorf("the %s schema describes %s, which no response "+
						"carries; a generated client gets a property that is "+
						"always empty", subject.schema, name)
				}
			}
		})
	}
}

// serializedFields is the set of names a struct puts on the wire. A field
// tagged `json:"-"` puts none, which is the case worth catching.
func serializedFields(value any) map[string]bool {
	names := map[string]bool{}
	collectFields(reflect.TypeOf(value), names)
	return names
}

func collectFields(structType reflect.Type, names map[string]bool) {
	for i := range structType.NumField() {
		field := structType.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		// An embedded struct with no tag is flattened by encoding/json, and
		// the schema flattens it too — through allOf. `IssuedSCIMCredential`
		// is a credential plus a token, in both descriptions.
		if field.Anonymous && tag == "" && field.Type.Kind() == reflect.Struct {
			collectFields(field.Type, names)
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			// No tag: encoding/json uses the field name as written.
			name = field.Name
		}
		names[name] = true
	}
}

func specSchemas(t *testing.T) map[string]map[string]bool {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]schemaNode `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}

	out := map[string]map[string]bool{}
	for name := range doc.Components.Schemas {
		out[name] = resolveProperties(t, doc.Components.Schemas, name, map[string]bool{})
	}
	return out
}

// schemaNode is as much of a schema object as this test reads: its own
// properties, and the allOf composition the document uses to say "everything
// that one has, plus these".
type schemaNode struct {
	Properties map[string]yaml.Node `yaml:"properties"`
	AllOf      []struct {
		Ref        string               `yaml:"$ref"`
		Properties map[string]yaml.Node `yaml:"properties"`
	} `yaml:"allOf"`
}

func resolveProperties(t *testing.T, schemas map[string]schemaNode, name string, seen map[string]bool) map[string]bool {
	t.Helper()

	properties := map[string]bool{}
	if seen[name] {
		t.Errorf("the %s schema composes itself through allOf", name)
		return properties
	}
	seen[name] = true

	schema := schemas[name]
	for property := range schema.Properties {
		properties[property] = true
	}
	for _, part := range schema.AllOf {
		for property := range part.Properties {
			properties[property] = true
		}
		if part.Ref == "" {
			continue
		}
		referenced := strings.TrimPrefix(part.Ref, "#/components/schemas/")
		if _, known := schemas[referenced]; !known {
			t.Errorf("the %s schema composes %s, which is not defined", name, part.Ref)
			continue
		}
		for property := range resolveProperties(t, schemas, referenced, seen) {
			properties[property] = true
		}
	}
	return properties
}
