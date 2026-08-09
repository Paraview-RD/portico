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

// recoveryDeliveryTimeout bounds the work that continues after the response.
// A relay that accepts a connection and then stops talking would otherwise
// hold a goroutine indefinitely.
const recoveryDeliveryTimeout = 30 * time.Second

// RecoveryTokenTTL is how long a reset link stays usable.
//
// Short, because the token is a password equivalent for its lifetime and it
// sits in a mailbox. Long enough that someone who steps away between asking
// and reading does not have to ask again.
const RecoveryTokenTTL = 30 * time.Minute

// Errors from password recovery.
var (
	ErrRecoveryUnavailable = httpx.NewError(503, "RECOVERY_UNAVAILABLE",
		"Password recovery over that channel is not configured on this deployment.")
	ErrInvalidResetToken = httpx.UnprocessableEntity("INVALID_RESET_TOKEN",
		"That reset link is invalid, already used, or has expired. Request a new one.")
)

// RecoveryService issues and redeems password-reset tokens (§3.5).
type RecoveryService struct {
	store *store.Store
	users *UserService
	audit *AuditService
	// settings is here for one thing: which language to write in. The
	// account's own preference decides it where there is one, and this is
	// where the tenant's answer comes from when there is not.
	settings *SettingsService
	messages *i18n.Catalog
	mailer   notify.Mailer
	sms      notify.SMSSender
	// publicURL is where a person reaches this deployment, which the server
	// cannot work out for itself: it sits behind a proxy that rewrites the
	// host, so the request's own headers are whatever that proxy sends.
	publicURL string
}

// NewRecoveryService wires a RecoveryService.
func NewRecoveryService(
	st *store.Store,
	users *UserService,
	audit *AuditService,
	settings *SettingsService,
	mailer notify.Mailer,
	sms notify.SMSSender,
	publicURL string,
) *RecoveryService {
	return &RecoveryService{
		store: st, users: users, audit: audit, settings: settings,
		messages: i18n.MustLoad(),
		mailer:   mailer, sms: sms,
		publicURL: strings.TrimSuffix(publicURL, "/"),
	}
}

// Request starts password recovery for whoever holds destination in tenant.
//
// It returns nil whether or not an account was found. Reporting the
// difference would turn this endpoint into an oracle for "does this person
// have an account here", which for an identity server is a disclosure in its
// own right — and the same neutrality has to hold for all three misses: no
// such account, an account with nothing bound on that channel, and a
// successful send. The only failure a caller sees is the deployment having
// no provider at all, which is about the deployment rather than about them.
//
// The account is resolved against the channel's own column. Sign-in resolves
// an identifier across all three, and reusing that here would be an account
// takeover: if one account's email equals another's username, the
// username holder wins the union lookup, and a reset token for their account
// would be sent to whoever typed that address.
func (s *RecoveryService) Request(ctx context.Context, tenant model.Tenant, channel model.RecoveryChannel, destination, ip string) error {
	destination = strings.TrimSpace(destination)

	if err := s.channelAvailable(channel); err != nil {
		return err
	}
	if destination == "" {
		return httpx.BadRequest("MISSING_DESTINATION",
			"An email address or phone number is required.")
	}

	q := s.store.ForTenant(tenant.ID)

	var (
		row sqlcgen.User
		err error
	)
	switch channel {
	case model.RecoveryEmail:
		row, err = q.GetUserByEmail(ctx, destination)
	case model.RecoverySMS:
		row, err = q.GetUserByPhone(ctx, destination)
	default:
		return httpx.BadRequest("INVALID_CHANNEL", "Channel must be EMAIL or SMS.")
	}

	if err != nil && !store.IsNoRows(err) {
		return fmt.Errorf("resolve recovery destination: %w", err)
	}

	// Everything past the lookup happens after the response, including the
	// writes. That is what makes the answer say nothing.
	//
	// Comparing response bodies is not enough: a hit does two writes and an
	// SMTP dial, a miss does nothing, and the difference is seconds rather
	// than microseconds — measurable from anywhere. A failure on either
	// would be worse still, because a 500 or a 502 is only reachable once an
	// account has been found, which turns a flaky relay into an enumeration
	// oracle. Detaching all of it leaves exactly one indexed SELECT between
	// the request and the reply, whether or not it matched.
	//
	// The cost is that a delivery or storage failure can no longer be
	// reported to the caller. That is the right trade: "we could not send
	// it" is information this endpoint has already decided not to give, and
	// the reason belongs in the process log where an operator will see it.
	found := err == nil
	go s.completeRequest(context.WithoutCancel(ctx), tenant, channel, row, found, ip)

	return nil
}

