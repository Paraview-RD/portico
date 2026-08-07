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
		// Configuration is parsed before anything else exists.
		"config": {"handler", "service", "store", "auth", "httpx", "server"},
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
