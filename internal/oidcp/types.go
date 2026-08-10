package oidcp

import (
	"crypto/rand"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// cryptoRead is crypto/rand.Read, named so the token helper reads clearly.
var cryptoRead = rand.Read

// authRequest adapts a stored row to op.AuthRequest.
//
// It is a thin projection with no behaviour of its own: every value came out
// of the database exactly as it went in, and anything the protocol library
// derives from it is the library's business.
type authRequest struct{ row sqlcgen.OauthAuthRequest }

func (a *authRequest) GetID() string  { return a.row.ID }
func (a *authRequest) GetACR() string { return "" }

func (a *authRequest) GetAMR() []string { return a.row.Amr }

func (a *authRequest) GetAudience() []string {
	// The subject is always in the audience: an ID token is about the person
	// and for the client, and relying parties check both.
	audience := append([]string{}, a.row.Audience...)
	return append(audience, a.row.ClientID)
}

func (a *authRequest) GetAuthTime() time.Time {
	if a.row.AuthTime == nil {
		return time.Time{}
	}
	return *a.row.AuthTime
}

func (a *authRequest) GetClientID() string { return a.row.ClientID }

func (a *authRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if a.row.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{
		Challenge: a.row.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(a.row.CodeChallengeMethod),
	}
}

func (a *authRequest) GetNonce() string       { return a.row.Nonce }
func (a *authRequest) GetRedirectURI() string { return a.row.RedirectUri }

func (a *authRequest) GetResponseType() oidc.ResponseType {
	return oidc.ResponseType(a.row.ResponseType)
}

func (a *authRequest) GetResponseMode() oidc.ResponseMode {
	return oidc.ResponseMode(a.row.ResponseMode)
}

func (a *authRequest) GetScopes() []string { return a.row.Scopes }
func (a *authRequest) GetState() string    { return a.row.State }

func (a *authRequest) GetSubject() string {
	if a.row.Subject == nil {
		return ""
	}
	return *a.row.Subject
}

// Done reports whether somebody has signed in. Until they have, the request
// cannot be exchanged for anything.
func (a *authRequest) Done() bool { return a.row.Done }

// refreshRequest adapts a stored refresh token to op.RefreshTokenRequest.
type refreshRequest struct {
	row      sqlcgen.OauthRefreshToken
	clientID string
	token    string
}

func (r *refreshRequest) GetAMR() []string      { return r.row.Amr }
func (r *refreshRequest) GetAudience() []string { return r.row.Audience }
func (r *refreshRequest) GetAuthTime() time.Time {
	return r.row.AuthTime
}
func (r *refreshRequest) GetClientID() string { return r.clientID }
func (r *refreshRequest) GetScopes() []string { return r.row.Scopes }
func (r *refreshRequest) GetSubject() string  { return r.row.Subject }

// SetCurrentScopes narrows what a refresh may ask for. A client may drop
// scopes on refresh but never add them, and the library enforces that
// before calling this.
func (r *refreshRequest) SetCurrentScopes(scopes []string) { r.row.Scopes = scopes }

// client adapts a registered relying party to op.Client.
type client struct {
	model    model.OAuthClient
	loginURL func(authRequestID string) string
	// idTokenLifetime is the tenant's setting as it stood when this client was
	// looked up. Held as a value because op.Client's accessor takes no context
	// and returns no error; a fresh client is built for every request, so this
	// is a per-request read rather than a snapshot taken at startup.
	idTokenLifetime time.Duration
}

func (c *client) GetID() string          { return c.model.ClientID }
func (c *client) RedirectURIs() []string { return c.model.RedirectURIs }

func (c *client) PostLogoutRedirectURIs() []string { return c.model.PostLogoutRedirectURIs }

func (c *client) ApplicationType() op.ApplicationType {
	switch c.model.ApplicationType {
	case model.AppTypeNative:
		return op.ApplicationTypeNative
	case model.AppTypeUserAgent:
		return op.ApplicationTypeUserAgent
	default:
		return op.ApplicationTypeWeb
	}
}

func (c *client) AuthMethod() oidc.AuthMethod {
	switch c.model.AuthMethod {
	case model.AuthMethodNone:
		return oidc.AuthMethodNone
	case model.AuthMethodPost:
		return oidc.AuthMethodPost
	default:
		return oidc.AuthMethodBasic
	}
}

// ResponseTypes is only "code". The implicit and hybrid flows put tokens in
// URLs, which is why OAuth 2.1 removes them; offering them would mean
// supporting the thing the version number exists to retire.
func (c *client) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}

func (c *client) GrantTypes() []oidc.GrantType {
	types := make([]oidc.GrantType, 0, len(c.model.GrantTypes))
	for _, t := range c.model.GrantTypes {
		types = append(types, oidc.GrantType(t))
	}
	return types
}

// LoginURL is where the browser is sent to sign in. The protocol library
// redirects there and expects to be handed the request back, authorized.
func (c *client) LoginURL(authRequestID string) string { return c.loginURL(authRequestID) }

// AccessTokenType is a JWT, not an opaque handle. The point of federating is
// that a resource server can verify a token without calling back here, which
// an opaque token forecloses.
func (c *client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeJWT }

// IDTokenLifetime matches the access token's, deliberately: an ID token that
// outlived the access token it arrived with would describe an authentication
// that may already have been withdrawn.
func (c *client) IDTokenLifetime() time.Duration { return c.idTokenLifetime }

// DevMode is never on. It relaxes redirect-URI checking, and there is no
// deployment where that is worth a flag somebody can leave set.
func (c *client) DevMode() bool { return false }

//nolint:revive // the name is op.Client's, not ours.
func (c *client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

// IsScopeAllowed checks a requested scope against what this client was
// registered for. A client asking for something it was not granted gets that
// scope dropped rather than an error, which is what the specification calls
// for.
func (c *client) IsScopeAllowed(scope string) bool {
	for _, allowed := range c.model.Scopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

// IDTokenUserinfoClaimsAssertion puts the profile claims in the ID token as
// well as behind userinfo, so a relying party that only wants a name does not
// need a second round trip.
func (c *client) IDTokenUserinfoClaimsAssertion() bool { return true }

// ClockSkew is the tolerance for a relying party whose clock is off. A
// minute covers ordinary drift; more would extend the life of every token by
// the same amount.
func (c *client) ClockSkew() time.Duration { return time.Minute }
