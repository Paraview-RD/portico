package model

import "time"

// SigningAlgorithm is what ID tokens are signed with.
//
// One algorithm, not a choice. RS256 is the only one every OpenID relying
// party is required to support, and offering a menu here would mean each
// tenant's discovery document advertising something different for no benefit
// anyone asked for.
const SigningAlgorithm = "RS256"

// OAuth client application types (§3.2).
const (
	// AppTypeWeb is a server-side application, which can hold a secret.
	AppTypeWeb = "WEB"
	// AppTypeNative is a desktop or mobile application, which cannot.
	AppTypeNative = "NATIVE"
	// AppTypeUserAgent is a browser application, which cannot either.
	AppTypeUserAgent = "USER_AGENT"
)

// Client authentication methods.
const (
	// AuthMethodNone is a public client: no secret, PKCE only.
	AuthMethodNone = "none"
	// AuthMethodBasic sends the secret in the Authorization header.
	AuthMethodBasic = "client_secret_basic"
	// AuthMethodPost sends it in the form body.
	AuthMethodPost = "client_secret_post"
)

// Lifetimes for the tokens this server issues as an OpenID Provider.
//
// The access token is deliberately short. It is verified offline by a
// resource server that never calls back here, so revoking it is not possible
// — the only control over how long a withdrawn permission keeps working is
// how soon the token expires and the refresh has to come back. Fifteen
// minutes is the usual answer and the one documented in SECURITY.md.
const (
	AccessTokenLifetime  = 15 * time.Minute
	IDTokenLifetime      = 15 * time.Minute
	RefreshTokenLifetime = 30 * 24 * time.Hour
	// AuthRequestLifetime bounds how long a sign-in may take, and with it
	// how long an authorization code is worth intercepting.
	AuthRequestLifetime = 15 * time.Minute
)

// OAuthClient is a registered relying party.
type OAuthClient struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	ClientID string `json:"clientId"`
	Name     string `json:"name"`

	// Confidential reports whether the client holds a secret. A public
	// client is not a lesser one — a browser application genuinely cannot
	// keep a secret, and pretending otherwise is how secrets end up in
	// JavaScript bundles.
	Confidential bool `json:"confidential"`

	ApplicationType string `json:"applicationType"`
	AuthMethod      string `json:"authMethod"`

	// LaunchURL is where a person opens this application, for the portal.
	// Not a redirect URI: that is where a code is delivered mid-flow, and
	// opening it directly produces an error rather than the application.
	// Empty when nobody supplied one, which is allowed.
	LaunchURL              string   `json:"launchUrl"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	GrantTypes             []string `json:"grantTypes"`
	Scopes                 []string `json:"scopes"`

	Status Status `json:"status"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
