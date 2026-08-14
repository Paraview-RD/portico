package socialrp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// WeChat, through the Open Platform's website application — the QR code
// somebody scans with the phone in their hand.
//
// Of the three things called "WeChat login" this is the one a browser-based
// console can use. The other two are not alternatives to it:
//
//   - The official-account web authorization only works inside WeChat's own
//     browser, so it cannot sign anybody in to a desktop console.
//   - WeCom is a different product with a different API and a different
//     notion of who a person is.
//
// This is OAuth 2 with three deliberate departures from it, and each one is
// why the OIDC relying party could not simply be pointed at WeChat:
//
//  1. The client credentials are called `appid` and `secret` and are sent as
//     query parameters, not in an Authorization header.
//  2. There is no ID token. Identity comes from a second call, and there is
//     nothing signed anywhere in the exchange — which is why the token
//     request must go server-to-server over TLS and the result must never be
//     taken from the browser.
//  3. Failure is reported with HTTP 200 and an `errcode` in the body. A
//     client that checks the status code concludes every failure succeeded.
const (
	weChatIssuer  = "https://open.weixin.qq.com"
	weChatAuthURL = "https://open.weixin.qq.com/connect/qrconnect"
	// gosec reads the word rather than the value. This is the address the
	// authorization code is spent at, published by WeChat; there is no
	// credential in it, and renaming it would be renaming somebody else's
	// endpoint.
	//nolint:gosec // G101: an endpoint, not a credential.
	weChatTokenURL   = "https://api.weixin.qq.com/sns/oauth2/access_token"
	weChatUserURL    = "https://api.weixin.qq.com/sns/userinfo"
	weChatLoginScope = "snsapi_login"
)

// WeChat is a configured Open Platform website application.
type WeChat struct {
	AppID       string
	AppSecret   string
	RedirectURI string

	client *http.Client
}

// NewWeChat builds one. The HTTP client is supplied rather than made here so
// that the timeout and the transport are the deployment's, and so a test can
// point it somewhere else — there is no discovery document to stand in for a
// fake provider, so the endpoints are the only seam.
func NewWeChat(appID, appSecret, redirectURI string, client *http.Client) *WeChat {
	return &WeChat{
		AppID: appID, AppSecret: appSecret,
		RedirectURI: redirectURI, client: client,
	}
}

// Issuer is the constant this provider's identities are namespaced under.
func (w *WeChat) Issuer() string { return weChatIssuer }

// AuthURL is the page carrying the QR code.
//
// nonce and codeVerifier are accepted and ignored: WeChat issues no ID
// token to carry a nonce, and does not implement PKCE. The single-use
// `state` this server issued is what ties a callback to a request, and it is
// the whole of that protection here — which is worth knowing rather than
// assuming the same defences are in place as for an OIDC provider.
func (w *WeChat) AuthURL(state, _, _ string) string {
	q := url.Values{}
	q.Set("appid", w.AppID)
	q.Set("redirect_uri", w.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", weChatLoginScope)
	q.Set("state", state)
	// WeChat reads the fragment for the QR page's own routing, and its
	// documentation is emphatic that it must be present and last.
	return weChatAuthURL + "?" + q.Encode() + "#wechat_redirect"
}

// weChatToken is the token response, which also carries the identity.
type weChatToken struct {
	AccessToken string `json:"access_token"`
	OpenID      string `json:"openid"`
	UnionID     string `json:"unionid"`
	Scope       string `json:"scope"`

	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// weChatUser is the profile, fetched only for something to show a person.
type weChatUser struct {
	Nickname string `json:"nickname"`
	UnionID  string `json:"unionid"`

	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// Exchange spends the authorization code.
func (w *WeChat) Exchange(ctx context.Context, code, _, _ string) (Identity, error) {
	q := url.Values{}
	q.Set("appid", w.AppID)
	q.Set("secret", w.AppSecret)
	q.Set("code", code)
	q.Set("grant_type", "authorization_code")

	var token weChatToken
	if err := w.get(ctx, weChatTokenURL+"?"+q.Encode(), &token); err != nil {
		return Identity{}, err
	}
	if token.ErrCode != 0 {
		return Identity{}, fmt.Errorf("wechat refused the code exchange: %d %s",
			token.ErrCode, token.ErrMsg)
	}
	if token.OpenID == "" {
		return Identity{}, fmt.Errorf("wechat returned no openid")
	}

	// The profile is a second call and is allowed to fail. What identifies
	// somebody arrived above; this only supplies a name to show them, and a
	// sign-in that works should not be refused because a display name did
	// not arrive. It is also where a unionid appears for an application whose
	// token response omitted one.
	var user weChatUser
	if token.AccessToken != "" {
		p := url.Values{}
		p.Set("access_token", token.AccessToken)
		p.Set("openid", token.OpenID)
		p.Set("lang", "zh_CN")
		if err := w.get(ctx, weChatUserURL+"?"+p.Encode(), &user); err != nil || user.ErrCode != 0 {
			user = weChatUser{}
		}
	}

	unionID := token.UnionID
	if unionID == "" {
		unionID = user.UnionID
	}

	return Identity{
		Issuer:      weChatIssuer,
		Subject:     WeChatSubject(unionID, token.OpenID),
		DisplayName: user.Nickname,
		// WeChat returns no address at all, verified or otherwise. So the
		// "trust verified addresses" switch can never do anything for this
		// provider, and the console says so rather than offering it.
	}, nil
}

// WeChatSubject decides what identifies a person, and says which it chose.
//
// A unionid is stable across every application under one Open Platform
// account; an openid is stable only within the one application. So a unionid
// is right whenever there is one — and there is not always one, because it
// requires the application to be bound to an Open Platform account, which is
// somebody else's administrative act.
//
// The prefix is not decoration. Should an application later gain a unionid
// it did not have, every identity bound under an openid stops matching, and
// the person is refused with "not linked to any account". That is a real
// migration rather than a bug, and it can only be written if the rows say
// which key they used. Encoded in the subject rather than in a column
// because this pair is the identity: two rows that disagree about which half
// of it is authoritative is exactly the state a separate column allows.
func WeChatSubject(unionID, openID string) string {
	if strings.TrimSpace(unionID) != "" {
		return "unionid:" + unionID
	}
	return "openid:" + openID
}

// get performs a call and decodes it, treating a non-200 as a failure.
//
// WeChat reports its own errors inside a 200, which each caller checks; this
// only catches the transport-level ones, where there is no body to read a
// code out of.
func (w *WeChat) get(ctx context.Context, target string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build the wechat request: %w", err)
	}
	res, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach wechat: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("wechat answered %d", res.StatusCode)
	}
	// Content-Type is text/plain on some of these endpoints, so the body is
	// decoded on its shape rather than on what it claims to be.
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		return fmt.Errorf("read wechat's answer: %w", err)
	}
	return nil
}
