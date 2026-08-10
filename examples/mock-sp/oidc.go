package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/client/rp"
	httphelper "github.com/zitadel/oidc/v3/pkg/http"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

// The callback path is a constant because it appears in three places that
// have to agree exactly: the URI registered in Portico, the one this program
// hands to the library, and the route it is served on. Portico matches
// redirect URIs as strings, so a disagreement in any of the three is an
// invalid_request at the end of an otherwise successful sign-in.
const oidcCallbackPath = "/oidc/callback"

type oidcParty struct {
	party  rp.RelyingParty
	issuer string
}

// newOIDC runs discovery against the issuer and prepares the relying party.
//
// Discovery is the part worth watching: everything the flow needs — the
// authorization and token endpoints, the key set an ID token's signature is
// checked against — comes from one document at a well-known URL. If the
// issuer is wrong, or PORTICO_PUBLIC_URL does not match the address the
// browser uses, it fails here rather than three redirects later.
func newOIDC(issuer, clientID, clientSecret, redirectURI string, scopes []string) (*oidcParty, error) {
	// These cookies carry the state and the PKCE verifier across the few
	// seconds between leaving for the sign-in screen and coming back.
	//
	// WithUnsecure because a demonstration runs over plain http on
	// localhost, and a browser does not return a Secure cookie over that.
	// Without it the callback finds no state, and the error reads like a
	// protocol failure rather than like a cookie that was never sent.
	cookies := httphelper.NewCookieHandler(randomKey(16), randomKey(16), httphelper.WithUnsecure())

	party, err := rp.NewRelyingPartyOIDC(context.Background(),
		issuer, clientID, clientSecret, redirectURI, scopes,
		// Portico implements OAuth 2.1, which requires PKCE of every client
		// — a confidential one included. Without this the authorization
		// request carries no code_challenge and is refused outright.
		rp.WithPKCE(cookies),
		rp.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, errorType, errorDesc, _ string) {
			page(w, http.StatusBadRequest, "oidcerror", map[string]string{
				"Type":        errorType,
				"Description": errorDesc,
			})
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("discovery against %s: %w", issuer, err)
	}
	return &oidcParty{party: party, issuer: issuer}, nil
}

func (o *oidcParty) mount(mux *http.ServeMux) {
	// Two handlers and no flow logic of our own. AuthURLHandler builds the
	// authorization request — state, PKCE challenge, scopes — and redirects;
	// CodeExchangeHandler checks the state, exchanges the code, verifies the
	// ID token against the discovered key set, and hands over the result.
	mux.Handle("/oidc", rp.AuthURLHandler(uuid.NewString, o.party))
	mux.Handle(oidcCallbackPath,
		rp.CodeExchangeHandler(rp.UserinfoCallback(o.signedIn), o.party))
}

// signedInView is what the page at the end of the flow shows.
type signedInView struct {
	Issuer       string
	Subject      string
	Audience     string
	Expiration   string
	AccessToken  string
	TokenExpiry  string
	RefreshToken string
	IDTokenJSON  string
	UserInfoJSON string
}

// signedIn renders the result of a completed sign-in.
//
// Two sources are shown side by side deliberately. The ID token is what
// Portico asserted at the moment of sign-in, signed; the userinfo response
// is what it says about the same person now, fetched with the access token.
// An integrator who confuses the two writes an application that never
// notices a changed name — or one that calls userinfo on every request.
func (o *oidcParty) signedIn(
	w http.ResponseWriter, _ *http.Request,
	tokens *oidc.Tokens[*oidc.IDTokenClaims], _ string,
	_ rp.RelyingParty, info *oidc.UserInfo,
) {
	claims := tokens.IDTokenClaims

	refresh := "none — ask for the offline_access scope, and register the client with it"
	if tokens.RefreshToken != "" {
		refresh = abbreviate(tokens.RefreshToken)
	}

	page(w, http.StatusOK, "signedin", signedInView{
		Issuer:     claims.GetIssuer(),
		Subject:    claims.GetSubject(),
		Audience:   strings.Join(claims.GetAudience(), ", "),
		Expiration: claims.GetExpiration().Format(time.RFC3339),
		// Abbreviated rather than printed in full: a demonstration is often
		// on a screen somebody else is looking at, and an access token is a
		// credential for as long as it lives.
		AccessToken:  abbreviate(tokens.AccessToken),
		TokenExpiry:  tokens.Expiry.Format(time.RFC3339),
		RefreshToken: refresh,
		IDTokenJSON:  prettyJSON(claims),
		UserInfoJSON: prettyJSON(info),
	})
}

func abbreviate(token string) string {
	const shown = 24
	if len(token) <= shown {
		return token
	}
	return token[:shown] + "… (" + fmt.Sprint(len(token)) + " characters)"
}

func prettyJSON(v any) string {
	encoded, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("could not be rendered: %v", err)
	}
	return string(encoded)
}
