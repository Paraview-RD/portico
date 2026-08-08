package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
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
	api := newAPITest(t)

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
