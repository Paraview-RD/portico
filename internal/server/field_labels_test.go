package server_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/service"
)

// Every built-in field has something a person can read.
//
// A built-in carries no stored label on purpose: it has to read the same in
// both languages and a database string can only be one of them, so the console
// holds them under `fields.<key>`. That arrangement has one failure mode, and
// it is the same one the error codes had — a feature adds keys to the
// catalogue and to neither bundle. The type system will not see it, because
// the Chinese bundle is typed against the English one and both are missing the
// same key. What a person sees is `organization_parent_code` where a label
// should be.
//
// This lives in Go for the same reason the error-code guard does: Go is where
// the keys are written. The bundles are read as text rather than as modules.

const (
	englishBundlePath = "../../web/src/i18n/en-US.ts"
	chineseBundlePath = "../../web/src/i18n/zh-CN.ts"
	fieldDocPath      = "../../docs/field-mappings.md"
)

var fieldLabelKey = regexp.MustCompile(`"fields\.([a-z][a-z0-9_]*)":`)

func labelledFields(t *testing.T, path string) map[string]bool {
	t.Helper()

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	labelled := map[string]bool{}
	for _, match := range fieldLabelKey.FindAllStringSubmatch(string(source), -1) {
		labelled[match[1]] = true
	}
	return labelled
}

// Both bundles carry a label for every built-in key.
//
// Both, not one, because the English half passing on its own is exactly the
// state that hides this: the fallback for a missing key is the key itself,
// which looks like a slightly technical label in English and like nothing at
// all in Chinese.
func TestEveryBuiltInFieldHasALabelInBothLanguages(t *testing.T) {
	for _, bundle := range []struct{ language, path string }{
		{"English", englishBundlePath},
		{"Chinese", chineseBundlePath},
	} {
		labelled := labelledFields(t, bundle.path)

		var missing []string
		for _, field := range service.BuiltInFields() {
			if !labelled[field.Key] {
				missing = append(missing, field.Key)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("these built-in fields have no `fields.<key>` entry in the %s "+
				"bundle (%s), so the mapping picker draws the key itself:\n  %s",
				bundle.language, bundle.path, strings.Join(missing, "\n  "))
		}
	}
}

// And no label for a field that no longer exists.
//
// A stale entry is not merely dead weight: `fields.<key>` is how the picker
// asks for a label, so an entry nothing asks for is a claim that a field
// exists. Somebody reading the bundle to find out what can be mapped would be
// reading a list that is wrong.
func TestNoLabelDescribesAFieldThatIsGone(t *testing.T) {
	known := map[string]bool{}
	for _, field := range service.BuiltInFields() {
		known[field.Key] = true
	}

	for _, path := range []string{englishBundlePath, chineseBundlePath} {
		var stale []string
		for key := range labelledFields(t, path) {
			if !known[key] {
				stale = append(stale, key)
			}
		}
		if len(stale) > 0 {
			sort.Strings(stale)
			t.Errorf("%s has labels for fields the catalogue no longer holds: %s",
				path, strings.Join(stale, ", "))
		}
	}
}

// The two bundles agree on which fields they label.
//
// Covered by the two tests above for the built-in set, and asserted directly
// so that a failure says which side is ahead rather than listing the same
// keys twice.
func TestTheTwoBundlesLabelTheSameFields(t *testing.T) {
	english := labelledFields(t, englishBundlePath)
	chinese := labelledFields(t, chineseBundlePath)

	for key := range english {
		if !chinese[key] {
			t.Errorf("%q has an English label and no Chinese one", key)
		}
	}
	for key := range chinese {
		if !english[key] {
			t.Errorf("%q has a Chinese label and no English one", key)
		}
	}
}

// The documentation names the catalogue rather than listing it.
//
// A page that listed forty-one keys would be a second copy of the catalogue,
// and the copy is what goes stale — which is why field-mappings.md points at
// `GET /api/v1/fields` instead. What this checks is that the pointer is still
// there and still points somewhere: a page that quietly stopped saying where
// the list is leaves an integrator with no way to find it.
func TestTheDocumentationSaysWhereTheFieldListIs(t *testing.T) {
	page, err := os.ReadFile(fieldDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", fieldDocPath, err)
	}
	if !strings.Contains(string(page), "/api/v1/fields") {
		t.Errorf("%s no longer says where the catalogue can be read. It deliberately "+
			"does not list the keys — a second copy is the one that goes stale — "+
			"so without the pointer there is nothing left.", fieldDocPath)
	}
}
