// Package notify delivers messages to a person, over whichever channels the
// deployment has configured.
//
// The interfaces exist so that the service layer never knows how a message
// travels. That matters for testing — a fake recorder is what lets the
// password-recovery tests assert where a token went — and it is the seam an
// operator picks a provider at.
//
// A deployment that has configured nothing is the normal starting state, not
// an error: the binary must run with no environment at all. Every sender
// therefore has a no-op form that fails with ErrNotConfigured, and callers
// turn that into an honest "this deployment cannot do that" rather than a
// silent success.
package notify

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotConfigured means no provider was configured for this channel.
//
// It is a sentinel rather than a Configured() method on the interface
// because the two cannot then disagree: there is no way to report configured
// and fail to send, or to report unconfigured and quietly deliver.
var ErrNotConfigured = errors.New("notify: no provider is configured for this channel")

// Message is one email.
type Message struct {
	To      string
	Subject string
	// Body is plain text. Portico sends no HTML mail: an identity server's
	// messages are short and transactional, and an HTML part is mostly a way
	// to be classified as marketing.
	Body string
}

// Mailer sends email.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Transport names for MailConfig.
const (
	// TransportSMTP relays through a mail server. The default, and the right
	// answer for anybody who has one.
	TransportSMTP = "smtp"
	// TransportResend posts to an HTTP API instead, for the deployments where
	// SMTP ports are not open at all. See resend.go.
	TransportResend = "resend"
)

// MailConfig is which transport carries the mail, and the settings for each.
//
// One struct rather than a choice made at the call site: the server asks for
// a Mailer and is handed one, and how a deployment configured it is a
// question the server has no business answering twice.
type MailConfig struct {
	// Transport is TransportSMTP (or empty, meaning the same) or
	// TransportResend.
	Transport string
	SMTP      SMTPConfig
	Resend    ResendConfig
}

// NewMailer builds the Mailer this deployment asked for.
//
// An unrecognised transport is an error rather than a fall back to SMTP. A
// typo that silently selects the default would leave somebody configuring a
// key that is never read, watching every message fail to connect to a relay
// they thought they had stopped using.
func NewMailer(cfg MailConfig) (Mailer, error) {
	switch cfg.Transport {
	case TransportSMTP, "":
		return NewSMTPMailer(cfg.SMTP)
	case TransportResend:
		return NewResendMailer(cfg.Resend)
	default:
		return nil, fmt.Errorf(
			"notify: unknown mail transport %q; it must be one of %s, %s",
			cfg.Transport, TransportSMTP, TransportResend)
	}
}

// SMSSender sends a text message.
//
// Every provider has its own API, so this is the seam rather than a
// half-hearted attempt at a common one. Implementing it against a provider
// means one type with one method; nothing above this interface changes.
type SMSSender interface {
	Send(ctx context.Context, phone, text string) error
}

// NotConfiguredMailer is the Mailer a deployment has when PORTICO_SMTP_HOST
// is unset.
type NotConfiguredMailer struct{}

// Send always fails with ErrNotConfigured.
func (NotConfiguredMailer) Send(context.Context, Message) error { return ErrNotConfigured }

// NotConfiguredSMS is the SMSSender every deployment has today.
//
// V0.1 ships the interface and no provider (a deliberate scope decision):
// SMS delivery means an account with a vendor, a per-message cost, and
// regional rules about what may be sent, none of which belong in a default
// build. Recovery over SMS therefore reports that the channel is
// unavailable, which is true, rather than accepting a request it cannot
// fulfil.
type NotConfiguredSMS struct{}

// Send always fails with ErrNotConfigured.
func (NotConfiguredSMS) Send(context.Context, string, string) error { return ErrNotConfigured }
