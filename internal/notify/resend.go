package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendConfig describes sending over Resend's HTTP API instead of SMTP.
//
// This exists for one reason: a great many places a small deployment can
// afford will not let a process open an SMTP port. Render blocks outbound 25,
// 465 and 587 on its free instances, most other free tiers do the same, and
// port 25 stays blocked even on paid ones because the whole platform sits
// behind a cloud provider that blocks it. In every one of those places SMTP
// fails as a connection timeout — nothing in the message is wrong, and no
// amount of correcting the relay settings helps.
//
// HTTPS is not blocked anywhere, which is the entire argument. Nothing about
// the product changes: it is the same Message going to the same address, over
// a socket the network will actually open.
//
// SMTP stays the default and is still the right answer for anybody with a
// relay of their own. This is the escape hatch for people who cannot use one,
// not a recommendation.
type ResendConfig struct {
	// APIKey is a Resend key. Send-only keys work and are what should be
	// used: nothing here reads or lists anything.
	APIKey string

	// From is the sender address. It must belong to a domain verified with
	// Resend — with no verified domain, the account may only send from
	// onboarding@resend.dev, and only to the address that owns the account.
	From string

	// endpoint is the API address, overridden by tests. Empty means Resend.
	endpoint string

	// client is the HTTP client, overridden by tests.
	client *http.Client
}

const resendEndpoint = "https://api.resend.com/emails"

// NewResendMailer builds a Mailer that posts to Resend.
//
// Both fields are required, and an empty one is an error rather than the
// not-configured sender. The distinction is the one the whole package is
// built on: NotConfiguredMailer means this deployment did not ask for email,
// and asking for Resend without a key is asking for something and getting it
// wrong. Reported at startup, where somebody is watching, rather than at the
// first password reset.
func NewResendMailer(cfg ResendConfig) (Mailer, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("notify: PORTICO_RESEND_API_KEY is required when the mail transport is resend")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("notify: PORTICO_MAIL_FROM is required when the mail transport is resend")
	}

	if cfg.endpoint == "" {
		cfg.endpoint = resendEndpoint
	}
	if cfg.client == nil {
		// A timeout of its own, because the caller's context is not always
		// one that ends: a request that hangs here holds a sign-in or a trial
		// submission open for as long as the network cares to.
		cfg.client = &http.Client{Timeout: 20 * time.Second}
	}
	return &resendMailer{cfg: cfg}, nil
}

type resendMailer struct {
	cfg ResendConfig
}

// resendRequest is the documented body of POST /emails.
//
// Text rather than html, matching what Message carries and what this product
// sends: short transactional messages, which an HTML part would mostly help
// to get classified as marketing.
type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (m *resendMailer) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(resendRequest{
		From:    m.cfg.From,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Text:    msg.Body,
	})
	if err != nil {
		return fmt.Errorf("notify: encode mail: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: build mail request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.cfg.client.Do(req)
	if err != nil {
		// Deliberately not wrapping the URL error's own text further: it
		// already names the host and the failure, and the key is not in it.
		return fmt.Errorf("notify: send mail: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("notify: send mail: %w", refusal(resp))
}

// refusal turns a non-2xx response into an error worth reading.
//
// The status alone is not enough to act on. Resend answers 403 both for a key
// that is not allowed to send and for a from-address on an unverified domain,
// and the difference is only in the message — which is the sentence that tells
// an operator whether to fix their account or their configuration.
//
// The body is read with a limit because it arrives from outside and ends up in
// a log line.
func refusal(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("resend answered %s with no explanation", resp.Status)
	}

	var problem struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &problem); err != nil || problem.Message == "" {
		return fmt.Errorf("resend answered %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	if problem.Name != "" {
		return fmt.Errorf("resend answered %s (%s): %s", resp.Status, problem.Name, problem.Message)
	}
	return fmt.Errorf("resend answered %s: %s", resp.Status, problem.Message)
}
