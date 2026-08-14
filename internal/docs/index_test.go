package docs_test

// docs/README.md is the index somebody sees when they open the docs
// directory on GitHub, and it is the one file in this repository that goes
// stale by omission rather than by contradiction. Nothing about adding a
// page makes anybody remember it, and a page missing from it is not wrong —
// it is invisible, which is worse, because the index reads as complete.
//
// It had drifted before this test existed: six pages were missing, three of
// them for months. That is the shape of failure this guards, and it is the
// same one internal/server/layering_test.go guards for packages — a diagram
// or a list that claims to be the whole of something has to be checked
// against the thing.
//
// Not the same list as the manual's own navigation in mkdocs.yml. That one
// is the built site's, for readers, and deliberately leaves out the
// conventions and the requirements. This one is the directory's, for
// somebody browsing the repository, and includes them. Two audiences, two
// lists, and each drifts on its own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notInTheIndex is every docs/*.md that docs/README.md does not list, with
// the reason. Adding a page means adding it to the index or to this map.
var notInTheIndex = map[string]string{
	"README.md": "is the index",
	// The manual's own front page. It is what mkdocs builds as the home
	// page and is reached by opening the manual rather than by being linked
	// from a list of chapters.
	"index.md": "the manual's home page, not a chapter of it",
}

func TestEveryDocumentIsInTheIndexOrSaysWhyNot(t *testing.T) {
	const dir = "../../docs"

	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}
	listed := string(index)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the documentation directory: %v", err)
	}

	var checked int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		// Translations are not separate entries. The index links the English
		// page and the page links its own translation, so listing both would
		// double every line to say the same thing twice.
		if strings.HasSuffix(name, ".zh.md") {
			continue
		}
		if reason, ok := notInTheIndex[name]; ok {
			if reason == "" {
				t.Errorf("%s is exempt with no reason given", name)
			}
			continue
		}

		checked++
		// Matched on the link target rather than on the filename appearing
		// anywhere: a page mentioned in prose is not a page somebody can
		// find from the index.
		if !strings.Contains(listed, "("+name+")") {
			t.Errorf("docs/%s exists and docs/README.md does not link it. "+
				"The index reads as the whole of this directory, so a page "+
				"missing from it is not merely unlisted — it is invisible. "+
				"Add it there, or to notInTheIndex with the reason.", name)
		}
	}

	if checked == 0 {
		t.Fatal("no pages were checked, so this asserted nothing")
	}
	t.Logf("%d pages checked against the index", checked)
}
