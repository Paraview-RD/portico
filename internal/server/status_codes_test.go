package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// An endpoint that resolves a tenant from the request says so in the
// document.
//
// Nine public endpoints can answer `TENANT_NOT_FOUND` (404) or
// `TENANT_DISABLED` (403), because they have no token to read the tenant from
// and take it from the body, a header, or a query parameter instead. None of
// them declared either, so a client generated from the specification had no
// branch for the two failures a multi-tenant deployment produces most often —
// a typo in a tenant code, and a tenant somebody turned off.
//
// They were found by hand, by listing the callers of `resolvePublicTenant`.
// Nothing stopped the tenth from being added without the same two lines, so
// this makes that list mechanical: the routing table says which handler
// serves each path, the handler's source says whether it resolves a tenant,
// and the document has to agree.
//
// The limit worth knowing: it looks for a direct call. A handler that
// resolved a tenant through some helper of its own would not be seen, and
// the check would silently pass. There is no such helper today, and adding
// one should mean revisiting this.
func TestEveryTenantResolvingEndpointDeclaresBothRefusals(t *testing.T) {
	resolvers := handlersResolvingATenant(t)
	if len(resolvers) == 0 {
		t.Fatal("no handler appears to resolve a tenant, which means this " +
			"test has stopped reading the source rather than that the code changed")
	}

	described := describedResponses(t)

	api := newAPITest(t)
	router, ok := api.srv.Handler().(chi.Routes)
	if !ok {
		t.Fatal("the server's handler is not a chi router; this test walks its route tree")
	}

	var checked []string
	err := chi.Walk(router, func(method, route string, handler http.Handler,
		_ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, specRoot) {
			return nil
		}
		name := handlerMethodName(handler)
		if !resolvers[name] {
			return nil
		}

		path := normalize(route)
		checked = append(checked, method+" "+path)

		codes, known := described[operation{Method: method, Path: path}]
		if !known {
			// Route-versus-document is the other test's subject; here it
			// would only produce a second copy of the same failure.
			return nil
		}
		for _, code := range []string{"403", "404"} {
			if !codes[code] {
				t.Errorf("%s %s resolves a tenant from the request and the "+
					"document does not declare %s; a client generated from it "+
					"has no branch for %s", method, path, code, refusalFor(code))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	sort.Strings(checked)
	t.Logf("%d endpoints resolve a tenant from the request: %s",
		len(checked), strings.Join(checked, ", "))
}

// describedResponses is the set of status codes the document declares for
// each operation.
func describedResponses(t *testing.T) map[operation]map[string]bool {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Responses map[string]yaml.Node `yaml:"responses"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}

	out := map[operation]map[string]bool{}
	for path, item := range doc.Paths {
		for method, op := range item {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}
			codes := map[string]bool{}
			for code := range op.Responses {
				codes[code] = true
			}
			out[operation{Method: strings.ToUpper(method), Path: normalize(path)}] = codes
		}
	}
	return out
}

func refusalFor(code string) string {
	if code == "404" {
		return "a tenant code that does not exist"
	}
	return "a tenant somebody has turned off"
}

// handlersResolvingATenant reads the handler package and returns the methods
// that call resolvePublicTenant.
//
// Syntactic, deliberately: this is a question about which functions name a
// function, and type information would buy nothing a selector name does not
// already give.
func handlersResolvingATenant(t *testing.T) map[string]bool {
	t.Helper()

	const pkg = "../handler"
	entries, err := os.ReadDir(pkg)
	if err != nil {
		t.Fatalf("read %s: %v", pkg, err)
	}

	found := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(pkg, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "resolvePublicTenant" {
					return true
				}
				found[fn.Name.Name] = true
				return false
			})
		}
	}
	return found
}

// handlerMethodName is the method a route is served by, as
// `(*Handler).Login`, taken from the function value the router holds.
func handlerMethodName(handler http.Handler) string {
	full := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	// e.g. github.com/…/internal/handler.(*Handler).Login-fm
	name := full
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, "-fm")
}