// completeRequest does the part of a recovery request that must not be
// visible in the response: the audit entry, the token, and delivery.
//
// It runs on a detached context so that a caller closing the tab — which is
// exactly what someone does after submitting a form they expect an email
// from — does not lose the audit entry or the message.
func (s *RecoveryService) completeRequest(ctx context.Context, tenant model.Tenant, channel model.RecoveryChannel, row sqlcgen.User, found bool, ip string) {
	ctx, cancel := context.WithTimeout(ctx, recoveryDeliveryTimeout)
	defer cancel()

	if !found {
		// No account, or one with nothing bound on this channel. Recorded so
		// a burst of these is visible to whoever reads the trail.
		s.audit.Log(ctx, tenant.ID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionPasswordRecoveryRequest,
			Result: model.LogFailure,
			Detail: fmt.Sprintf("no account for a %s recovery request", channel),
			IP:     ip,
		})
		return
	}

	if model.Status(row.Status) != model.StatusActive {
		// A disabled account is not recoverable — resetting the password
		// would not make it usable, and sending a link implies it would.
		s.audit.Log(ctx, tenant.ID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionPasswordRecoveryRequest,
			Result:  model.LogFailure,
			ActorID: row.ID, ActorName: row.Username,
			Detail: "account is disabled", IP: ip,
		})
		return
	}

	q := s.store.ForTenant(tenant.ID)

	token, hash, err := newRecoveryToken()
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate a password recovery token", "error", err)
		return
	}

	now := store.Now()
	// Asking again invalidates the previous link, so only the most recent
	// message works and an older one someone else has read does not.
	if err := q.SupersedePasswordResets(ctx, row.ID, now); err != nil {
		slog.ErrorContext(ctx, "failed to supersede outstanding password resets",
			"tenant", tenant.Code, "error", err)
		return
	}
	err = q.CreatePasswordReset(ctx, sqlcgen.CreatePasswordResetParams{
		ID:        uuid.NewString(),
		UserID:    row.ID,
		TokenHash: hash,
		Channel:   string(channel),
		ExpiresAt: now.Add(RecoveryTokenTTL),
		CreatedAt: now,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to record a password reset",
			"tenant", tenant.Code, "error", err)
		return
	}

	// The destination is the account's stored field, never the submitted
	// one. They are equal here by construction, but taking it from the row
	// is what keeps that true if the lookup ever changes.
	if err := s.deliver(ctx, tenant, channel, row, token); err != nil {
		slog.ErrorContext(ctx, "failed to deliver a password recovery message",
			"channel", channel, "tenant", tenant.Code, "error", err)
		return
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordRecoveryRequest,
		ActorID: row.ID, ActorName: row.Username,
		TargetType: "USER", TargetID: row.ID, TargetName: row.Username,
		Detail: fmt.Sprintf("sent over %s", channel), IP: ip,
	})
}

// Confirm redeems a reset token and sets a new password.
//
// Every way of failing — unknown token, spent token, expired token — returns
// the same error. Distinguishing them would let someone with a stolen but
// expired link learn that it was once real, and there is nothing a
// legitimate user does differently in the three cases: they request another.
func (s *RecoveryService) Confirm(ctx context.Context, tenantID, token, newPassword, ip string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidResetToken
	}

	q := s.store.ForTenant(tenantID)

	// The query itself requires unspent and unexpired, so there is no way to
	// hold a dead row and forget to check it.
	reset, err := q.GetLivePasswordReset(ctx, hashRecoveryToken(token), store.Now())
	if err != nil {
		if store.IsNoRows(err) {
			return ErrInvalidResetToken
		}
		return fmt.Errorf("look up password reset: %w", err)
	}

	target, err := q.GetUserByID(ctx, reset.UserID)
	if err != nil {
		if store.IsNoRows(err) {
			return ErrInvalidResetToken
		}
		return fmt.Errorf("get user: %w", err)
	}
	if model.Status(target.Status) != model.StatusActive {
		return ErrInvalidResetToken
	}

	// Setting the password bumps token_version, so completing a recovery
	// signs the account out everywhere. That is the desired behaviour and
	// not a side effect: if the reason for recovering was that someone else
	// had the password, their sessions have to end too.
	if err := s.users.setPassword(ctx, q, tenantID, reset.UserID, newPassword); err != nil {
		return err
	}
	if err := q.SpendPasswordReset(ctx, reset.ID, store.Now()); err != nil {
		return fmt.Errorf("spend password reset: %w", err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordRecoveryComplete,
		ActorID: target.ID, ActorName: target.Username,
		TargetType: "USER", TargetID: target.ID, TargetName: target.Username,
		Detail: fmt.Sprintf("completed over %s", reset.Channel), IP: ip,
	})
	return nil
}

