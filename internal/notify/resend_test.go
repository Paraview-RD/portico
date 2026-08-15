package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The HTTP transport, held to the shape Resend documents.
//
// Everything here is asserted against a local server rather than the real
// one. The value is not in proving Resend works — it is in proving that this
// package sends what Resend expects, which is a thing that can only be got
// wrong silently: a misspelled field is accepted as JSON, refused by the API,
// and reported to the visitor as "the confirmation email could not be sent".

// mailer builds a resend mailer pointed at a test server.
func mailer(t *testing.T, handler http.HandlerFunc) (Mailer, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	m, err := NewResendMailer(ResendConfig{
		APIKey:   "re_test_key",
		From:     "portico@example.com",
		endpoint: server.URL,
		client:   server.Client(),
	})
	if err != nil {
		t.Fatalf("build mailer: %v", err)
	}
	return m, server
}

func TestTheRequestCarriesWhatResendExpects(t *testing.T) {
	var (
		method string
		auth   string
		ctype  string
		body   map[string]any
	)

	m, _ := mailer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		auth = r.Header.Get("Authorization")
		ctype = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"c1f0…"}`))
	})

	err := m.Send(context.Background(), Message{
		To:      "someone@example.org",
		Subject: "Confirm your Portico trial",
		Body:    "Open this link.",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if method != http.MethodPost {
		t.Errorf("method was %s, want POST", method)
	}
	// Bearer, not the raw key: a key sent as the whole header value is
	// refused with a 401 that names authentication, which reads like a bad
	// key rather than a bad request.
	if auth != "Bearer re_test_key" {
		t.Errorf("Authorization was %q", auth)
	}
	if !strings.HasPrefix(ctype, "application/json") {
		t.Errorf("Content-Type was %q", ctype)
	}

	if body["from"] != "portico@example.com" {
		t.Errorf("from was %v", body["from"])
	}
	// An array even for one recipient, which is what the API takes. A bare
	// string is a 422 that this package would report as an unexplained
	// failure to send.
	to, ok := body["to"].([]any)
	if !ok || len(to) != 1 || to[0] != "someone@example.org" {
		t.Errorf("to was %#v, want a one-element array", body["to"])
	}
	if body["subject"] != "Confirm your Portico trial" {
		t.Errorf("subject was %v", body["subject"])
	}
	if body["text"] != "Open this link." {
		t.Errorf("text was %v", body["text"])
	}
	// Plain text only. An html part would be sent as marketing by half the
	// receiving side, and there is nothing in these messages to mark up.
	if _, present := body["html"]; present {
		t.Errorf("the request carried an html part: %#v", body["html"])
	}
}

func TestARefusalSaysWhatResendSaid(t *testing.T) {
	// The case an operator actually hits: a from-address on a domain that was
	// never verified. The status alone does not distinguish it from a key
	// that may not send, and the difference decides whether they go and fix
	// their account or their configuration.
	m, _ := mailer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"statusCode":403,"name":"validation_error",` +
			`"message":"The example.com domain is not verified."}`))
	})

	err := m.Send(context.Background(), Message{To: "someone@example.org"})
	if err == nil {
		t.Fatal("a 403 was reported as a successful send")
	}
	if !strings.Contains(err.Error(), "domain is not verified") {
		t.Errorf("the error does not repeat what Resend said: %v", err)
	}
	if !strings.Contains(err.Error(), "validation_error") {
		t.Errorf("the error drops the error name: %v", err)
	}
}

func TestARefusalWithNoBodyStillReportsTheStatus(t *testing.T) {
	// A gateway between here and Resend can answer with nothing at all.
	// Reporting that as a nil error would tell a visitor their link is on the
	// way when nothing was sent.
	m, _ := mailer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	err := m.Send(context.Background(), Message{To: "someone@example.org"})
	if err == nil {
		t.Fatal("a 502 with an empty body was reported as a successful send")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("the error does not name the status: %v", err)
	}
}

func TestTheKeyIsNeverInAnError(t *testing.T) {
	// Every one of these errors is logged, and a log line carrying a sending
	// key is a credential in a file somebody forwards.
	m, server := mailer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"API key is invalid"}`))
	})

	err := m.Send(context.Background(), Message{To: "someone@example.org"})
	if err == nil || strings.Contains(err.Error(), "re_test_key") {
		t.Errorf("the error carries the API key: %v", err)
	}

	// And the same when the request never arrives.
	server.Close()
	err = m.Send(context.Background(), Message{To: "someone@example.org"})
	if err == nil || strings.Contains(err.Error(), "re_test_key") {
		t.Errorf("a transport failure carries the API key: %v", err)
	}
}

func TestResendWithoutItsSettingsIsRefusedAtStartup(t *testing.T) {
	// Not the not-configured mailer. NotConfiguredMailer means a deployment
	// did not ask for email; asking for Resend without a key is asking for
	// something and getting it wrong, and it has to be heard at startup
	// rather than at somebody's first password reset.
	for _, cfg := range []ResendConfig{
		{From: "portico@example.com"},
		{APIKey: "re_test_key"},
	} {
		if _, err := NewResendMailer(cfg); err == nil {
			t.Errorf("NewResendMailer(%+v) was accepted", cfg)
		}
	}
}

func TestAnUnknownTransportIsNotQuietlyTheDefault(t *testing.T) {
	// A typo that selected SMTP would leave somebody configuring a key that
	// is never read, watching every message fail to reach a relay they
	// thought they had stopped using.
	if _, err := NewMailer(MailConfig{Transport: "resent"}); err == nil {
		t.Fatal("an unknown transport was accepted")
	}

	// And the two that are real, including the empty default.
	for _, transport := range []string{"", TransportSMTP} {
		m, err := NewMailer(MailConfig{Transport: transport})
		if err != nil {
			t.Fatalf("transport %q: %v", transport, err)
		}
		if _, ok := m.(NotConfiguredMailer); !ok {
			t.Errorf("transport %q with no host gave %T, want the not-configured mailer",
				transport, m)
		}
	}
}
