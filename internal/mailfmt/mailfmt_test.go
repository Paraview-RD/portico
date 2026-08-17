package mailfmt

import (
	"strings"
	"testing"
)

func sample() Document {
	return Document{
		Title: "Your Portico trial is ready.",
		Intro: []string{"Everything below is what you sign in with."},
		Sections: []Section{{
			Heading: "Sign in",
			Facts: []Fact{
				{Label: "Tenant", Value: "mytrial", Code: true},
				{Label: "Username", Value: "admin", Code: true},
				{Label: "Password", Value: "P0T5zLoMbaVwccd8XKb8H7my", Code: true},
			},
			Action: &Action{Label: "Open the console", URL: "https://demo.example.com/login?tenant=mytrial"},
		}},
		Footer: []string{"This is a demonstration."},
	}
}

// Both parts carry everything. A message whose HTML says more than its text
// is a message that loses information the moment a client strips the markup —
// and the text part is what a screen reader and a terminal client read.
func TestNothingIsInOnePartOnly(t *testing.T) {
	doc := sample()
	text := doc.Text()
	html, err := doc.HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}

	for _, needed := range []string{
		"Your Portico trial is ready.",
		"Everything below is what you sign in with.",
		"mytrial", "admin", "P0T5zLoMbaVwccd8XKb8H7my",
		"https://demo.example.com/login?tenant=mytrial",
		"This is a demonstration.",
	} {
		if !strings.Contains(text, needed) {
			t.Errorf("the text part is missing %q", needed)
		}
		if !strings.Contains(html, needed) {
			t.Errorf("the html part is missing %q", needed)
		}
	}
}

// The address is in the HTML as text as well as in the href.
//
// A button is not a link somebody can read, copy, or trust: a client that
// refuses to render the anchor, a person who wants to see where it goes
// before opening it, and anybody forwarding the message to somebody without
// HTML all need the address written out.
func TestTheAddressIsReadableAndNotOnlyClickable(t *testing.T) {
	html, err := sample().HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	url := "https://demo.example.com/login?tenant=mytrial"
	if strings.Count(html, url) < 2 {
		t.Errorf("the address appears %d time(s); it should be both the href "+
			"and text somebody can read:\n%s", strings.Count(html, url), html)
	}
}

// A value marked Code is set in a monospace font.
//
// This is the reason this package renders HTML at all. A generated password
// in a proportional font is ambiguous — 0 and O, l and 1 — and somebody has
// to type it. text/plain carries no formatting and cannot ask.
func TestAPasswordIsSetInAFontYouCanReadItIn(t *testing.T) {
	html, err := sample().HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}

	password := "P0T5zLoMbaVwccd8XKb8H7my"
	at := strings.Index(html, password)
	if at < 0 {
		t.Fatal("the password is not in the html at all")
	}
	// The cell it sits in, back to the tag that opens it.
	cell := html[strings.LastIndex(html[:at], "<td"):at]
	if !strings.Contains(cell, "monospace") {
		t.Errorf("the password is not in a monospace cell:\n%s", cell)
	}
}

// Values are escaped, which is not a nicety here.
//
// A tenant's name arrives from a form a stranger filled in and travels
// straight into this message. Building the HTML by hand would put whatever
// they typed into the markup; html/template is what makes that a string
// again, and this is the test that keeps somebody from replacing it with a
// concatenation that reads more simply.
func TestWhatSomebodyTypedStaysText(t *testing.T) {
	doc := Document{
		Title: `Trial for <script>alert("x")</script>`,
		Sections: []Section{{
			Facts: []Fact{{Label: "Tenant", Value: `"><img src=x onerror=alert(1)>`}},
		}},
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}

	// Tag openings are what matter: `onerror=` sitting inside escaped text is
	// a word, not an attribute, because the < that would start a tag is gone.
	for _, injected := range []string{"<script", "<img"} {
		if strings.Contains(html, injected) {
			t.Errorf("%q reached the markup unescaped:\n%s", injected, html)
		}
	}
	// And it is still readable, rather than dropped.
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("the value was not escaped so much as lost")
	}
}

// An address that is not one cannot become an attribute either.
//
// The one place a value lands inside markup rather than between tags is the
// href, where escaping is not enough — `javascript:` is a valid URL and a
// valid attribute. html/template refuses it; this is the test that notices if
// somebody swaps the template for a formatted string.
func TestALinkThatIsNotAnAddressDoesNotBecomeOne(t *testing.T) {
	doc := Document{Sections: []Section{{
		Action: &Action{Label: "Open", URL: "javascript:alert(1)"},
	}}}
	html, err := doc.HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if strings.Contains(html, "href=\"javascript:") {
		t.Errorf("a javascript: URL survived into an href:\n%s", html)
	}
}

// The text part is text.
func TestTheTextPartCarriesNoMarkup(t *testing.T) {
	text := sample().Text()
	for _, tag := range []string{"<html", "<div", "<a ", "<td", "&lt;", "&amp;"} {
		if strings.Contains(text, tag) {
			t.Errorf("the text part contains %q:\n%s", tag, text)
		}
	}
}

// Nothing is aligned into a column.
//
// A column built from spaces is a column only in a monospace font, and the
// client rendering this will usually pick a proportional one — which turns a
// tidy table into a ragged one and reads as a fault rather than as a list.
// This is what the trial mail used to do, and the screenshot that started
// this work is what it looks like when it does not line up.
func TestTheTextPartDoesNotPretendToBeATable(t *testing.T) {
	text := sample().Text()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.TrimSpace(line), ":  ") {
			t.Errorf("this line pads a value into a column, which only lines "+
				"up in a monospace font:\n\t%q", line)
		}
	}
}

// An empty document renders rather than failing, since a message with no
// action or no footer is an ordinary message rather than a broken one.
func TestThePartsAreAllOptional(t *testing.T) {
	doc := Document{Title: "Only a title."}
	if text := doc.Text(); !strings.Contains(text, "Only a title.") {
		t.Errorf("text: %q", text)
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if !strings.Contains(html, "Only a title.") {
		t.Errorf("html: %s", html)
	}
}
