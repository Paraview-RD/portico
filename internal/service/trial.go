package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Self-service trials: a stranger proves an email address and gets a tenant.
//
// Everything else in this package authorizes inside a tenant, against somebody
// who has already signed in. This does neither, because neither exists yet —
// which is why it is reachable only where PORTICO_TRIAL_SIGNUP is on, and why
// the checks it does instead are written out one at a time below rather than
// inherited from a middleware.
//
// The address is the whole of the identity check. That is thin, and it is
// deliberately the only thing claimed: what it buys is that a tenant traces to
// a mailbox somebody could read, which is enough to make abuse attributable
// and to keep one visitor from opening fifty. It is not an assertion that they
// are who they say they work for.

// TrialIndustryGeneric is the one seeded world a trial may ask for today.
//
// The four industry packs are the next piece of work. The four industry packs are
// the next piece of work — manufacturing, banking, hospital, university, each
// with its own organization shape, custom attributes and application mix — and
// the column holding this is free text precisely so adding one needs no
// migration. Offering a name with nothing behind it would hand somebody an
// empty tenant and a wrong expectation, so the list is what exists.
const TrialIndustryGeneric = "generic"

func validTrialIndustry(name string) bool {
	return name == TrialIndustryGeneric
}

// TrialTokenTTL is how long a confirmation link stays usable.
//
// Shorter than a registration verification, which is a day: that one is a
// person finishing a signup on their own account, and coming back tomorrow is
// reasonable. This one holds a reserved tenant code, so an abandoned request
// costs a name somebody else might want.
const TrialTokenTTL = 2 * time.Hour

// trialsPerAddressPerDay bounds one client address over a day, which the
// per-minute rate limiter cannot see: fifty requests spread across an
// afternoon are inside every limit and are still one machine filling the quota.
const trialsPerAddressPerDay = 5

var (
	// ErrTrialSignupClosed is what every method answers when the deployment
	// has not enabled this. Registered routes are the real gate; this is the
	// backstop for a service constructed without one.
	ErrTrialSignupClosed = httpx.NotFound("TRIAL_SIGNUP_CLOSED",
		"This deployment does not offer self-service trials.")

	// ErrTrialQuotaReached is the shared demonstration being full. Said out
	// loud rather than swallowed: a visitor told to check their email waits
	// for a link that will never come.
	ErrTrialQuotaReached = httpx.Conflict("TRIAL_QUOTA_REACHED",
		"This demonstration is full. Try again later, or run Portico yourself.")

	// ErrTrialCodeTaken is the one failure a visitor can fix, which is why it
	// is reported before a link is sent rather than after.
	ErrTrialCodeTaken = httpx.Conflict("TRIAL_CODE_TAKEN",
		"That tenant code is already in use. Choose another.")

	// ErrTrialEmailUsed is one tenant per address, already spent.
	ErrTrialEmailUsed = httpx.Conflict("TRIAL_EMAIL_USED",
		"That address already has a trial tenant.")

	// ErrTrialTooManyFromAddress bounds one client address over a day, which
	// the per-minute throttle cannot see.
	ErrTrialTooManyFromAddress = httpx.TooManyRequests("TRIAL_TOO_MANY",
		"Too many trials requested from this address today.")

	// ErrTrialLinkInvalid is a token that names no request.
	ErrTrialLinkInvalid = httpx.BadRequest("TRIAL_LINK_INVALID",
		"That link is not valid. Request a new trial.")

	// ErrTrialLinkExpired is a link that outlived its two hours, and with it
	// the tenant code it was holding.
	ErrTrialLinkExpired = httpx.BadRequest("TRIAL_LINK_EXPIRED",
		"That link has expired. Request a new trial.")

	// ErrTrialLinkSpent is a second click. The credentials from the first are
	// valid, so this says to use them rather than reporting a broken link.
	ErrTrialLinkSpent = httpx.Conflict("TRIAL_LINK_SPENT",
		"That link has already been used. Sign in with the credentials it sent.")
)

// TrialService creates tenants for people who have no account anywhere.
type TrialService struct {
	store   *store.Store
	tenants *TenantService
	users   *UserService
	mailer  notify.Mailer
	audit   *AuditService

	enabled    bool
	maxTenants int
	publicURL  string

	// now is replaceable so a test can expire a link without waiting two
	// hours for it.
	now func() time.Time
}

// NewTrialService wires a TrialService. A nil mailer is the same as no SMTP:
// requests are refused rather than accepted and dropped.
func NewTrialService(
	st *store.Store,
	tenants *TenantService,
	users *UserService,
	mailer notify.Mailer,
	audit *AuditService,
	enabled bool,
	maxTenants int,
	publicURL string,
) *TrialService {
	return &TrialService{
		store: st, tenants: tenants, users: users, mailer: mailer, audit: audit,
		enabled: enabled, maxTenants: maxTenants, publicURL: publicURL,
		now: time.Now,
	}
}

// Enabled reports whether this deployment offers trials, which is what the
// sign-in screen asks before drawing the entry point.
func (s *TrialService) Enabled() bool { return s.enabled }

