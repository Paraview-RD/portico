package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/notify"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

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
	store  *store.Store
	users  *UserService
	audit  *AuditService
	mailer notify.Mailer
	sms    notify.SMSSender
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
	mailer notify.Mailer,
	sms notify.SMSSender,
	publicURL string,
) *RecoveryService {
	return &RecoveryService{
		store: st, users: users, audit: audit,
		mailer: mailer, sms: sms,
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

	if err != nil {
		if store.IsNoRows(err) {
			// No account, or one with nothing bound on this channel. Recorded
			// so a burst of these is visible to whoever reads the trail, and
			// answered exactly as a success is.
			s.audit.Log(ctx, tenant.ID, AuditEntry{
				Kind: model.LogOperation, Action: model.ActionPasswordRecoveryRequest,
				Result: model.LogFailure,
				Detail: fmt.Sprintf("no account for a %s recovery request", channel),
				IP:     ip,
			})
			return nil
		}
		return fmt.Errorf("resolve recovery destination: %w", err)
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
		return nil
	}

	token, hash, err := newRecoveryToken()
	if err != nil {
		return err
	}

	now := store.Now()
	// Asking again invalidates the previous link, so only the most recent
	// message works and an older one someone else has read does not.
	if err := q.SupersedePasswordResets(ctx, row.ID, now); err != nil {
		return fmt.Errorf("supersede outstanding resets: %w", err)
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
		return fmt.Errorf("record password reset: %w", err)
	}

	// The destination is the account's stored field, never the submitted
	// one. They are equal here by construction, but taking it from the row
	// is what keeps that true if the lookup ever changes.
	if err := s.deliver(ctx, tenant, channel, row, token); err != nil {
		return err
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPasswordRecoveryRequest,
		ActorID: row.ID, ActorName: row.Username,
		TargetType: "USER", TargetID: row.ID, TargetName: row.Username,
		Detail: fmt.Sprintf("sent over %s", channel), IP: ip,
	})
	return nil
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
	if err := s.users.setPassword(ctx, q, reset.UserID, newPassword); err != nil {
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

func (s *RecoveryService) deliver(ctx context.Context, tenant model.Tenant, channel model.RecoveryChannel, row sqlcgen.User, token string) error {
	link := s.resetLink(tenant.Code, token)

	var err error
	switch channel {
	case model.RecoveryEmail:
		err = s.mailer.Send(ctx, notify.Message{
			To:      row.Email,
			Subject: fmt.Sprintf("Reset your %s password", tenant.Name),
			Body: fmt.Sprintf(`Hello %s,

Someone asked to reset the password for your account (%s). Open this link
to choose a new one:

%s

The link works once and expires in %d minutes. If this was not you, no
action is needed — the password has not changed.
`, row.DisplayName, row.Username, link, int(RecoveryTokenTTL.Minutes())),
		})
	case model.RecoverySMS:
		err = s.sms.Send(ctx, row.Phone, fmt.Sprintf(
			"Reset your %s password: %s (expires in %d minutes)",
			tenant.Name, link, int(RecoveryTokenTTL.Minutes())))
	}

	if err != nil {
		if errors.Is(err, notify.ErrNotConfigured) {
			return ErrRecoveryUnavailable
		}
		// The token is already recorded, so a delivery failure leaves an
		// unusable row that expires on its own. The caller is told the
		// request failed rather than being left to wait for a message that
		// is not coming — and the reason stays in the process log, since it
		// is about the deployment's mail relay and not about them.
		slog.ErrorContext(ctx, "failed to deliver a password recovery message",
			"channel", channel, "tenant", tenant.Code, "error", err)
		return httpx.NewError(502, "RECOVERY_DELIVERY_FAILED",
			"The message could not be sent. Try again, or contact an administrator.")
	}
	return nil
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
