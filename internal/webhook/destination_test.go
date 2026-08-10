package webhook_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/webhook"
)

// The destination rules, which are the security of this feature.
//
// Portico runs inside a network and the URL comes from whoever administers a
// tenant. Every case below is a way of asking this server to make a request
// somewhere the person asking could not reach themselves.

func TestDestinationsThatWouldTurnThisIntoAProxyAreRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		why  string
	}{
		{
			"cloud metadata",
			"https://169.254.169.254/latest/meta-data/iam/security-credentials/",
			"the address that hands out cloud credentials to anything that asks",
		},
		{
			"metadata by another name",
			"https://169.254.170.2/v2/credentials",
			"the same link-local range, a different well-known endpoint",
		},
		{"loopback", "https://127.0.0.1:5432/", "the database this process is authenticated to"},
		{"loopback by name", "https://localhost/hook", "the same, spelled differently"},
		{"ipv6 loopback", "https://[::1]/hook", "and again"},
		{"private 10", "https://10.0.0.5/hook", "the internal network"},
		{"private 172", "https://172.16.4.4/hook", "the internal network"},
		{"private 192.168", "https://192.168.1.1/hook", "somebody's router"},
		{"carrier-grade NAT", "https://100.64.0.1/hook", "RFC 6598, which IsPrivate does not cover"},
		{"unspecified", "https://0.0.0.0/hook", "resolves to something local"},
		{"plain http", "http://example.com/hook", "the payload and its signature would be readable in transit"},
		{"credentials in the url", "https://user:pass@example.com/hook", "would be stored and logged"},
		{"not a url", "://nonsense", "cannot be called at all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := webhook.ValidateDestination(tt.url)
			if !errors.Is(err, webhook.ErrDestinationNotAllowed) {
				t.Errorf("%s was accepted (%v) — %s", tt.url, err, tt.why)
			}
		})
	}
}

func TestAnOrdinaryPublicDestinationIsAccepted(t *testing.T) {
	t.Parallel()

	// The rules have to leave the actual use case working, which is the half
	// a deny-list gets wrong: a check strict enough to refuse everything is
	// not a check, it is an outage.
	if err := webhook.ValidateDestination("https://hooks.example.com/portico"); err != nil {
		t.Errorf("a public https destination was refused: %v", err)
	}
}

// The dialer is the check that survives DNS rebinding: a name that resolved
// publicly at registration and resolves to 127.0.0.1 by the time anything is
// sent. Nothing in the URL says so, so the address itself has to be checked
// at the moment of connection.
func TestTheClientRefusesToConnectToALocalAddressWhateverTheNameSaid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// server.URL is a loopback address, which stands in for a name that has
	// started resolving to one. Validation is deliberately not called here:
	// this is the case where validation already passed.
	client := webhook.NewClient(5 * time.Second)
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the client connected to a loopback address. A destination " +
			"validated at registration can resolve somewhere else by the time " +
			"anything is delivered, so the dialer is what has to refuse it.")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	t.Parallel()

	// A destination that answers with a redirect would otherwise walk the
	// request somewhere its owner never named — the classic escape from any
	// check performed on the registered URL.
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	client := webhook.NewClient(5 * time.Second)
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// Not actually sent — the assertion is on the client's configuration,
	// which is what decides this before any network is involved.
	if client.CheckRedirect == nil {
		t.Fatal("the client follows redirects")
	}
	if err := client.CheckRedirect(req, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("redirects are followed: %v", err)
	}
	if redirected {
		t.Error("unexpected request to the redirect target")
	}
}
