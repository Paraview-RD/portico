package socialrp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// DingTalk, through the login API it has had since 2022.
//
// Closer to OAuth 2 than WeChat is — the token request is a JSON POST and
// failures arrive with an HTTP status that means what it says — but still
// not OpenID Connect: there is no discovery document, no ID token, and the
// access token goes in a header of DingTalk's own naming rather than in
// Authorization.
//
// `scope=openid` in the authorization request is DingTalk's spelling, not a
// promise of OIDC. Nothing signed comes back.
const (
	dingTalkIssuer   = "https://login.dingtalk.com"
	dingTalkAuthURL  = "https://login.dingtalk.com/oauth2/auth"
	dingTalkTokenURL = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
	dingTalkUserURL  = "https://api.dingtalk.com/v1.0/contact/users/me"
	dingTalkScope    = "openid"
)

// DingTalk is a configured application.
type DingTalk struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string

	client *http.Client
}

// NewDingTalk builds one.
func NewDingTalk(clientID, clientSecret, redirectURI string, client *http.Client) *DingTalk {
	return &DingTalk{
		ClientID: clientID, ClientSecret: clientSecret,
		RedirectURI: redirectURI, client: client,
	}
}

// Issuer is the constant this provider's identities are namespaced under.
func (d *DingTalk) Issuer() string { return dingTalkIssuer }

// AuthURL is where to send somebody. nonce and codeVerifier are ignored, for
// the same reasons as WeChat: no ID token to carry one, no PKCE to use the
// other. The single-use state is what ties the callback to this request.
func (d *DingTalk) AuthURL(state, _, _ string) string {
	q := url.Values{}
	q.Set("client_id", d.ClientID)
	q.Set("redirect_uri", d.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", dingTalkScope)
	q.Set("state", state)
	// Asked for explicitly. Without it DingTalk may complete a sign-in
	// silently for somebody already authenticated in the browser, which is
	// the opposite of what a person expects from pressing a button that says
	// "sign in with DingTalk" while watching.
	q.Set("prompt", "consent")
	return dingTalkAuthURL + "?" + q.Encode()
}

type dingTalkToken struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

type dingTalkUser struct {
	Nick    string `json:"nick"`
	OpenID  string `json:"openId"`
	UnionID string `json:"unionId"`
	Email   string `json:"email"`
	Mobile  string `json:"mobile"`
}

// Exchange spends the authorization code.
func (d *DingTalk) Exchange(ctx context.Context, code, _, _ string) (Identity, error) {
	body, err := json.Marshal(map[string]string{
		"clientId":     d.ClientID,
		"clientSecret": d.ClientSecret,
		"code":         code,
		"grantType":    "authorization_code",
	})
	if err != nil {
		return Identity{}, fmt.Errorf("build the dingtalk token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dingTalkTokenURL, bytes.NewReader(body))
	if err != nil {
		return Identity{}, fmt.Errorf("build the dingtalk token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var token dingTalkToken
	if err := d.do(ctx, req, &token); err != nil {
		return Identity{}, err
	}
	if token.AccessToken == "" {
		return Identity{}, fmt.Errorf("dingtalk returned no access token")
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dingTalkUserURL, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("build the dingtalk profile request: %w", err)
	}
	// DingTalk's own header rather than Authorization, which is the one
	// thing about this API most likely to be got wrong by analogy.
	userReq.Header.Set("x-acs-dingtalk-access-token", token.AccessToken)

	var user dingTalkUser
	if err := d.do(ctx, userReq, &user); err != nil {
		return Identity{}, err
	}

	// unionId over openId for the same reason as WeChat: openId is scoped to
	// one application and unionId is not. DingTalk returns both, so unlike
	// WeChat there is no fallback to write and no later migration to expect.
	subject := user.UnionID
	if subject == "" {
		subject = user.OpenID
	}
	if subject == "" {
		return Identity{}, fmt.Errorf("dingtalk returned no identifier for this person")
	}

	return Identity{
		Issuer:      dingTalkIssuer,
		Subject:     subject,
		DisplayName: user.Nick,
		Email:       user.Email,
		// Never true. DingTalk states the address it holds for somebody and
		// does not say it has been proved, and this flag decides whether an
		// address alone may reach an existing account — so the absence of a
		// claim has to read as "not verified" rather than as "assume so".
		EmailVerified: false,
	}, nil
}

// do performs a request and decodes it.
func (d *DingTalk) do(ctx context.Context, req *http.Request, into any) error {
	res, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach dingtalk: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		// The body carries a code and a message, and both are useful in a
		// log — but they are not returned to the caller, which turns every
		// failure here into one refusal a person sees.
		return fmt.Errorf("dingtalk answered %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		return fmt.Errorf("read dingtalk's answer: %w", err)
	}
	_ = ctx
	return nil
}