// TrialRequestInput is what the form collects.
type TrialRequestInput struct {
	Email       string
	CompanyName string
	TenantCode  string
	Industry    string
}

// TrialTenant is what a confirmed link produced. The password is here once,
// on its way into an email, and is not stored anywhere in readable form.
type TrialTenant struct {
	TenantCode    string
	TenantName    string
	AdminUsername string
	AdminPassword string
	SignInURL     string
}

// Request records an intent and emails a link. It deliberately reports the
// same success whether or not the address was already used, except where the
// caller could have known — see the comment at the quota check.
func (s *TrialService) Request(ctx context.Context, in TrialRequestInput, ip string) error {
	if !s.enabled {
		return ErrTrialSignupClosed
	}
	if s.mailer == nil {
		// Not a 500. A deployment that turned trials on without SMTP has a
		// configuration problem, and saying so is more useful than a link
		// that never arrives.
		return httpx.BadRequest("TRIAL_MAIL_UNAVAILABLE",
			"Trials need email and this deployment has no relay configured.")
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		return httpx.BadRequest("INVALID_EMAIL", "Enter an email address.")
	}
	company := strings.TrimSpace(in.CompanyName)
	if company == "" {
		return httpx.BadRequest("COMPANY_REQUIRED", "Enter an organization name.")
	}
	code := strings.ToLower(strings.TrimSpace(in.TenantCode))
	if err := validateTenantCode(code); err != nil {
		return err
	}
	industry := strings.TrimSpace(in.Industry)
	if industry == "" {
		industry = TrialIndustryGeneric
	}
	if !validTrialIndustry(industry) {
		return httpx.BadRequest("INVALID_INDUSTRY", "Pick one of the offered industries.")
	}

	// The quota is reported rather than hidden. A visitor who is told "full"
	// stops trying; one told "check your email" waits for a link that will
	// never come and concludes the product is broken.
	confirmed, err := s.store.Queries.CountConfirmedTrials(ctx)
	if err != nil {
		return fmt.Errorf("count trials: %w", err)
	}
	if s.maxTenants > 0 && confirmed >= int64(s.maxTenants) {
		return ErrTrialQuotaReached
	}

	if ip != "" {
		since := s.now().Add(-24 * time.Hour)
		recent, err := s.store.Queries.CountRecentTrialRequestsFromIP(ctx, sqlcgen.CountRecentTrialRequestsFromIPParams{
			RequestIp: ip,
			CreatedAt: since,
		})
		if err != nil {
			return fmt.Errorf("count recent trials: %w", err)
		}
		if recent >= trialsPerAddressPerDay {
			return ErrTrialTooManyFromAddress
		}
	}

	// Already has one. The partial index enforces this on confirmed rows, but
	// it cannot see a second pending request for the same address — so without
	// this read the visitor gets a link, clicks it, and is refused at the point
	// where they expected credentials.
	used, err := s.store.Queries.CountConfirmedTrialsForEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("count trials for address: %w", err)
	}
	if used > 0 {
		return ErrTrialEmailUsed
	}

	// A code taken by a tenant that exists is refused before a link is sent:
	// it is the one thing the visitor can fix, and finding out after they
	// have checked their email is the worst moment to be told.
	// A disabled tenant counts as taken: the code is spoken for, and handing
	// it out again would give a visitor a tenant somebody switched off.
	switch _, err := s.tenants.Resolve(ctx, code); {
	case err == nil, errors.Is(err, ErrTenantDisabled):
		return ErrTrialCodeTaken
	case errors.Is(err, ErrTenantNotFound):
		// Free.
	default:
		return fmt.Errorf("resolve tenant: %w", err)
	}

	token, hash, err := newTrialToken()
	if err != nil {
		return fmt.Errorf("issue trial token: %w", err)
	}

	id := uuid.NewString()

	_, err = s.store.Queries.CreateTrialRequest(ctx, sqlcgen.CreateTrialRequestParams{
		ID:          id,
		Email:       email,
		CompanyName: company,
		TenantCode:  code,
		Industry:    industry,
		TokenHash:   hash,
		ExpiresAt:   s.now().Add(TrialTokenTTL),
		RequestIp:   ip,
	})
	if err != nil {
		// Which index rejected it decides the answer, and both are things the
		// visitor can act on. The read above cannot replace this: two
		// requests arriving together both pass it.
		if store.IsUniqueViolation(err) {
			if strings.Contains(err.Error(), "one_per_email") {
				return ErrTrialEmailUsed
			}
			return ErrTrialCodeTaken
		}
		return fmt.Errorf("create trial request: %w", err)
	}

	return s.sendLink(ctx, email, company, token)
}

