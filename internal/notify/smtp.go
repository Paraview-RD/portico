package notify

import (
	"context"
	"fmt"

	"github.com/wneessen/go-mail"
)

// SMTPConfig describes the mail server to relay through.
//
// Plain SMTP rather than a vendor SDK is the whole point: an operator with a
// mail server already has one, and every hosted provider speaks it. Nothing
// here ties a deployment to a company.
type SMTPConfig struct {
	// Host empty means email is not configured, and NewSMTPMailer returns the
	// not-configured sender rather than an error — running with no
	// environment at all has to work.
	Host string
	Port int

	// Username empty means connect without authentication, which is normal
	// for a relay on a private network.
	Username string
	Password string

	// From is the envelope and header sender.
	From string

	// Encryption is "starttls", "tls", or "none".
	Encryption string
}

// SMTPEncryption values.
const (
	EncryptionSTARTTLS = "starttls"
	EncryptionTLS      = "tls"
	EncryptionNone     = "none"
)

// NewSMTPMailer builds a Mailer from cfg, or the not-configured sender when
// no host is set.
func NewSMTPMailer(cfg SMTPConfig) (Mailer, error) {
	if cfg.Host == "" {
		return NotConfiguredMailer{}, nil
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("notify: PORTICO_SMTP_FROM is required when a host is set")
	}

	options := []mail.Option{mail.WithPort(cfg.Port)}

	switch cfg.Encryption {
	case EncryptionTLS:
		// Implicit TLS, usually port 465. WithSSL rather than WithSSLPort:
		// the latter also rewrites the port and can fall back to an
		// unencrypted connection, which is not something to do silently
		// while carrying a reset link.
		options = append(options, mail.WithSSL())
	case EncryptionNone:
		// Deliberately available: a relay reachable only over a private
		// network is a legitimate deployment, and refusing it would push
		// people to disable verification instead, which is worse.
		options = append(options, mail.WithTLSPolicy(mail.NoTLS))
	default:
		// STARTTLS is required rather than opportunistic. Opportunistic
		// STARTTLS is downgradeable by anyone on the path, which for a
		// message carrying a password-reset link is the whole threat.
		options = append(options, mail.WithTLSPolicy(mail.TLSMandatory))
	}

	if cfg.Username != "" {
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password))
	}

	client, err := mail.NewClient(cfg.Host, options...)
	if err != nil {
		return nil, fmt.Errorf("notify: configure SMTP: %w", err)
	}

	return &smtpMailer{client: client, from: cfg.From}, nil
}

type smtpMailer struct {
	client *mail.Client
	from   string
}

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	message := mail.NewMsg()
	if err := message.From(m.from); err != nil {
		return fmt.Errorf("notify: sender address %q: %w", m.from, err)
	}
	if err := message.To(msg.To); err != nil {
		// Rejecting the address here rather than passing it to the server is
		// what keeps a crafted address from being encoded into extra
		// envelope commands.
		return fmt.Errorf("notify: recipient address: %w", err)
	}
	message.Subject(msg.Subject)
	message.SetBodyString(mail.TypeTextPlain, msg.Body)

	if err := m.client.DialAndSendWithContext(ctx, message); err != nil {
		return fmt.Errorf("notify: send mail: %w", err)
	}
	return nil
}
