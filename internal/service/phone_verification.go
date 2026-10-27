package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// PhoneVerificationCodeTTL is how long a phone-ownership code stays usable.
//
// Longer than SMSLoginCodeTTL's five minutes: this one is read from inside
// an already-open profile screen rather than typed straight back into a
// sign-in form, and a person switching to their messaging app and back
// should not lose the window.
const PhoneVerificationCodeTTL = 10 * time.Minute

// PhoneVerificationMaxAttempts is how many wrong guesses one code
// tolerates, same reasoning as SMSLoginMaxAttempts.
const PhoneVerificationMaxAttempts = 5

// PhoneVerificationPerAccountPerDay is how many verification codes one
// account may request in a day, regardless of which numbers they targeted.
//
// The thing this bounds is not enumeration -- the caller is already a known,
// signed-in account, so RequestPhoneChange refuses a number already claimed
// by somebody else synchronously (ErrPhoneTaken) rather than staying quiet
// about it, the way SMS login's phone lookup has to. What this bounds
// instead is a compromised or malicious account being used to nuisance-text
// arbitrary real phone numbers that are not yet claimed by anybody: every
// send here goes to an unclaimed number by construction, so the daily cap
// is the only thing standing between one account and an unbounded number of
// strangers. Five, the same figure recovery uses for the same reason
// (RecoveryPerAccountPerDay) and no separate cooldown or per-phone counter,
// deliberately: unlike SMS login, there is exactly one account behind every
// request here, so a plain per-account daily count is the whole rule.
const PhoneVerificationPerAccountPerDay = 5

// ErrPhoneVerificationUnavailable means this deployment has not configured
// SMS delivery, so a phone number cannot be proven.
var ErrPhoneVerificationUnavailable = httpx.NewError(503, "PHONE_VERIFICATION_UNAVAILABLE",
	"Verifying a phone number is not available. Ask an administrator to configure SMS delivery.")

// ErrInvalidPhoneVerificationCode covers every way ConfirmPhoneChange can
// refuse: wrong code, expired code, and too many wrong attempts. All three
// return the identical sentinel -- there is no enumeration concern to
// justify distinguishing them (the caller already knows which phone they
// are trying to confirm), but a caller correcting a typo does not need to
// know whether the code aged out mid-retry or was simply wrong.
var ErrInvalidPhoneVerificationCode = httpx.UnprocessableEntity("INVALID_PHONE_VERIFICATION_CODE",
	"That code is incorrect or has expired.")

// ErrPhoneUnchanged means the submitted number is already this account's.
var ErrPhoneUnchanged = httpx.BadRequest("PHONE_UNCHANGED",
	"That is already this account's phone number.")

// PhoneVerificationService proves a signed-in user controls a phone number
// before self_service.go's UpdateOwnProfile is allowed to write it.
//
// A separate service rather than more methods on UserService for the same
// reason RecoveryService and VerificationService are: it has its own
// dependency on notify.SMSSender and its own request/confirm lifecycle, and
// folding it into UserService would give every caller of that service a
// dependency it does not need.
type PhoneVerificationService struct {
	store    *store.Store
	users    *UserService
	settings *SettingsService
	audit    *AuditService
	sms      notify.SMSSender
}

// NewPhoneVerificationService wires a PhoneVerificationService.
func NewPhoneVerificationService(
	st *store.Store, users *UserService, settings *SettingsService,
	audit *AuditService, sms notify.SMSSender,
) *PhoneVerificationService {
	return &PhoneVerificationService{store: st, users: users, settings: settings, audit: audit, sms: sms}
}

