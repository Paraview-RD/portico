// Package mailfmt describes a transactional email once and renders it twice:
// as plain text, and as the HTML that most people will actually see.
//
// One description rather than two bodies, because two bodies drift. A change
// made to the HTML and forgotten in the text is not a formatting bug — the
// text part is what a screen reader, a terminal client, and a spam filter
// read, and what arrives when the HTML is stripped.
//
// What this is for, beyond looking better. A generated password rendered in a
// proportional font is genuinely ambiguous: 0 and O, l and 1, are the same
// few pixels, and somebody has to type it. Plain text cannot ask for a
// monospace font — text/plain carries no formatting, and a client renders it
// in whatever it likes, which today is usually proportional. That is also why
// the text form here does not try to line values up in a column: an
// attempted table that does not line up looks broken, and a plain list does
// not.
//
// Deliberately small. No images, no external stylesheet, no web fonts, no
// tracking pixel, no layout tables: an identity server's mail is short, it is
// asked for, and every one of those would be a way to be classified as
// marketing or to leak that a message was opened.
package mailfmt

import (
	"bytes"
	"html/template"
	"strings"
)

// Document is one email.
type Document struct {
	// Title is the first line, and the only heading.
	Title string
	// Intro is the paragraphs before anything structured.
	Intro []string
	// Sections carry the parts somebody acts on.
	Sections []Section
	// Footer is the small print: what to do if this was not you, and what
	// this deployment is.
	Footer []string
}

// Section is a heading with any of a paragraph, some facts, and one action.
type Section struct {
	// Heading may be empty, for a section that is only an action.
	Heading string
	Body    []string
	Facts   []Fact
	Action  *Action
}

// Fact is one labelled value.
type Fact struct {
	Label string
	Value string
	// Code marks a value that has to be read character by character — a
	// password, a code, an address. It is what earns the monospace font,
	// which is the whole reason this package renders HTML at all.
	Code bool
}

// Action is the one thing this message wants somebody to open.
//
// A pointer on Section because most sections have none, and a button that
// leads nowhere is worse than no button.
type Action struct {
	Label string
	URL   string
}

// Text renders the plain-text part.
//
// Values are never aligned into columns. A column assembled with spaces is a
// column only in a monospace font, and the client that shows this will
// probably choose a proportional one — which turns a tidy table into a ragged
// one, and reads as a fault rather than as a list.
func (d Document) Text() string {
	var b strings.Builder

	if d.Title != "" {
		b.WriteString(d.Title + "\n")
	}
	for _, paragraph := range d.Intro {
		b.WriteString("\n" + paragraph + "\n")
	}

	for _, section := range d.Sections {
		b.WriteString("\n")
		if section.Heading != "" {
			// No rule under it. A row of dashes as wide as the heading is
			// only as wide as the heading in a monospace font — and in
			// Chinese not even then, since one character is two columns and
			// one rune. Underlining a heading with something that does not
			// reach the end of it looks worse than not underlining it, and it
			// would be the same mistake as the column alignment this dropped.
			// A blank line above is what separates a section here.
			b.WriteString(section.Heading + "\n")
		}
		for _, paragraph := range section.Body {
			b.WriteString(paragraph + "\n")
		}
		if len(section.Facts) > 0 {
			if len(section.Body) > 0 {
				b.WriteString("\n")
			}
			for _, fact := range section.Facts {
				b.WriteString("  " + fact.Label + ": " + fact.Value + "\n")
			}
		}
		if section.Action != nil {
			b.WriteString("\n  " + section.Action.URL + "\n")
		}
	}

	for _, paragraph := range d.Footer {
		b.WriteString("\n" + paragraph + "\n")
	}
	return b.String()
}

// HTML renders the marked-up part.
//
// Every style is inline. A <style> block is stripped by enough clients that
// relying on one means designing for the strippers anyway, and a message
// whose formatting is optional should not have two designs.
//
// Nothing is coloured for a dark theme either. The clients that invert do it
// to the whole message, and a white card with dark text inverts cleanly;
// a hand-made dark palette is what inverts into something unreadable.
func (d Document) HTML() (string, error) {
	var out bytes.Buffer
	if err := htmlTemplate.Execute(&out, d); err != nil {
		return "", err
	}
	return out.String(), nil
}

// html/template rather than a string built by hand, and not for tidiness:
// a tenant's name reaches these messages from a form a stranger filled in,
// so every value here is attacker-supplied until something escapes it. This
// package is where that happens.
var htmlTemplate = template.Must(template.New("mail").Parse(`<!doctype html>
<html>
<head>
<!-- The charset is declared here as well as in the Content-Type header the
     transport sets. Not redundant: a client that renders the part on its own
     — a preview pane, a saved .html, a webmail that reframes the body — has
     only this, and without it an em dash arrives as two replacement
     characters in the middle of a sentence. Which is exactly what it did. -->
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
</head>
<body style="margin:0;padding:24px;background:#f1f5fb;
  font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif;
  font-size:15px;line-height:1.6;color:#1e2430;">
<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e3e8f0;border-radius:10px;padding:28px;">
{{if .Title}}<h1 style="margin:0 0 16px;font-size:19px;line-height:1.4;color:#1e2430;">{{.Title}}</h1>{{end}}
{{range .Intro}}<p style="margin:0 0 14px;">{{.}}</p>{{end}}
{{range .Sections}}
<div style="margin-top:22px;">
{{if .Heading}}<h2 style="margin:0 0 10px;font-size:13px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:#5b6577;">{{.Heading}}</h2>{{end}}
{{range .Body}}<p style="margin:0 0 12px;">{{.}}</p>{{end}}
{{if .Facts}}<table cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;">
{{range .Facts}}<tr>
<td style="padding:5px 12px 5px 0;color:#5b6577;white-space:nowrap;vertical-align:top;">{{.Label}}</td>
<td style="padding:5px 0;vertical-align:top;{{if .Code}}font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:14px;background:#f5f7fb;border-radius:4px;padding-left:8px;padding-right:8px;{{end}}word-break:break-all;">{{.Value}}</td>
</tr>{{end}}
</table>{{end}}
{{with .Action}}<p style="margin:16px 0 0;">
<a href="{{.URL}}" style="display:inline-block;padding:10px 18px;background:#2563eb;color:#ffffff;text-decoration:none;border-radius:6px;font-weight:600;">{{.Label}}</a>
</p>
<p style="margin:10px 0 0;font-size:13px;color:#5b6577;word-break:break-all;">{{.URL}}</p>{{end}}
</div>
{{end}}
{{if .Footer}}<div style="margin-top:24px;padding-top:16px;border-top:1px solid #e3e8f0;font-size:13px;color:#5b6577;">
{{range .Footer}}<p style="margin:0 0 8px;">{{.}}</p>{{end}}
</div>{{end}}
</div>
</body></html>
`))
