package socialrp

import (
	"testing"
)

// The two things about these providers that are decisions rather than
// transcription, and that a later change could quietly undo.

// WeChat's subject is a choice with a consequence, so it is a named function
// with a test rather than three lines inside an exchange.
func TestAWeChatSubjectPrefersTheUnionIDAndSaysWhichItUsed(t *testing.T) {
	for _, c := range []struct {
		name    string
		unionID string
		openID  string
		want    string
	}{
		{"a bound application has both", "u-123", "o-456", "unionid:u-123"},
		{"an unbound one has only an openid", "", "o-456", "openid:o-456"},
		{"blank is not a unionid", "   ", "o-456", "openid:o-456"},
	} {
		if got := WeChatSubject(c.unionID, c.openID); got != c.want {
			t.Errorf("%s: subject = %q, want %q", c.name, got, c.want)
		}
	}
}

// The prefix is the whole point of the previous test, and this says why in a
// form that fails if somebody removes it as noise.
func TestTheTwoWeChatIdentifiersCannotBeConfusedForEachOther(t *testing.T) {
	// The same string, arriving as each kind, must not be the same identity.
	// Without the prefix it would be — and a person whose application later
	// gained a unionid would silently match somebody else's openid row if the
	// values ever collided.
	if WeChatSubject("same", "") == WeChatSubject("", "same") {
		t.Error("a unionid and an openid with the same value produce the same " +
			"subject; the pair (issuer, subject) can then name two different " +
			"people, which is the one thing an identity must not do")
	}
}

// An issuer is a constant per kind, and the constants are what every stored
// identity is namespaced under. Getting one wrong later would orphan every
// binding made before the change, so it is pinned.
func TestEachKindHasItsOwnFixedIssuer(t *testing.T) {
	if Issuer(KindWeChat) != "https://open.weixin.qq.com" {
		t.Errorf("wechat issuer = %q", Issuer(KindWeChat))
	}
	if Issuer(KindDingTalk) != "https://login.dingtalk.com" {
		t.Errorf("dingtalk issuer = %q", Issuer(KindDingTalk))
	}
	if Issuer("OIDC") != "" {
		t.Error("OIDC has no fixed issuer here; its issuer is configured")
	}
	if Issuer(KindWeChat) == Issuer(KindDingTalk) {
		t.Error("two kinds share an issuer, so the same subject from each " +
			"would be one identity")
	}
}

// WeChat returns no address, so the setting that lets an address reach an
// existing account can only mislead for it.
func TestWeChatCannotOfferToTrustAnAddressItNeverSends(t *testing.T) {
	if SupportsVerifiedEmail(KindWeChat) {
		t.Error("WeChat is offered the trust-verified-email switch, which it " +
			"can never satisfy — it returns no address at all, so the switch " +
			"would read as a decision somebody made and do nothing")
	}
	if !SupportsVerifiedEmail(KindDingTalk) {
		t.Error("DingTalk does return an address; the switch is a real choice there")
	}
}
