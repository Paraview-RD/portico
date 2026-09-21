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

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// SMSLoginCodeTTL is how long a code stays usable.
//
// Short, because unlike a password-reset link this is not sitting in a
// mailbox waiting for a person to get back to their laptop -- it is typed
// back into the same form within seconds.
const SMSLoginCodeTTL = 5 * time.Minute

// SMSLoginMaxAttempts is how many wrong guesses one code tolerates before
// it is dead, without waiting for it to expire on its own.
const SMSLoginMaxAttempts = 5

// SMSLoginCooldown is the minimum spacing between two codes sent to the
// same phone number, enforced in memory -- see internal/httpx.DayLimiter's
// doc comment and docs/superpowers/specs/2026-09-21-sms-otp-login-design.md
// §3.2 for why this is not a database column.
const SMSLoginCooldown = 60 * time.Second

// SMSLoginPerPhonePerDay, SMSLoginPerIPPerDay, and
// SMSLoginPerDeploymentPerDay are the three daily caps, all in-memory for
// the same reason as the cooldown. Per-phone and per-deployment mirror
// RecoveryPerAccountPerDay's reasoning: this spends a shared sending quota
// and the sender's reputation. Per-phone deliberately aggregates *across
// tenants* -- see SMSLoginService.RequestCode.
const (
	SMSLoginPerPhonePerDay      = 5
	SMSLoginPerIPPerDay         = 20
	SMSLoginPerDeploymentPerDay = 1000
)

// smsLoginBudgetKey is the constant key SMSLoginService's deployment-wide
// daily limiter is checked against -- there is exactly one such budget, so
// there is exactly one key.
const smsLoginBudgetKey = "deployment"

// SMSLoginService issues and verifies the codes behind the standalone
// phone-number + SMS-code login method (spec §2).
type SMSLoginService struct {
	store    *store.Store
	users    *UserService
	settings *SettingsService
	// audit is stored but not read by this file -- issueSessionForUser does
	// its own auditing on a successful sign-in, and there is no separate
	// audit trail for a code request/attempt yet. Kept because the
	// constructor signature is contracted with Task 11's wiring.
	audit   *AuditService
	metrics *metrics.Registry
	sms     notify.SMSSender

	cooldown            *httpx.RateLimiter
	perPhonePerDay      *httpx.DayLimiter
	perIPPerDay         *httpx.DayLimiter
	perDeploymentPerDay *httpx.DayLimiter
}

// NewSMSLoginService wires up a code-issuing service. perDeploymentPerDay
// lets a deployment tune SMSLoginPerDeploymentPerDay via configuration;
// pass 0 to use the default.
func NewSMSLoginService(
	st *store.Store, users *UserService, settings *SettingsService,
	audit *AuditService, registry *metrics.Registry, sms notify.SMSSender,
	perDeploymentPerDay int,
) *SMSLoginService {
	if perDeploymentPerDay <= 0 {
		perDeploymentPerDay = SMSLoginPerDeploymentPerDay
	}
	return &SMSLoginService{
		store: st, users: users, settings: settings, audit: audit, metrics: registry, sms: sms,
		cooldown:            httpx.NewRateLimiter(1, 1), // one per minute, burst one == SMSLoginCooldown (60s) per key -- the constant documents the policy, this call implements it; keep them in sync if either changes
		perPhonePerDay:      httpx.NewDayLimiter(SMSLoginPerPhonePerDay, 24*time.Hour),
		perIPPerDay:         httpx.NewDayLimiter(SMSLoginPerIPPerDay, 24*time.Hour),
		perDeploymentPerDay: httpx.NewDayLimiter(perDeploymentPerDay, 24*time.Hour),
	}
}

// ErrSMSLoginUnavailable means this deployment or this tenant has not
// turned SMS login on.
var ErrSMSLoginUnavailable = httpx.NewError(503, "SMS_LOGIN_UNAVAILABLE",
	"SMS login is not available. Ask an administrator to enable it, or sign in with a password.")

// ErrInvalidSMSCode covers every way LoginWithCode can refuse: wrong code,
// expired code, too many wrong attempts, and no matching account. All four
// return the identical sentinel deliberately -- see the doc comment on
// LoginWithCode for why distinguishing them would be an enumeration oracle.
var ErrInvalidSMSCode = httpx.Unauthorized("INVALID_SMS_CODE", "That code is incorrect or has expired.")

// smsLoginDeliveryTimeout bounds the work that continues after the
// response, same reasoning as recoveryDeliveryTimeout.
const smsLoginDeliveryTimeout = 30 * time.Second

