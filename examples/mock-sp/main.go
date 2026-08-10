// Command mock-sp signs in through Portico so that a person can watch it
// happen.
//
// internal/server/federation_test.go already proves the protocols work, but
// it proves it to a test runner. This proves it to whoever is in the room: a
// browser, Portico's own sign-in screen, and a page at the end showing what
// actually came back. Use it to demonstrate single sign-on, and to check a
// deployment before handing its details to somebody who has to integrate
// against it.
//
// It is deliberately not production code, and not a library. Nothing is
// stored beyond a SAML key, the sign-in leaves no session behind, and
// signing in twice starts over both times. What is worth copying out of it
// is which library calls appear in which order, not the program around them.
//
//	go run ./examples/mock-sp
//
// All three protocols are set up independently, and one that fails to start
// takes only its own page down. A wrong SAML certificate should not be able
// to stop a demonstration of OpenID Connect.
package main

import (
	"bytes"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// repeatable collects a flag that may be given more than once.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, " ") }

func (r *repeatable) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// protocolCard is one protocol's entry on the home page, and whether it is
// in a state to be clicked.
//
// A protocol that could not be set up stays on the page and says why. The
// alternative — dropping it — leaves somebody wondering whether Portico
// supports it at all, which is the question this tool exists to answer.
type protocolCard struct {
	Name  string
	Path  string
	Blurb string
	Err   string
}

func (c *protocolCard) Ready() bool { return c.Err == "" }

func main() {
	// Loopback by default, for the reason deploy/docker-compose.yml binds
	// Portico the same way: these pages render an access token and
	// everything an assertion claims about somebody, over plain http. On a
	// laptop on a conference network, the default must not be something the
	// room can reach.
	addr := flag.String("addr", "127.0.0.1:8413", "address to listen on")
	baseURL := flag.String("base-url", "", "where a browser reaches this program (defaults to http://localhost<addr>)")
	issuer := flag.String("issuer", "http://localhost:8410",
		"the Portico deployment to sign in against; add /t/<code> for a tenant other than the default")
	clientID := flag.String("client-id", "mock-sp", "the OpenID Connect client_id registered in Portico")
	clientSecret := flag.String("client-secret", "",
		"the client secret, for a confidential client; empty means the client is public")
	stateDir := flag.String("state-dir", ".mock-sp",
		"where the SAML key, certificate, and metadata document are kept between runs")

	// The default matches what `portico client register` grants when no
	// --scope is given, so the two halves of the walkthrough agree without
	// anybody having to notice. Asking for a scope the client was not
	// registered with is refused, and the refusal is easy to misread as the
	// sign-in itself having failed.
	scopes := repeatable{}
	flag.Var(&scopes, "scope", "a scope to request (repeatable; defaults to openid profile email)")

	flag.Parse()

	if len(scopes) == 0 {
		scopes = repeatable{"openid", "profile", "email"}
	}

	base := *baseURL
	if base == "" {
		base = defaultBaseURL(*addr)
	}
	portico := strings.TrimSuffix(*issuer, "/")

	mux := http.NewServeMux()
	mux.HandleFunc("/", home)

	cards := []*protocolCard{
		{Name: "OpenID Connect", Path: "/oidc",
			Blurb: "Authorization code with PKCE, then the ID token and userinfo."},
		{Name: "SAML 2.0", Path: "/saml",
			Blurb: "A signed assertion, posted back by the browser."},
		{Name: "CAS 3.0", Path: "/cas",
			Blurb: "A one-minute ticket, validated from this server."},
	}

	// Each protocol is set up on its own and reports its own failure. This
	// is why the three are not a loop over a common interface: what each one
	// needs to start is different, and pretending otherwise would put the
	// differences somewhere less obvious than here.
	if party, err := newOIDC(portico, *clientID, *clientSecret, base+oidcCallbackPath, scopes); err != nil {
		cards[0].Err = err.Error()
	} else {
		party.mount(mux)
	}

	saml, err := newSAML(portico, base, *stateDir)
	if err != nil {
		cards[1].Err = err.Error()
	} else {
		saml.mount(mux)
	}

	if party, err := newCAS(portico, base); err != nil {
		cards[2].Err = err.Error()
	} else {
		party.mount(mux)
	}

	for _, card := range cards {
		if !card.Ready() {
			mux.HandleFunc(card.Path, brokenHandler(card))
		}
	}
	homeCards = cards

	announce(os.Stderr, *addr, base, portico, *clientID, clientKind(*clientSecret),
		strings.Join(scopes, " "), saml, cards)

	// An explicit server rather than http.ListenAndServe, for the header
	// timeout: without one a connection that opens and never finishes its
	// request holds a goroutine indefinitely.
	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("mock-sp: %v", err)
	}
}

