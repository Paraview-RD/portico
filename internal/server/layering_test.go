package server_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// The layering described in docs/code-conventions.md is only real if
// something enforces it. Go rejects import cycles on its own, but it will
// happily let a handler reach straight into the store, which is how business
// rules end up duplicated across endpoints and enforced in only some of
// them.
func TestLayeringRules(t *testing.T) {
	const module = "github.com/paraview/portico/"

	forbidden := map[string][]string{
		// Handlers go through a service. Always.
		"handler": {"store"},
		// A service that needs something from the HTTP layer means the thing
		// belongs in model, or should be a parameter.
		"service": {"handler", "server"},
		// The leaves stay leaves: that is what keeps them testable in
		// isolation and stops business changes rippling into them.
		"model": {"handler", "service", "store", "auth", "httpx", "server"},
		"httpx": {"handler", "service", "store", "auth", "server"},
		"store": {"handler", "service", "auth", "server"},
		// Authentication is consumed by services, so it must not depend on
		// them — that is why auth.UserLookup is declared in auth.
		"auth": {"handler", "service", "server"},
		// Configuration is parsed before anything else exists. It may name
		// notify's config type, which is why that package has to stay a leaf.
		"config": {"handler", "service", "store", "auth", "httpx", "server"},
		// Delivery knows nothing about this application. That is what lets a
		// test substitute a recorder for it, and what would let someone add
		// an SMS provider without reading anything else.
		"notify": {"handler", "service", "store", "auth", "httpx", "server", "config", "model"},
		// The OpenID Provider adapter is glue between the protocol library and
		// the service layer. It must not reach into the web stack: an endpoint
		// it needed from there would mean the protocol was leaking into
		// Portico's own API surface rather than sitting beside it.
		"oidcp": {"handler", "httpx", "server", "config"},
		// The SAML adapter is glue on the same terms as oidcp: it must not
		// reach into the web stack, because an endpoint it needed from there
		// would mean the protocol was leaking into Portico's own API surface
		// rather than sitting beside it.
		"samlp": {"handler", "httpx", "server", "config"},
		// And the CAS server, which is implemented directly rather than
		// through a library but sits in exactly the same place.
		"casp": {"handler", "httpx", "server", "config"},
		// Measurement must not depend on what it measures. The service layer
		// records into this package, so anything it reached back for would be
		// an import cycle at best — and at worst a metric that cannot be
		// recorded from a test without standing up half the application.
		"metrics": {"handler", "service", "store", "auth", "httpx", "server", "config", "model"},
		// Provisioning is a second composition root, for the operations that
		// have no HTTP surface. It builds services directly and must not
		// reach for the web stack: anything it needed from there would mean
		// the rule belongs in a service, where the API can reach it too.
		"provision": {"handler", "httpx", "server"},
	}

	for pkg, banned := range forbidden {
		t.Run(pkg, func(t *testing.T) {
			dir := filepath.Join("..", pkg)
			imported, err := build.ImportDir(dir, 0)
			if err != nil {
				t.Fatalf("read package %s: %v", pkg, err)
			}

			for _, imp := range imported.Imports {
				if !strings.HasPrefix(imp, module) {
					continue
				}
				rel := strings.TrimPrefix(imp, module+"internal/")
				// Only compare the first path segment, so a subpackage such
				// as store/sqlcgen is attributed to store.
				top, _, _ := strings.Cut(rel, "/")

				for _, bad := range banned {
					if top == bad {
						t.Errorf("%s imports %s, which the layering forbids "+
							"(see docs/code-conventions.md)", pkg, imp)
					}
				}
			}
		})
	}
}
