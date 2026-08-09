package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/i18n"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/notify"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Proving that a self-registered account owns the address it gave.
//
// Registration used to create a usable account immediately with whatever
// email was typed. That address is both a sign-in identifier and where a
// password-reset link goes, so an unverified one lets somebody open an
// account under a colleague's address and then receive their reset links.
// Acceptable on a closed intranet, which is the only place open registration
// has ever been usable here; not acceptable the moment it faces outward.
//
// Deliberately not folded into RecoveryService, and not sharing its table.
// The two flows look alike and mean opposite things: a reset token lets
// somebody in, and a verification token only marks an address as proven.
// Keeping them apart is what stops one query's mistake turning a token
// issued for the second purpose into one redeemed for the first.

// VerificationTokenTTL is how long a verification link stays usable.
//
// Longer than a reset link, on purpose. A reset is something somebody is
// waiting on with a form open; a registration is often finished later, and
// an expired link sends them back to a form they have already filled in. It
// is also a far weaker token: redeeming it grants nothing beyond marking an
// address proven.
const VerificationTokenTTL = 24 * time.Hour

// verificationDeliveryTimeout bounds the work that continues after the
// response, so a relay that accepts a connection and stops talking cannot
// hold a goroutine indefinitely.
const verificationDeliveryTimeout = 30 * time.Second

// ErrVerificationUnavailable is returned when verification is required and
// this deployment cannot send anything.
//
// It is checked here as well as when the setting is saved. The setting is
// validated once, at the moment somebody turns it on; removing SMTP from the
// environment afterwards would leave it standing, and registration would
// create accounts nobody can ever verify.
var ErrVerificationUnavailable = httpx.NewError(503, "VERIFICATION_UNAVAILABLE",
	"This deployment requires new accounts to verify an address and has no way to send one. "+
		"An administrator has to configure a mail relay or switch the requirement off.")

// ErrInvalidVerificationToken is what every way of failing to redeem a link
// returns.
//
// Unknown, spent, and expired are one answer for the same reason they are in
// password recovery: distinguishing them tells somebody holding a stolen but
// dead link that it was once real, and a legitimate person does the same
// thing in all three cases — ask for another.
var ErrInvalidVerificationToken = httpx.BadRequest("INVALID_VERIFICATION_TOKEN",
	"That verification link is not valid or has already been used. Request another.")

// VerificationService issues and redeems address-verification tokens.
type VerificationService struct {
	store    *store.Store
	users    *UserService
	settings *SettingsService
	messages *i18n.Catalog
	audit    *AuditService
	mailer   notify.Mailer
	sms      notify.SMSSender

	publicURL string
}

// NewVerificationService wires a VerificationService.
func NewVerificationService(
	st *store.Store,
	users *UserService,
	settings *SettingsService,
	audit *AuditService,
	mailer notify.Mailer,
	sms notify.SMSSender,
	publicURL string,
) *VerificationService {
	return &VerificationService{
		store: st, users: users, settings: settings, audit: audit,
		messages: i18n.MustLoad(),
		mailer:   mailer, sms: sms, publicURL: publicURL,
	}
}

