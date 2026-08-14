// Package socialrp signs people in through providers that are not OpenID
// Connect.
//
// It is the sibling of internal/oidcrp and exists for one reason: OpenID
// Connect's discovery document is what makes a provider configurable rather
// than programmed. Give oidcrp an issuer and it learns the endpoints, the
// keys, and how to validate what comes back. WeChat and DingTalk publish no
// such document, so each needs its endpoints, its parameter names, its error
// convention and its notion of a subject written down here.
//
// That is the whole distinction, and it is worth keeping visible: adding a
// provider to oidcrp is a row in a table, and adding one here is a file.
//
// # What these two do not have
//
// Neither issues an ID token, so nothing in either exchange is signed and
// there is no nonce to compare. Neither implements PKCE. What ties a
// callback to a request this server started is therefore the single-use
// `state` alone — where an OIDC provider is held to three separate checks.
//
// This is not a weakness introduced here; it is what those providers offer,
// and the reason the token exchange must happen server-to-server over TLS
// with the client secret. The browser is never trusted with anything but the
// authorization code.
package socialrp

import "context"

// Identity is what a provider says about a person.
//
// Deliberately the same shape as oidcrp.Identity rather than a shared type
// in a third package: the two are alike today by coincidence of what
// identity is, and a common struct would make one provider's new field
// everybody's problem. The service converts.
type Identity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

// Provider is a thing somebody can be sent to and come back from.
//
// nonce and codeVerifier appear in both signatures and are ignored by every
// implementation in this package. They are kept so that one interface covers
// these and oidcrp's Party, which does use them — the alternative is two
// call paths through the sign-in service, and the second one is where a
// check gets forgotten.
type Provider interface {
	// Issuer namespaces this provider's subjects. A subject is unique only
	// within its issuer, so the pair is the identity.
	Issuer() string
	// AuthURL is where the browser goes.
	AuthURL(state, nonce, codeVerifier string) string
	// Exchange spends the authorization code, server to server.
	Exchange(ctx context.Context, code, codeVerifier, nonce string) (Identity, error)
}

// Kinds this package implements, as stored in
// external_identity_providers.kind.
const (
	KindWeChat   = "WECHAT"
	KindDingTalk = "DINGTALK"
)

// Issuer returns the fixed issuer for a kind, or "" if this package does not
// implement it.
//
// The issuer is a constant rather than something an administrator types: it
// is not an address anything is fetched from, it is the namespace the
// subjects live in, and letting it be typed would let two tenants disagree
// about what "WeChat" means.
func Issuer(kind string) string {
	switch kind {
	case KindWeChat:
		return weChatIssuer
	case KindDingTalk:
		return dingTalkIssuer
	default:
		return ""
	}
}

// SupportsVerifiedEmail says whether a kind can ever produce an address good
// enough to link a first sign-in by.
//
// WeChat returns no address at all, so the "trust verified addresses"
// setting can do nothing for it and the console does not offer it. DingTalk
// returns one and does not claim it is proved, which this package reports as
// unverified — so the switch is offered, and turning it on is the
// administrator saying they trust their own DingTalk organization.
func SupportsVerifiedEmail(kind string) bool {
	return kind != KindWeChat
}
