package notify

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // required by Aliyun's Dysmsapi V2 RPC signing scheme (HMAC-SHA1); not our choice.
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const aliyunSMSEndpoint = "https://dysmsapi.aliyuncs.com/"

// AliyunSMSConfig describes sending SMS through Alibaba Cloud's Dysmsapi.
//
// Every message this project sends over SMS goes through a template Aliyun
// has reviewed and approved in advance -- see the SMSKind doc comment on
// why. TemplateCodes is therefore a map rather than one field: a login code,
// a recovery link, and a verification link are three different reviewed
// templates with three different variable names, and a deployment may have
// approval for only some of them.
type AliyunSMSConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	// SignName is the approved SMS signature shown before the message body,
	// e.g. "【Portico】". Required by Aliyun on every send.
	SignName string
	// TemplateCodes maps each SMSKind this deployment can send to the
	// template code Aliyun issued for it. A kind absent from this map is
	// refused at Send time, not at construction: a deployment may have
	// approval for the login-code template only, and still is not asking
	// this to fail before it ever tries to use the one it has.
	TemplateCodes map[SMSKind]string
	// Endpoint overrides the API address; empty means Aliyun's default.
	// Set by tests.
	Endpoint string
	// client is the HTTP client, overridden by tests.
	client *http.Client
}

type aliyunSMSSender struct {
	cfg    AliyunSMSConfig
	client *http.Client
}

// NewAliyunSMSSender builds an SMSSender backed by Alibaba Cloud's
// Dysmsapi.
//
// The three account-identifying fields are required and checked here,
// reported at startup rather than at the first sign-in. TemplateCodes is
// allowed to be a subset of SMSKind -- see the field's doc comment.
func NewAliyunSMSSender(cfg AliyunSMSConfig) (SMSSender, error) {
	if cfg.AccessKeyID == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_ACCESS_KEY_ID is required")
	}
	if cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_ACCESS_KEY_SECRET is required")
	}
	if cfg.SignName == "" {
		return nil, fmt.Errorf("notify: PORTICO_ALIYUN_SMS_SIGN_NAME is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = aliyunSMSEndpoint
	}
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 10 * time.Second}
	}
	return &aliyunSMSSender{cfg: cfg, client: cfg.client}, nil
}

// Send posts one message through Dysmsapi's SendSms action.
func (a *aliyunSMSSender) Send(ctx context.Context, phone string, kind SMSKind, params map[string]string) error {
	templateCode, ok := a.cfg.TemplateCodes[kind]
	if !ok {
		return fmt.Errorf("notify: no Aliyun template configured for SMS kind %q", kind)
	}

	paramJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal template params: %w", err)
	}

	nonce, err := randomNonce()
	if err != nil {
		return err
	}

	query := url.Values{
		"AccessKeyId":      {a.cfg.AccessKeyID},
		"Action":           {"SendSms"},
		"Format":           {"JSON"},
		"PhoneNumbers":     {phone},
		"SignName":         {a.cfg.SignName},
		"SignatureMethod":  {"HMAC-SHA1"},
		"SignatureNonce":   {nonce},
		"SignatureVersion": {"1.0"},
		"TemplateCode":     {templateCode},
		"TemplateParam":    {string(paramJSON)},
		"Timestamp":        {time.Now().UTC().Format("2006-01-02T15:04:05Z")},
		"Version":          {"2017-05-25"},
	}
	query.Set("Signature", a.sign(http.MethodGet, query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.Endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build Aliyun SMS request: %w", err)
	}

	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Aliyun SMS: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var body struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode Aliyun SMS response: %w", err)
	}
	if body.Code != "OK" {
		return fmt.Errorf("notify: Aliyun SMS gateway refused: %s: %s", body.Code, body.Message)
	}
	return nil
}

// sign computes Dysmsapi's RPC-style signature: HMAC-SHA1 over
// "<method>&<percent-encoded '/'>&<percent-encoded, sorted query string>",
// keyed by "<AccessKeySecret>&".
//
// Verified 2026-09-21 against Alibaba Cloud's current "Request syntax and
// signature method V2 for RPC APIs" documentation
// (https://www.alibabacloud.com/help/en/sdk/product-overview/rpc-mechanism):
// StringToSign = percentEncode(HTTPMethod) + "&" + percentEncode("/") + "&"
// + percentEncode(CanonicalizedQueryString), HMAC-SHA1 keyed by
// "<AccessKeySecret>&", Base64-encoded. That page notes signature method V2
// itself is deprecated in favor of V3, but Dysmsapi's SendSms action (and
// its official SDKs) still use this HMAC-SHA1 query-signing scheme as of
// this writing.
func (a *aliyunSMSSender) sign(method string, query url.Values) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, percentEncode(k)+"="+percentEncode(query.Get(k)))
	}
	canonical := strings.Join(pairs, "&")

	toSign := method + "&" + percentEncode("/") + "&" + percentEncode(canonical)

	mac := hmac.New(sha1.New, []byte(a.cfg.AccessKeySecret+"&"))
	mac.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// percentEncode follows Aliyun's RFC 3986 variant, confirmed against the
// current documentation's own reference implementation:
//
//	URLEncoder.encode(str, UTF-8).replace("+", "%20").replace("*", "%2A").replace("%7E", "~")
//
// i.e. url.QueryEscape's output is adjusted because Aliyun additionally
// requires '~' left un-encoded and '*' encoded (the reverse of Go's
// default), and a literal space encoded as %20 rather than QueryEscape's
// '+'.
func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func randomNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate signature nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
