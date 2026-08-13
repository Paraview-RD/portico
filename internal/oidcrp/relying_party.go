// Package oidcrp signs people in through somebody else's OpenID Provider.
//
// This is the mirror of internal/oidcp and shares nothing with it. There,
// Portico is the issuer and other applications decide whether to trust what
// it says. Here it is the relying party: the assertions arrive from outside,
// and everything that matters is in what happens to them on the way in.
//
// The protocol work — discovery, the code exchange, and validating an ID
// token against a key set — is the library's rather than this file's. That
// is deliberate and is the most important decision in this package. A
// mistake in ID token validation is not a bug that produces a wrong answer;
// it is an authentication bypass, and the ones that have happened in public
// are subtle: an `iss` that was never compared, an `aud` that accepted any
// audience, an alg that was taken from the token. The repository already
// trusts github.com/zitadel/oidc for the issuing half, and this uses the
// same module's relying-party half rather than hand-rolling the checks.
package oidcrp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/Paraview-RD/portico/internal/webhook"
)

// DiscoveryTimeout bounds the round trip to somebody else's server.
//
// Short, because it runs inside an administrator's request while they wait
// for a form to save, and a provider that cannot answer in ten seconds is a
// provider a sign-in would time out against anyway.
const DiscoveryTimeout = 10 * time.Second

// Config is what an operator supplies for one provider.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	// Scopes as configured. `openid` is added here rather than trusted to
	// be present: without it the provider returns no ID token at all, and
	// the failure appears at the callback as a token that cannot be read.
	Scopes []string
}

// Party is a configured relying party, ready to send somebody out and take
// them back.
type Party struct {
	rp     rp.RelyingParty
	issuer string
}

// Discover reads a provider's configuration and prepares to use it.
//
// The issuer is checked against the same destination rules webhook
// deliveries follow, before anything is fetched. The threat is identical
// and so is the attacker: a tenant administrator types a URL and this
// server fetches it, so an unchecked one turns Portico into a way to probe
// the network it runs in. Reusing that policy rather than writing a second
// one also means a rule added there — a new reserved range, a new
// refusal — is a rule this obeys without anybody remembering to.
func Discover(ctx context.Context, cfg Config) (*Party, error) {
	issuer := strings.TrimSpace(cfg.Issuer)
	if err := webhook.ValidateDestination(issuer); err != nil {
		return nil, fmt.Errorf("issuer is not an acceptable address: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()

	party, err := rp.NewRelyingPartyOIDC(ctx,
		issuer, cfg.ClientID, cfg.ClientSecret, cfg.RedirectURI,
		withOpenID(cfg.Scopes),
		// The same client the deliveries use: it re-checks the address at
		// connection time, so a hostname that resolved publicly during
		// discovery and privately a moment later does not get through.
		rp.WithHTTPClient(webhook.NewClient(DiscoveryTimeout)),
		// Proof key, from what the provider says it supports. A public
		// client would need it; a confidential one is better with it, and
		// this asks for it wherever the other side advertises it.
		rp.WithPKCE(nil),
	)
	if err != nil {
		return nil, fmt.Errorf("read the provider's configuration: %w", err)
	}
	return &Party{rp: party, issuer: issuer}, nil
}

// withOpenID guarantees the one scope the protocol is named after.
func withOpenID(scopes []string) []string {
	out := make([]string, 0, len(scopes)+1)
	seen := false
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if scope == oidc.ScopeOpenID {
			seen = true
		}
		out = append(out, scope)
	}
	if !seen {
		out = append([]string{oidc.ScopeOpenID}, out...)
	}
	return out
}

// Identity is what came back, reduced to what Portico will act on.
//
// Deliberately narrow. A provider may return a great deal about somebody,
// and everything kept here is something that has to be defended later — so
// this is the subject, which is the identity, and an address and name that
// are shown to a person deciding whether to bind, and never used to find an
// account.
type Identity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

// AuthURL is where to send somebody.
//
// state and nonce are the caller's to generate and to remember: state is
// what makes the callback answerable — a callback arriving with a state
// nobody issued is a forged one — and nonce is what ties the ID token to
// this request rather than to one somebody replayed.
func (p *Party) AuthURL(state, nonce, codeVerifier string) string {
	return rp.AuthURL(state, p.rp,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(codeVerifier)),
		// The nonce goes on as a plain authorization-code option: the
		// library has a helper for the challenge and none for this, and it
		// is a parameter rather than a feature.
		func() []oauth2.AuthCodeOption {
			return []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("nonce", nonce)}
		},
	)
}

// Exchange turns the code from a callback into an identity.
//
// The ID token's signature, issuer, audience and expiry are checked by the
// library against the key set it discovered. The nonce is checked here, on
// the value the caller stored when it issued the request: a token that does
// not carry it is a token from some other exchange.
func (p *Party) Exchange(ctx context.Context, code, codeVerifier, nonce string) (Identity, error) {
	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](ctx, code, p.rp,
		rp.WithCodeVerifier(codeVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	if tokens.IDTokenClaims == nil {
		return Identity{}, fmt.Errorf("the provider returned no ID token; " +
			"the `openid` scope may have been refused")
	}

	claims := tokens.IDTokenClaims
	if claims.Nonce != nonce {
		// Not a wrapped error: this is the check that a token was minted for
		// the request that is being answered, and it fails identically
		// whether somebody replayed one or a provider dropped the value.
		return Identity{}, fmt.Errorf("the ID token does not answer this sign-in")
	}
	if claims.Subject == "" {
		return Identity{}, fmt.Errorf("the ID token names no subject")
	}

	return Identity{
		Issuer:        p.issuer,
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: bool(claims.EmailVerified),
		DisplayName:   strings.TrimSpace(claims.Name),
	}, nil
}
