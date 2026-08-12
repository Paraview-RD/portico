package oidcrp

import (
	"context"
	"strings"
	"testing"
)

// Where an issuer may point is the security question this package answers
// before it answers any other.
//
// A tenant administrator types the URL and this server fetches it. That is
// the same shape as a webhook destination and the same attacker, so it is
// the same policy — and this test exists to make sure the check is actually
// reached rather than to re-test the policy, which has its own tests.
func TestAnIssuerInsideTheNetworkIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	for _, issuer := range []string{
		"http://accounts.example.com",              // not TLS
		"https://127.0.0.1:8443",                   // loopback
		"https://10.1.2.3/oidc",                    // private
		"https://169.254.169.254/latest/meta-data", // the metadata service
		"https://[::1]/oidc",                       // loopback, the other way
	} {
		_, err := Discover(context.Background(), Config{
			Issuer: issuer, ClientID: "portico", RedirectURI: "https://portico.example.com/cb",
		})
		if err == nil {
			t.Errorf("Discover accepted %q; a tenant administrator could then "+
				"make this server fetch addresses inside its own network", issuer)
			continue
		}
		// It must fail on the address, not on being unreachable — otherwise a
		// deployment where that address happens to answer would proceed.
		if !strings.Contains(err.Error(), "not an acceptable address") {
			t.Errorf("Discover(%q) failed with %q; that reads as a network "+
				"problem rather than a refusal, and the refusal is the point",
				issuer, err)
		}
	}
}

func TestTheOpenIDScopeIsAddedRatherThanAssumed(t *testing.T) {
	// Without it a provider returns no ID token, and the failure surfaces at
	// the callback as a token that cannot be read — a long way from the form
	// where somebody left it out.
	got := withOpenID([]string{"profile", "email"})
	if got[0] != "openid" {
		t.Errorf("scopes = %v, want openid first", got)
	}

	// And not twice: a duplicated scope is refused outright by some
	// providers and silently accepted by others, which is the worst pair.
	got = withOpenID([]string{"openid", "profile"})
	count := 0
	for _, scope := range got {
		if scope == "openid" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("scopes = %v, want openid exactly once", got)
	}
}
