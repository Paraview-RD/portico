package server_test

import (
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The layering described in docs/code-conventions.md is only real if
// something enforces it. Go rejects import cycles on its own, but it will
// happily let a handler reach straight into the store, which is how business
// rules end up duplicated across endpoints and enforced in only some of
// them.
func TestLayeringRules(t *testing.T) {
	const module = "github.com/Paraview-RD/portico/"

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
		// Seeding is a third composition root and belongs on the same terms
		// as provision. It reaches the services and the store — the store
		// because history has to be written at a chosen time, which no service
		// will do — and must not reach the web stack: an endpoint it needed
		// from there would mean the seed was exercising the API rather than
		// filling the database the API reads.
		"seed": {"handler", "httpx", "server"},
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

// Both layout diagrams name every package, and name nothing else.
//
// The table above reads itself, which the document says leaves one direction
// free to drift: a package added to the tree and to neither diagram. That is
// what happened. `directory`, `scim`, `webhook`, `secrets`, `i18n`, and
// several more were absent from one or both, and README named a `middleware`
// package that had not existed for some time — a reader looking for the
// authentication middleware would have gone hunting for a directory rather
// than opening `auth`.
//
// A diagram that lists most of the packages is worse than none, because it
// reads as complete. So both directions are checked: every package appears,
// and everything that appears is a package.
func TestBothLayoutDiagramsNameEveryPackage(t *testing.T) {
	onDisk := packagesOnDisk(t)

	for _, doc := range []string{"../../README.md", "../../docs/code-conventions.md"} {
		t.Run(filepath.Base(doc), func(t *testing.T) {
			listed := layoutDiagram(t, doc)

			for _, pkg := range onDisk {
				if !listed[pkg] {
					t.Errorf("internal/%s exists and %s does not list it; "+
						"the diagram reads as the whole of it", pkg, doc)
				}
			}
			for pkg := range listed {
				if !slicesContains(onDisk, pkg) {
					t.Errorf("%s lists internal/%s, which does not exist; "+
						"somebody will go looking for it", doc, pkg)
				}
			}
		})
	}
}

// packagesOnDisk is every directory directly under internal/ that holds Go
// source. Subpackages such as store/sqlcgen are deliberately not included:
// the diagrams describe the top level, and mention the generated one in
// prose beside its parent.
func packagesOnDisk(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("read internal/: %v", err)
	}

	var packages []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join("..", entry.Name()))
		if err != nil {
			t.Fatalf("read internal/%s: %v", entry.Name(), err)
		}
		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".go") {
				packages = append(packages, entry.Name())
				break
			}
		}
	}
	sort.Strings(packages)
	return packages
}

// layoutDiagram reads the fenced block holding the source tree and returns
// the names indented under `internal/`. Indentation is what distinguishes
// them from the top-level entries beside it — `docs/` and `web/` are both a
// package and a directory at the root, and only one of those is meant here.
func layoutDiagram(t *testing.T, path string) map[string]bool {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var tree, block []string
	inside := false
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "```") {
			if inside && tree == nil && holdsTree(block) {
				tree = block
			}
			inside, block = !inside, nil
			continue
		}
		if inside {
			block = append(block, line)
		}
	}
	if tree == nil {
		t.Fatalf("%s has no fenced block containing an internal/ tree", path)
	}

	entry := regexp.MustCompile(`^ {2,}([a-z][a-z0-9]*)/`)
	names := map[string]bool{}
	for _, line := range tree {
		if match := entry.FindStringSubmatch(line); match != nil {
			names[match[1]] = true
		}
	}
	return names
}

func holdsTree(block []string) bool {
	for _, line := range block {
		if strings.HasPrefix(line, "internal/") {
			return true
		}
	}
	return false
}

func slicesContains(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}