// Required reports whether this tenant makes a self-registered account prove
// its address before it can sign in.
func (s *VerificationService) Required(ctx context.Context, tenantID string) (bool, error) {
	settings, err := s.settings.Get(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return settings.RegistrationVerification, nil
}

// Channel picks how to reach an account: the address it gave, over a channel
// this deployment can actually use.
//
// Email first where both are possible, because that is the one with a
// provider in this version; SMS is defined and ships without one, so a
// deployment offering it is a deployment that added one.
func (s *VerificationService) channelFor(row sqlcgen.User) (model.RecoveryChannel, bool) {
	available := map[model.RecoveryChannel]bool{}
	if s.settings.deliverable != nil {
		for _, c := range s.settings.deliverable() {
			available[c] = true
		}
	}
	if row.Email != "" && available[model.RecoveryEmail] {
		return model.RecoveryEmail, true
	}
	if row.Phone != "" && available[model.RecoverySMS] {
		return model.RecoverySMS, true
	}
	return "", false
}

// Send issues a token for an account and delivers it.
//
// Called from registration, where the account has just been created and the
// caller is entitled to know whether it worked — unlike Resend below, which
// must not disclose anything.
func (s *VerificationService) Send(ctx context.Context, tenant model.Tenant, userID string) error {
	q := s.store.ForTenant(tenant.ID)

	row, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	channel, ok := s.channelFor(row)
	if !ok {
		return ErrVerificationUnavailable
	}

	token, err := s.issue(ctx, q, row, channel)
	if err != nil {
		return err
	}
	if err := s.deliver(ctx, tenant, channel, row, token); err != nil {
		// The token is recorded already, so a delivery failure leaves an
		// unusable row that expires on its own — and the caller is told, so
		// somebody registering does not sit waiting for a message that was
		// never sent.
		return fmt.Errorf("deliver verification: %w", err)
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionVerificationSent,
		ActorID: row.ID, ActorName: row.Username,
		TargetType: "USER", TargetID: row.ID, TargetName: row.Username,
		Detail: fmt.Sprintf("sent over %s", channel),
	})
	return nil
}

// Resend issues another token for whoever holds destination.
//
// It returns nil whether or not an account was found, whether or not that
// account still needs verifying, and whether or not delivery worked. The
// endpoint is public and unauthenticated, so reporting the difference would
// make it an oracle for "does this address have an account here".
//
// That is a deliberate asymmetry with sign-in, which *does* disclose: a
// person refused for being unverified has to be told why, or they have no
// way forward at all. The disclosure is confined to somebody who already has
// the password.
func (s *VerificationService) Resend(ctx context.Context, tenant model.Tenant, destination, ip string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), verificationDeliveryTimeout)
	defer cancel()

	destination = strings.TrimSpace(destination)
	if destination == "" {
		return
	}

	q := s.store.ForTenant(tenant.ID)

	// Resolved against the contact columns, never the union that sign-in
	// uses. If one account's email equals another's username, a union lookup
	// would send that account's verification to whoever typed the address.
	row, err := q.GetUserByEmail(ctx, destination)
	if err != nil {
		row, err = q.GetUserByPhone(ctx, destination)
	}
	if err != nil {
		s.audit.Log(ctx, tenant.ID, AuditEntry{
			Kind: model.LogRegistration, Action: model.ActionVerificationSent,
			Result: model.LogFailure,
			Detail: "no account for a verification resend", IP: ip,
		})
		return
	}

	// Already verified, or not the kind of account this applies to. Both
	// answer the caller identically; the trail records which.
	if row.VerifiedAt != nil || model.UserSource(row.Source) != model.SourceRegistration {
		s.audit.Log(ctx, tenant.ID, AuditEntry{
			Kind: model.LogRegistration, Action: model.ActionVerificationSent,
			Result:  model.LogFailure,
			ActorID: row.ID, ActorName: row.Username,
			Detail: "nothing to verify", IP: ip,
		})
		return
	}

	channel, ok := s.channelFor(row)
	if !ok {
		slog.WarnContext(ctx, "verification resend with no deliverable channel",
			"tenant", tenant.Code)
		return
	}

	token, err := s.issue(ctx, q, row, channel)
	if err != nil {
		slog.ErrorContext(ctx, "failed to issue a verification token",
			"tenant", tenant.Code, "error", err)
		return
	}
	if err := s.deliver(ctx, tenant, channel, row, token); err != nil {
		slog.ErrorContext(ctx, "failed to deliver a verification message",
			"channel", channel, "tenant", tenant.Code, "error", err)
		return
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionVerificationSent,
		ActorID: row.ID, ActorName: row.Username,
		TargetType: "USER", TargetID: row.ID, TargetName: row.Username,
		Detail: fmt.Sprintf("resent over %s", channel), IP: ip,
	})
}