// homeCards is what the home page renders.
//
// Package-level because the home handler is registered before the protocols
// are set up — the alternative is a closure built after them, which puts the
// route table in two places for no gain in a program this size.
var homeCards []*protocolCard

func announce(w *os.File, addr, base, portico, clientID, kind, scopes string,
	saml *samlParty, cards []*protocolCard) {
	_, _ = fmt.Fprintf(w, `mock-sp is listening on %s

  Open           %s
  Portico        %s
  OIDC client    %s (%s)
  OIDC scopes    %s
  Redirect URI   %s

The redirect URI above must be registered in Portico exactly as written —
it is matched as a string, so a different host spelling or port is a
different URI.
`, addr, base, portico, clientID, kind, scopes, base+oidcCallbackPath)

	// Each line is printed on its own condition. Gating both on SAML would
	// hide the CAS command whenever SAML failed to start, which is exactly
	// the coupling the separate set-up above exists to avoid.
	if saml != nil {
		_, _ = fmt.Fprintf(w, `
Register this service provider:

  portico sp register --metadata %s --name "Mock SP"
`, saml.metadataPath)
	}
	if cards[2].Ready() {
		_, _ = fmt.Fprintf(w, `
Register this CAS service:

  portico cas register --url %s --name "Mock SP"
`, base+casPrefix)
	}
	_, _ = fmt.Fprintln(w)

	for _, card := range cards {
		if !card.Ready() {
			_, _ = fmt.Fprintf(w, "%s is not available: %s\n", card.Name, card.Err)
		}
	}
}

// defaultBaseURL turns a listen address into the URL a browser would use.
//
// Any loopback address becomes localhost, which is not cosmetic. The
// redirect URI is derived from this, and Portico matches redirect URIs as
// strings: binding to 127.0.0.1 while the documentation says to register
// localhost would fail on the last redirect of an otherwise successful
// sign-in, with an error about the URI rather than about the two spellings.
// One spelling, chosen here, and the whole walkthrough agrees with itself.
func defaultBaseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "localhost" {
		return "http://localhost:" + port
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "http://localhost:" + port
	}
	return "http://" + addr
}

func clientKind(secret string) string {
	if secret == "" {
		return "public, PKCE only"
	}
	return "confidential"
}

// randomKey returns bytes for the cookie handler's hash and encryption keys.
//
// Generated per run on purpose. These protect the state and PKCE verifier
// cookies for the few seconds a sign-in takes; a fixed key in an example
// program is a key that ends up copied into something that matters.
func randomKey(n int) []byte {
	key := make([]byte, n)
	if _, err := rand.Read(key); err != nil {
		log.Fatalf("mock-sp: generate a cookie key: %v", err)
	}
	return key
}

func home(w http.ResponseWriter, r *http.Request) {
	// ServeMux's "/" matches everything unmatched, so an unknown path lands
	// here. Say so, rather than showing the home page under the wrong URL.
	if r.URL.Path != "/" {
		page(w, http.StatusNotFound, "notfound", r.URL.Path)
		return
	}
	page(w, http.StatusOK, "home", homeCards)
}

// brokenHandler renders why a protocol could not be set up.
func brokenHandler(card *protocolCard) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		page(w, http.StatusServiceUnavailable, "broken", card)
	}
}

// page renders a template to the browser.
//
// It renders into a buffer first so that a template failure can still be
// reported as an error: writing directly would have already sent 200 and
// part of a page by the time anything went wrong, and a demonstration that
// half-renders is harder to diagnose than one that says what broke.
func page(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "mock-sp: render "+name+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
