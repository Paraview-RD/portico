package server_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// One Chinese word per concept, and it is the console's word.
//
// The manual and the console are the same product to whoever is reading
// them, and the console is where a term is met first — so where they
// disagreed, the manual was wrong. It disagreed with itself as well:
// federation.zh.md had 访问令牌 in one table row and ID token in the next,
// and "这些在 ID token、访问令牌和 userinfo 响应里" in a single sentence.
// Nothing was checking, so nothing said so.
//
// This is the fourth check of its shape in this repository — after the
// example DSN port, the bootstrap password, and the seeded password. The
// pattern each time: a fact written in more than one place, with nothing
// obliging the copies to agree.
//
// What it cannot do is worth stating, because the last one taught it. A
// check like this holds that the same *word* is used; it cannot hold that
// the sentence around the word is true. Five stale descriptions of the
// bootstrap password survived the check that reads that password, because
// they either named no password or named the right one two paragraphs above
// the wrong claim.

// termsPath is the table this reads. Keeping the list in the document rather
// than here is deliberate: the document is what a person changes when they
// decide a term, and a list in the test would be a second place to forget.
const termsPath = "../../docs/i18n-conventions.md"

const consoleBundlePath = "../../web/src/i18n/zh-CN.ts"

// glossaryRow matches a row of the table under "One Chinese word per
// concept" — English concept, Chinese term, and where it is settled.
var glossaryRow = regexp.MustCompile(`(?m)^\| ([a-z][a-zA-Z ]+|ID token) \| ([^|]+?) \| `)

func glossary(t *testing.T) map[string]string {
	t.Helper()

	content, err := os.ReadFile(termsPath)
	if err != nil {
		t.Fatalf("read %s: %v", termsPath, err)
	}

	terms := map[string]string{}
	for _, row := range glossaryRow.FindAllStringSubmatch(string(content), -1) {
		terms[row[1]] = strings.TrimSpace(row[2])
	}

	// Without this the table could be renamed, emptied, or reformatted and
	// this file would go on passing while checking nothing.
	if len(terms) < 3 {
		t.Fatalf("found %d terms in %s; the table has moved or changed shape, "+
			"and this check is now vacuous", len(terms), termsPath)
	}
	return terms
}

func TestTheConsoleUsesTheChineseTermsTheGlossaryNames(t *testing.T) {
	bundle, err := os.ReadFile(consoleBundlePath)
	if err != nil {
		t.Fatalf("read %s: %v", consoleBundlePath, err)
	}
	text := string(bundle)

	for english, chinese := range glossary(t) {
		if !strings.Contains(text, chinese) {
			t.Errorf("the glossary settles %q as %q, and the console does not use "+
				"that word anywhere.\nThe console is the authority here, so either "+
				"it changed and the glossary did not, or the term was never the "+
				"console's to begin with", english, chinese)
		}
	}
}

func TestTheDocumentsUseTheConsolesChineseTerms(t *testing.T) {
	terms := glossary(t)

	pages, err := filepath.Glob("../../docs/*.zh.md")
	if err != nil || len(pages) == 0 {
		t.Fatalf("no Chinese documentation found: %v", err)
	}

	for _, page := range pages {
		content, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		prose := withoutCode(string(content))

		for english, chinese := range terms {
			// The gloss a document is allowed once: 访问令牌（access token）.
			// Removed before looking, so what is left is bare English.
			bare := strings.ReplaceAll(prose, chinese+"（"+english+"）", "")
			if !strings.Contains(bare, english) {
				continue
			}
			t.Errorf("%s writes %q where the console says %q.\n"+
				"Give the English once, at the term's first appearance, as "+
				"%s（%s） — after that it is Chinese. See %s.",
				filepath.Base(page), english, chinese, chinese, english, termsPath)
		}
	}
}

// withoutCode removes fenced blocks and backticked spans.
//
// Not tidiness: `refresh_token` is a grant type and a JSON field, and the
// ASCII diagram in field-mappings.zh.md labels its columns in English
// because they are protocol names. A check that read those would be asking
// the manual to describe a request nobody can send.
func withoutCode(markdown string) string {
	fenced := regexp.MustCompile("(?s)```.*?```")
	inline := regexp.MustCompile("`[^`\n]*`")
	return inline.ReplaceAllString(fenced.ReplaceAllString(markdown, ""), "")
}
