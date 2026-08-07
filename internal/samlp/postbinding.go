package samlp

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
)

// The HTTP-POST binding is an HTML page that posts a form to somebody
// else's origin and submits it with an inline script. Both of those are
// exactly what this application's Content-Security-Policy forbids
// everywhere else, and rightly so — so the policy for this one response is
// written here rather than inherited.
//
// Without it, every SAML sign-in fails in a browser and passes in every
// test, because a test client does not enforce CSP. That is worth stating
// plainly: the eleven tests that drive the whole flow all passed while this
// was broken, and a browser found it in one attempt.

// submitScript is the whole of the inline script. It is a constant because
// the policy below carries its hash, and a script the policy does not name
// is a script the browser will not run — so the two must come from the same
// place rather than be kept in step by hand.
const submitScript = "document.forms[0].submit()"

// postFormTemplate is Portico's own rather than the protocol library's
// default, for the same reason: the library's script could change in a patch
// release and take SAML sign-in down with it.
var postFormTemplate = template.Must(template.New("saml-post-form").Parse(
	`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<title>Signing you in…</title></head><body>` +
		`<form method="post" action="{{.URL}}">` +
		`<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}">` +
		`<input type="hidden" name="RelayState" value="{{.RelayState}}">` +
		`<noscript><p>Your browser does not run scripts. ` +
		`Continue to finish signing in.</p>` +
		`<input type="submit" value="Continue"></noscript>` +
		`</form><script>` + submitScript + `</script></body></html>`))

// scriptHash is the CSP source expression naming the script above.
var scriptHash = func() string {
	sum := sha256.Sum256([]byte(submitScript))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

// postBindingCSP is the policy for that one page.
//
// Everything is denied except the two things the binding is: a form posting
// to the assertion consumer service the request named — which the protocol
// library resolved out of the registered metadata, so it is not an address
// a caller chose — and the one script that submits it, by hash rather than
// by 'unsafe-inline', which would permit any script on the page.
func postBindingCSP(acsURL string) string {
	return "default-src 'none'; " +
		"script-src " + scriptHash + "; " +
		"form-action " + acsURL + "; " +
		"base-uri 'none'; frame-ancestors 'none'"
}

// applyPostBindingHeaders replaces the response headers for a POST-binding
// page. The middleware has already set the application's own policy, which
// would block this page entirely.
func applyPostBindingHeaders(w http.ResponseWriter, acsURL string) {
	h := w.Header()
	h.Set("Content-Security-Policy", postBindingCSP(acsURL))
	h.Set("Content-Type", "text/html; charset=utf-8")
	// The page carries an assertion about somebody. It is single use and
	// short lived, but there is no reason for it to sit in a cache.
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
}
