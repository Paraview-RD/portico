package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNewAliyunSMSSenderRequiresCoreFields(t *testing.T) {
	cases := []AliyunSMSConfig{
		{AccessKeySecret: "s", SignName: "n"},
		{AccessKeyID: "k", SignName: "n"},
		{AccessKeyID: "k", AccessKeySecret: "s"},
	}
	for _, cfg := range cases {
		if _, err := NewAliyunSMSSender(cfg); err == nil {
			t.Errorf("cfg %+v: want an error for a missing required field", cfg)
		}
	}
}

func TestAliyunSMSSenderSendsTheConfiguredTemplate(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return
		}
		gotQuery = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Code": "OK", "Message": "OK"})
	}))
	defer server.Close()

	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
		SignName:        "Portico",
		TemplateCodes:   map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
		Endpoint:        server.URL,
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}

	err = sender.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456", "Minutes": "5"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := gotQuery.Get("PhoneNumbers"); got != "+8613800138000" {
		t.Errorf("PhoneNumbers = %q", got)
	}
	if got := gotQuery.Get("TemplateCode"); got != "SMS_000001" {
		t.Errorf("TemplateCode = %q", got)
	}
	if got := gotQuery.Get("SignName"); got != "Portico" {
		t.Errorf("SignName = %q", got)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(gotQuery.Get("TemplateParam")), &params); err != nil {
		t.Fatalf("TemplateParam is not JSON: %v", err)
	}
	if params["Code"] != "123456" || params["Minutes"] != "5" {
		t.Errorf("TemplateParam = %v", params)
	}
	if gotQuery.Get("Signature") == "" {
		t.Error("request was not signed")
	}
}

func TestAliyunSMSSenderRefusesAnUnconfiguredKind(t *testing.T) {
	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID: "key", AccessKeySecret: "secret", SignName: "Portico",
		TemplateCodes: map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}
	err = sender.Send(context.Background(), "+8613800138000", SMSKindRecovery, nil)
	if err == nil {
		t.Fatal("want an error: no template configured for SMSKindRecovery")
	}
}

func TestAliyunSMSSenderReportsAGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Code": "isv.BUSINESS_LIMIT_CONTROL", "Message": "触发分钟级流控Permits:1",
		})
	}))
	defer server.Close()

	sender, err := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID: "key", AccessKeySecret: "secret", SignName: "Portico",
		TemplateCodes: map[SMSKind]string{SMSKindLoginCode: "SMS_000001"},
		Endpoint:      server.URL,
	})
	if err != nil {
		t.Fatalf("NewAliyunSMSSender: %v", err)
	}
	err = sender.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456"})
	if err == nil {
		t.Fatal("want an error when Aliyun's Code is not OK")
	}
}

func TestAliyunSignatureMatchesAliyunsPublishedVector(t *testing.T) {
	s, _ := NewAliyunSMSSender(AliyunSMSConfig{
		AccessKeyID: "testid", AccessKeySecret: "testsecret", SignName: "x",
	})
	q := url.Values{
		"AccessKeyId": {"testid"}, "Action": {"DescribeDedicatedHosts"},
		"Format": {"JSON"}, "RegionId": {"cn-beijing"},
		"SignatureMethod":  {"HMAC-SHA1"},
		"SignatureNonce":   {"edb2b34af0af9a6d14deaf7c1a5315eb"},
		"SignatureVersion": {"1.0"}, "Timestamp": {"2023-03-13T08:34:30Z"},
		"Version": {"2014-05-26"},
	}
	if got := s.(*aliyunSMSSender).sign("GET", q); got != "9NaGiOspFP5UPcwX8Iwt2YJXXuk=" {
		t.Errorf("signature = %q, want the value Aliyun's own documentation publishes for this input", got)
	}
}