// AvailableChannels reports which recovery channels this deployment can
// actually use, so the sign-in screen offers only those.
func (s *RecoveryService) AvailableChannels() []model.RecoveryChannel {
	channels := make([]model.RecoveryChannel, 0, 2)
	for _, channel := range []model.RecoveryChannel{model.RecoveryEmail, model.RecoverySMS} {
		if s.channelAvailable(channel) == nil {
			channels = append(channels, channel)
		}
	}
	return channels
}

func (s *RecoveryService) channelAvailable(channel model.RecoveryChannel) error {
	switch channel {
	case model.RecoveryEmail:
		if _, unconfigured := s.mailer.(notify.NotConfiguredMailer); unconfigured {
			return ErrRecoveryUnavailable
		}
	case model.RecoverySMS:
		if _, unconfigured := s.sms.(notify.NotConfiguredSMS); unconfigured {
			return ErrRecoveryUnavailable
		}
	default:
		return httpx.BadRequest("INVALID_CHANNEL", "Channel must be EMAIL or SMS.")
	}
	return nil
}

// deliver sends the message. Its error is for the process log — by the time
// it runs the caller has already been answered.
func (s *RecoveryService) deliver(ctx context.Context, tenant model.Tenant, channel model.RecoveryChannel, row sqlcgen.User, token string) error {
	link := s.resetLink(tenant.Code, token)

	// Written in the account's own language where it has one. Safe to depend
	// on the account here in a way it would not be earlier: a request for an
	// address nobody holds never reaches this function — completeRequest
	// returns before it — so the language of this message is only ever seen
	// by the person whose account it is. Choosing it from the row discloses
	// nothing to anyone else.
	locale := s.settings.MessageLocale(ctx, tenant.ID, row.PreferredLanguage)
	data := i18n.RecoveryData{
		Tenant:   tenant.Name,
		Name:     row.DisplayName,
		Username: row.Username,
		Link:     link,
		Minutes:  int(RecoveryTokenTTL.Minutes()),
	}

	var err error
	switch channel {
	case model.RecoveryEmail:
		subject, body, renderErr := s.render(locale,
			i18n.KeyRecoveryEmailSubject, i18n.KeyRecoveryEmailBody, data)
		if renderErr != nil {
			return renderErr
		}
		err = s.mailer.Send(ctx, notify.Message{
			To: row.Email, Subject: subject, Body: body,
		})
	case model.RecoverySMS:
		text, renderErr := s.messages.Render(locale, i18n.KeyRecoverySMS, data)
		if renderErr != nil {
			return renderErr
		}
		err = s.sms.Send(ctx, row.Phone, text)
	}

	// The token is already recorded, so a delivery failure leaves an unusable
	// row that expires on its own.
	return err
}

// resetLink builds the URL in the message.
//
// The tenant travels in the link so that redeeming the token is a
// tenant-scoped lookup like everything else. Without it the confirm endpoint
// would need to search every tenant for a matching token, which is exactly
// the unscoped query the isolation guards exist to prevent.
func (s *RecoveryService) resetLink(tenantCode, token string) string {
	query := url.Values{}
	query.Set("tenant", tenantCode)
	query.Set("token", token)
	return s.publicURL + "/reset-password?" + query.Encode()
}

// newRecoveryToken returns a token and the hash to store.
//
// 32 bytes from crypto/rand: the token is guessed or it is not, and at that
// width it is not. It is returned base64url so it survives a mail client's
// idea of what is part of a URL.
func newRecoveryToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate recovery token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashRecoveryToken(token), nil
}

// hashRecoveryToken is a plain SHA-256, not a password hash.
//
// The slow-hash argument for passwords is about low-entropy secrets people
// choose. This one is 256 random bits, so there is nothing to brute-force
// and a slow hash would only make redeeming a link slow.
func hashRecoveryToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// render produces a subject and a body together, so a message is never sent
// with one of them missing.
func (s *RecoveryService) render(locale i18n.Locale, subjectKey, bodyKey string, data any) (string, string, error) {
	subject, err := s.messages.Render(locale, subjectKey, data)
	if err != nil {
		return "", "", err
	}
	body, err := s.messages.Render(locale, bodyKey, data)
	if err != nil {
		return "", "", err
	}
	return subject, body, nil
}
