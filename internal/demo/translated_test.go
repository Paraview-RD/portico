package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The packs are named in Go and read in a browser, and nothing in either
// language connects the two.
//
// A pack is offered by key. The form asks the server which keys exist and
// renders each one through `trial.industry.<key>` — so a pack added here
// without a line in both locale files appears in the picker as its own key.
// That is not a crash and no other test sees it: the tenant is created
// correctly, and the only symptom is a visitor choosing "manufacturing" from a
// list of English words.
//
// This is the same arrangement as the other documents this repository pins to
// the thing they describe. It reads the locale files as text rather than
// parsing TypeScript, which is enough: the question is whether a line exists,
// not what it says.
func TestEveryPackIsNamedInBothLocales(t *testing.T) {
	root := repoRoot(t)

	for _, locale := range []string{"zh-CN", "en-US"} {
		path := filepath.Join(root, "web", "src", "i18n", locale+".ts")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(source)

		for _, p := range packs {
			key := fmt.Sprintf("%q", "trial.industry."+p.Key)
			if !strings.Contains(text, key) {
				t.Errorf("%s has no %s. The trial form renders each offered industry "+
					"through that key, so this pack would appear in the picker under its "+
					"own name in a language nobody chose.", locale, key)
			}
		}
	}
}

// repoRoot walks up from this package until it finds the module file.
func repoRoot(t *testing.T) string {
	t.Helper()

	at, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(at, "go.mod")); err == nil {
			return at
		}
		parent := filepath.Dir(at)
		if parent == at {
			t.Fatal("no go.mod above this package; cannot find the locale files")
		}
		at = parent
	}
}