// RequestPhoneChange sends actor a code proving they control phone, the
// first half of replacing their profile's phone number (§3.5).
//
// Unlike SMSLoginService.RequestCode, this is fully synchronous: the caller
// is already an authenticated account, so there is nothing left to hide by
// detaching the work. A delivery failure is reported as one, not swallowed.
func (s *PhoneVerificationService) RequestPhoneChange(ctx context.Context, actor auth.Principal, phone, ip string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return httpx.BadRequest("MISSING_PHONE", "A phone number is required.")
	}
	if err := validateContactDetails(phone, ""); err != nil {
		return err
	}
	if !s.settings.CanDeliverSMS() {
		return ErrPhoneVerificationUnavailable
	}

	q := s.store.ForTenant(actor.TenantID)

	current, err := q.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if current.Phone == phone {
		return ErrPhoneUnchanged
	}

	// Refused synchronously rather than folded into a silent send: the
	// caller is a known account, not an anonymous one, so there is no
	// anti-enumeration reason to stay quiet -- and staying quiet here would
	// mean sending a real text message to a stranger's already-verified
	// phone number every time somebody fat-fingers or guesses one. Every
	// other place this app assigns a phone number (registration, an
	// administrator's edit) already answers PHONE_TAKEN the same way.
	if _, err := q.GetUserByPhone(ctx, phone); err == nil {
		return ErrPhoneTaken
	} else if !store.IsNoRows(err) {
		return fmt.Errorf("look up phone: %w", err)
	}

	sent, err := q.CountRecentPhoneVerificationCodes(ctx, actor.UserID, store.Now().Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("count recent phone verification codes: %w", err)
	}
	if sent >= PhoneVerificationPerAccountPerDay {
		s.audit.Log(ctx, actor.TenantID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionPhoneVerificationSent,
			Result:  model.LogFailure,
			ActorID: actor.UserID, ActorName: actor.Username,
			Detail: fmt.Sprintf("daily phone verification code limit reached (%d in 24h)", sent),
			IP:     ip,
		})
		return httpx.TooManyRequests("TOO_MANY_ATTEMPTS",
			"Too many verification codes requested today. Try again tomorrow.")
	}

	code, hash, err := newPhoneVerificationCode()
	if err != nil {
		return fmt.Errorf("generate phone verification code: %w", err)
	}
	now := store.Now()
	if err := q.CreatePhoneVerificationCode(ctx, sqlcgen.CreatePhoneVerificationCodeParams{
		ID: uuid.NewString(), UserID: actor.UserID, Phone: phone,
		CodeHash: hash, ExpiresAt: now.Add(PhoneVerificationCodeTTL), CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("record phone verification code: %w", err)
	}

	if err := s.sms.Send(ctx, phone, notify.SMSKindPhoneVerification, map[string]string{
		"Code":    code,
		"Minutes": strconv.Itoa(int(PhoneVerificationCodeTTL.Minutes())),
	}); err != nil {
		return fmt.Errorf("send phone verification code: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPhoneVerificationSent,
		ActorID: actor.UserID, ActorName: actor.Username,
		Detail: fmt.Sprintf("code sent to %q", phone), IP: ip,
	})
	return nil
}

// ConfirmPhoneChange verifies a code and, if it is right, writes phone into
// actor's profile -- the second half of §3.5.
func (s *PhoneVerificationService) ConfirmPhoneChange(ctx context.Context, actor auth.Principal, phone, code, ip string) (model.User, error) {
	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	if phone == "" || code == "" {
		return model.User{}, httpx.BadRequest("MISSING_CODE", "A phone number and code are required.")
	}

	q := s.store.ForTenant(actor.TenantID)
	now := store.Now()

	row, err := q.GetLivePhoneVerificationCode(ctx, actor.UserID, phone, now)
	if err != nil {
		if store.IsNoRows(err) {
			return model.User{}, ErrInvalidPhoneVerificationCode
		}
		return model.User{}, fmt.Errorf("look up phone verification code: %w", err)
	}

	if row.Attempts >= PhoneVerificationMaxAttempts || hashPhoneVerificationCode(code) != row.CodeHash {
		if row.Attempts < PhoneVerificationMaxAttempts {
			_ = q.IncrementPhoneVerificationCodeAttempts(ctx, row.ID)
		}
		s.audit.Log(ctx, actor.TenantID, AuditEntry{
			Kind: model.LogOperation, Action: model.ActionPhoneVerificationConfirm,
			Result:  model.LogFailure,
			ActorID: actor.UserID, ActorName: actor.Username,
			Detail: "wrong or expired phone verification code", IP: ip,
		})
		return model.User{}, ErrInvalidPhoneVerificationCode
	}

	current, err := q.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return model.User{}, fmt.Errorf("get user: %w", err)
	}

	if err := q.ConsumePhoneVerificationCode(ctx, row.ID, now); err != nil {
		return model.User{}, fmt.Errorf("consume phone verification code: %w", err)
	}

	// The unique index is the real guarantee against a race with another
	// account claiming this number between Request and Confirm; this turns
	// that race into the same ErrPhoneTaken a caller already knows how to
	// handle rather than a raw constraint-violation error.
	if err := q.UpdateUserProfile(ctx, sqlcgen.UpdateUserProfileParams{
		ID: actor.UserID, DisplayName: current.DisplayName, Phone: phone,
		Email: current.Email, OrganizationID: current.OrganizationID,
		Role: current.Role, UpdatedAt: now,
	}); err != nil {
		if taken := takenFieldError(err); taken != nil {
			return model.User{}, taken
		}
		return model.User{}, fmt.Errorf("update profile: %w", err)
	}

	updated, err := s.users.Get(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return model.User{}, err
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionPhoneVerificationConfirm,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: actor.UserID, TargetName: actor.Username,
		Detail: describeContactChange(current, updated), IP: ip,
	})

	return updated, nil
}

// newPhoneVerificationCode generates a 6-digit code and its stored hash,
// the same shape as newSMSLoginCode.
func newPhoneVerificationCode() (code, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", fmt.Errorf("generate phone verification code: %w", err)
	}
	code = fmt.Sprintf("%06d", n.Int64())
	return code, hashPhoneVerificationCode(code), nil
}

// hashPhoneVerificationCode is a plain SHA-256, not a password hash -- same
// reasoning as hashSMSLoginCode.
func hashPhoneVerificationCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