// RequestCode asks for a login code to be sent to phone.
//
// Reveals nothing about whether phone belongs to an account -- the lookup
// and the send both happen after this returns (spec §2.1). The two
// synchronous checks below (deployment/day, IP/day) are safe to refuse
// distinctly: neither depends on which phone was typed, so a 429 from them
// says nothing about whether an account exists. The phone-scoped checks
// (cooldown, phone/day) are NOT safe to refuse distinctly -- doing so would
// let a caller learn "this phone recently received a code" == "this phone
// has an account" -- so they are folded into the same silent path as the
// account lookup itself, inside completeRequest.
func (s *SMSLoginService) RequestCode(ctx context.Context, tenant model.Tenant, phone, ip string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return httpx.BadRequest("MISSING_PHONE", "A phone number is required.")
	}

	settings, err := s.settings.Get(ctx, tenant.ID)
	if err != nil {
		return err
	}
	if !settings.SMSLoginEnabled || !s.settings.CanDeliverSMS() {
		return ErrSMSLoginUnavailable
	}

	// IP-scoped first: Allow both checks and consumes a token on success, so
	// checking the deployment-wide budget first would let one abusive IP
	// spend a shared token on every request even after its own cap should
	// have refused it.
	if !s.perIPPerDay.Allow(ip) {
		return httpx.TooManyRequests("TOO_MANY_ATTEMPTS",
			"Too many requests from this address today. Try again tomorrow.")
	}
	if !s.perDeploymentPerDay.Allow(smsLoginBudgetKey) {
		return httpx.TooManyRequests("TOO_MANY_ATTEMPTS",
			"This deployment has reached its daily SMS limit. Try again tomorrow.")
	}

	go s.completeRequest(context.WithoutCancel(ctx), tenant, phone, ip)
	return nil
}

// completeRequest does the part that must stay invisible to the caller:
// the phone-scoped rate limits, the account lookup, and the send.
func (s *SMSLoginService) completeRequest(ctx context.Context, tenant model.Tenant, phone, ip string) {
	ctx, cancel := context.WithTimeout(ctx, smsLoginDeliveryTimeout)
	defer cancel()

	// Phone-scoped limits are checked here, not in RequestCode, precisely
	// because a synchronous refusal would disclose that this phone has
	// received a code recently -- which discloses that it is bound to an
	// account. Silently declining to send is indistinguishable from a
	// phone number nobody has ever typed.
	if allowed, _ := s.cooldown.Allow(phone); !allowed {
		return
	}
	if !s.perPhonePerDay.Allow(phone) {
		return
	}

	q := s.store.ForTenant(tenant.ID)
	if _, err := q.GetUserByPhone(ctx, phone); err != nil {
		return // no such phone in this tenant; nothing to send
	}

	code, hash, err := newSMSLoginCode()
	if err != nil {
		return
	}
	now := store.Now()
	if err := q.CreateSMSLoginCode(ctx, sqlcgen.CreateSMSLoginCodeParams{
		ID: uuid.NewString(), Phone: phone, CodeHash: hash,
		ExpiresAt: now.Add(SMSLoginCodeTTL), CreatedAt: now, Ip: ip,
	}); err != nil {
		return
	}

	_ = s.sms.Send(ctx, phone, notify.SMSKindLoginCode, map[string]string{
		"Code":    code,
		"Minutes": strconv.Itoa(int(SMSLoginCodeTTL.Minutes())),
	})
}

// LoginWithCode verifies a code and, if it is right, signs the phone's
// account in.
//
// Wrong code, expired code, too-many-attempts, and no-such-phone all
// return the same ErrInvalidSMSCode (spec §2.1) -- see
// UserService.Login's doc comment for why a sign-in path answers this way.
// Only once the code itself checks out does the account's own state
// (locked/disabled/closed) get its own answer, through
// UserService.issueSessionForUser.
func (s *SMSLoginService) LoginWithCode(ctx context.Context, tenant model.Tenant, phone, code, ip, userAgent string) (session Session, err error) {
	defer func() { s.metrics.RecordSignIn(signInOutcome(err)) }()

	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	if phone == "" || code == "" {
		return Session{}, httpx.BadRequest("MISSING_CREDENTIALS", "A phone number and code are required.")
	}

	q := s.store.ForTenant(tenant.ID)
	now := store.Now()

	row, err := q.GetLiveSMSLoginCode(ctx, phone, now)
	if err != nil {
		if store.IsNoRows(err) {
			s.users.logLoginFailure(ctx, tenant.ID, "", phone, ip, "no live sms code")
			return Session{}, ErrInvalidSMSCode
		}
		return Session{}, fmt.Errorf("look up sms login code: %w", err)
	}

	if row.Attempts >= SMSLoginMaxAttempts || hashSMSLoginCode(code) != row.CodeHash {
		if row.Attempts < SMSLoginMaxAttempts {
			_ = q.IncrementSMSLoginCodeAttempts(ctx, row.ID)
		}
		s.users.logLoginFailure(ctx, tenant.ID, "", phone, ip, "wrong sms code")
		return Session{}, ErrInvalidSMSCode
	}

	user, err := q.GetUserByPhone(ctx, phone)
	if err != nil {
		if store.IsNoRows(err) {
			s.users.logLoginFailure(ctx, tenant.ID, "", phone, ip, "sms code correct but no matching account")
			return Session{}, ErrInvalidSMSCode
		}
		return Session{}, fmt.Errorf("look up user by phone: %w", err)
	}

	if err := q.ConsumeSMSLoginCode(ctx, row.ID, now); err != nil {
		return Session{}, fmt.Errorf("consume sms login code: %w", err)
	}

	return s.users.issueSessionForUser(ctx, tenant, user, ip, userAgent, "sms code")
}

// newSMSLoginCode generates a 6-digit code and its stored hash.
func newSMSLoginCode() (code, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", fmt.Errorf("generate sms login code: %w", err)
	}
	code = fmt.Sprintf("%06d", n.Int64())
	return code, hashSMSLoginCode(code), nil
}

// hashSMSLoginCode is a plain SHA-256, not a password hash -- see the
// CREATE TABLE comment on sms_login_codes.code_hash for why that is the
// right choice at this entropy.
func hashSMSLoginCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