// Confirm redeems a token and marks the address proven.
func (s *VerificationService) Confirm(ctx context.Context, tenantID, token, ip string) error {
	if strings.TrimSpace(token) == "" {
		return ErrInvalidVerificationToken
	}

	q := s.store.ForTenant(tenantID)

	// Spent in the same statement it is read, so the same link cannot be
	// redeemed twice by two requests arriving together.
	record, err := q.ConsumeRegistrationVerification(ctx, hashToken(token), store.Now())
	if err != nil {
		return ErrInvalidVerificationToken
	}

	now := store.Now()
	if err := q.MarkUserVerified(ctx, record.UserID, now); err != nil {
		return fmt.Errorf("mark verified: %w", err)
	}

	user, err := s.users.Get(ctx, tenantID, record.UserID)
	if err != nil {
		return err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionVerificationConfirm,
		ActorID: user.ID, ActorName: user.Username,
		TargetType: "USER", TargetID: user.ID, TargetName: user.Username,
		IP: ip,
	})
	return nil
}

func (s *VerificationService) issue(ctx context.Context, q *store.Scoped, row sqlcgen.User, channel model.RecoveryChannel) (string, error) {
	token, hash, err := newVerificationToken()
	if err != nil {
		return "", fmt.Errorf("generate verification token: %w", err)
	}

	now := store.Now()
	// Asking again invalidates the previous link, so only the newest message
	// works and an older one somebody else has read does not.
	if err := q.SupersedeRegistrationVerifications(ctx, row.ID, now); err != nil {
		return "", fmt.Errorf("supersede verifications: %w", err)
	}
	err = q.CreateRegistrationVerification(ctx, sqlcgen.CreateRegistrationVerificationParams{
		ID:        uuid.NewString(),
		UserID:    row.ID,
		TokenHash: hash,
		Channel:   string(channel),
		ExpiresAt: now.Add(VerificationTokenTTL),
		CreatedAt: now,
	})
	if err != nil {
		return "", fmt.Errorf("record verification: %w", err)
	}
	return token, nil
}

func (s *VerificationService) deliver(ctx context.Context, tenant model.Tenant, channel model.RecoveryChannel, row sqlcgen.User, token string) error {
	link := s.verifyLink(tenant.Code, token)
	hours := int(VerificationTokenTTL.Hours())

	locale := s.settings.MessageLocale(ctx, tenant.ID, row.PreferredLanguage)
	data := i18n.VerificationData{
		Tenant:   tenant.Name,
		Name:     row.DisplayName,
		Username: row.Username,
		Link:     link,
		Hours:    hours,
	}

	switch channel {
	case model.RecoveryEmail:
		subject, err := s.messages.Render(locale, i18n.KeyVerificationEmailSubject, data)
		if err != nil {
			return err
		}
		body, err := s.messages.Render(locale, i18n.KeyVerificationEmailBody, data)
		if err != nil {
			return err
		}
		return s.mailer.Send(ctx, notify.Message{
			// The account's stored address, never a submitted one. They are
			// equal here by construction; taking it from the row is what
			// keeps that true if the lookup ever changes.
			To: row.Email, Subject: subject, Body: body,
		})
	case model.RecoverySMS:
		text, err := s.messages.Render(locale, i18n.KeyVerificationSMS, data)
		if err != nil {
			return err
		}
		return s.sms.Send(ctx, row.Phone, text)
	}
	return ErrVerificationUnavailable
}

// verifyLink builds the address in the message.
//
// From PublicURL rather than the request, for the same reason a reset link
// is: behind a proxy the Host header is whatever that proxy sends, and
// building a link from it would let anyone who can reach the server choose
// the domain it points at.
func (s *VerificationService) verifyLink(tenantCode, token string) string {
	base := strings.TrimRight(s.publicURL, "/")
	query := url.Values{"token": {token}}
	if tenantCode != "" && tenantCode != model.DefaultTenantCode {
		query.Set("tenant", tenantCode)
	}
	return base + "/verify?" + query.Encode()
}

func newVerificationToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