// Confirm spends a link, creates the tenant and its administrator, and mails
// the credentials.
//
// No client address, unlike Request. There is nothing here to attribute it to:
// the audit trail is per tenant and this runs before one exists, and the
// address that mattered — the one that asked — is already on the row.
func (s *TrialService) Confirm(ctx context.Context, token string) (TrialTenant, error) {
	if !s.enabled {
		return TrialTenant{}, ErrTrialSignupClosed
	}

	row, err := s.store.Queries.GetTrialRequestByToken(ctx, hashTrialToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return TrialTenant{}, ErrTrialLinkInvalid
	} else if err != nil {
		return TrialTenant{}, fmt.Errorf("read trial request: %w", err)
	}

	if row.ConfirmedAt != nil {
		return TrialTenant{}, ErrTrialLinkSpent
	}
	if s.now().After(row.ExpiresAt) {
		return TrialTenant{}, ErrTrialLinkExpired
	}

	tenant, err := s.tenants.Create(ctx, row.TenantCode, row.CompanyName)
	if err != nil {
		return TrialTenant{}, err
	}

	password, err := newTrialPassword()
	if err != nil {
		return TrialTenant{}, fmt.Errorf("generate password: %w", err)
	}

	// Not the documented default, so the account signs in normally rather
	// than demanding a change first. A visitor here did not choose this
	// password and has nowhere to look it up but the email; making them
	// replace it on the way in adds a step and nothing else.
	if _, _, err := s.users.EnsureInitialAdmin(ctx, tenant.ID, "admin", password); err != nil {
		return TrialTenant{}, fmt.Errorf(
			"tenant %q was created but its administrator was not: %w", tenant.Code, err)
	}

	// Spent last, and conditionally: two clicks a moment apart both find a
	// pending row above, and only one may keep the tenant it made. The loser
	// finds zero rows updated and says the link is spent — which it is.
	if _, err := s.store.Queries.MarkTrialRequestConfirmed(ctx, sqlcgen.MarkTrialRequestConfirmedParams{
		ID:       row.ID,
		TenantID: &tenant.ID,
	}); errors.Is(err, sql.ErrNoRows) {
		return TrialTenant{}, ErrTrialLinkSpent
	} else if err != nil {
		return TrialTenant{}, fmt.Errorf("mark trial confirmed: %w", err)
	}

	out := TrialTenant{
		TenantCode:    tenant.Code,
		TenantName:    tenant.Name,
		AdminUsername: "admin",
		AdminPassword: password,
		SignInURL:     s.signInLink(tenant.Code),
	}

	// Best effort, and after the tenant exists. A mail failure here must not
	// undo a tenant somebody can already sign in to — the response carries
	// the same credentials, which is why this is not the only copy.
	if s.mailer != nil {
		_ = s.mailer.Send(ctx, notify.Message{
			To:      row.Email,
			Subject: fmt.Sprintf("Your Portico trial: %s", tenant.Name),
			Body: fmt.Sprintf(
				"Your trial tenant is ready.\n\n"+
					"    Address:   %s\n"+
					"    Tenant:    %s\n"+
					"    Username:  %s\n"+
					"    Password:  %s\n\n"+
					"This is a demonstration. Do not put anything real in it.\n",
				out.SignInURL, out.TenantCode, out.AdminUsername, out.AdminPassword),
		})
	}

	return out, nil
}

// SweepExpired deletes unconfirmed requests whose links have expired, which
// is what returns a reserved tenant code to circulation.
func (s *TrialService) SweepExpired(ctx context.Context) (int64, error) {
	return s.store.Queries.DeleteExpiredTrialRequests(ctx)
}

func (s *TrialService) sendLink(ctx context.Context, email, company, token string) error {
	link := s.confirmLink(token)
	err := s.mailer.Send(ctx, notify.Message{
		To:      email,
		Subject: "Confirm your Portico trial",
		Body: fmt.Sprintf(
			"Somebody asked for a Portico trial for %s using this address.\n\n"+
				"Confirm it here, within %s:\n\n    %s\n\n"+
				"If that was not you, ignore this. Nothing was created.\n",
			company, TrialTokenTTL, link),
	})
	if err != nil {
		// Reverse the reservation. Keeping the row would hold the code and the
		// address for two hours, so the visitor who was just told the mail
		// failed cannot retry with the same details — the retry would collide
		// with their own abandoned request and report the code as taken. An
		// unsent link reserves nothing.
		if _, del := s.store.Queries.DeleteTrialRequestByToken(ctx, hashTrialToken(token)); del != nil {
			return fmt.Errorf("send trial link: %w; and could not release the reservation: %w", err, del)
		}
		return fmt.Errorf("send trial link: %w", err)
	}
	return nil
}

func (s *TrialService) confirmLink(token string) string {
	base := strings.TrimRight(s.publicURL, "/")
	return base + "/trial/confirm?" + url.Values{"token": {token}}.Encode()
}

func (s *TrialService) signInLink(code string) string {
	base := strings.TrimRight(s.publicURL, "/")
	return base + "/login?" + url.Values{"tenant": {code}}.Encode()
}

func newTrialToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashTrialToken(token), nil
}

func hashTrialToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newTrialPassword is long rather than clever. A new tenant's policy asks only
// for the engine's floor of eight characters and no character classes, so
// entropy is the only thing worth spending here — and nobody types this by
// hand, they copy it out of an email.
func newTrialPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
