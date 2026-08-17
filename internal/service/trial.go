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
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/model"
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

// TrialIndustryGeneric is what a request naming no industry gets.
//
// The rest of the list comes from the filler rather than from a constant here.
// A trial names a world it wants seeded, the worlds are data in another
// package, and a copy of their names in this one would be a second list to
// keep in agreement — with the first symptom of disagreement being a visitor
// choosing an industry that turns out not to exist.
const TrialIndustryGeneric = "generic"

// TenantFill is the request to fill a tenant that has just been created.
type TenantFill struct {
	TenantID string
	Industry string
	// Actor is whose name the audit trail carries for everything the fill
	// does. The tenant's own administrator, so that clicking through from an
	// entry lands on an account that exists.
	Actor auth.Principal
	// Password is what the demonstration accounts sign in with — one string for
	// all of them, generated per tenant and sent to the visitor along with the
	// administrator's own.
	//
	// Generated rather than published: these accounts share a password by
	// design, so a fixed one would mean anybody who guessed a tenant code could
	// sign in to somebody else's trial.
	Password string
}

// TenantFiller creates demonstration data inside a tenant.
//
// An interface here and an implementation elsewhere, because the packs are
// fixtures — a few hundred lines of invented people — and this package is the
// domain. It also breaks what would otherwise be an import cycle: the filler
// creates its contents by calling the services in this package.
type TenantFiller interface {
	// Industries returns the pack keys on offer, in the order to show them.
	Industries() []string
	// Fill creates one pack's contents. It is called after the tenant and its
	// administrator exist.
	Fill(ctx context.Context, in TenantFill) error
}

// TrialTokenTTL is how long a confirmation link stays usable.
//
// A day, the same as a registration verification. It was two hours, on the
// argument that an abandoned request holds a reserved tenant code and a
// shorter hold returns the name sooner. That is true and it was the wrong
// trade: somebody who asks for a trial in the evening and reads their mail
// the next morning is the ordinary case, not the abusive one, and the failure
// they met was a dead link with no way back to what they had typed.
//
// What the hold costs is bounded by the limits below rather than by the
// clock. A code held for a day is only worth holding if a request can be made
// cheaply, and between the per-mailbox, per-client and whole-deployment caps,
// it cannot.
const TrialTokenTTL = 24 * time.Hour

// trialsPerAddressPerDay bounds one client address over a day, which the
// per-minute rate limiter cannot see: fifty requests spread across an
// afternoon are inside every limit and are still one machine filling the quota.
const trialsPerAddressPerDay = 5

// trialsPerMailboxPerDay bounds how much mail one address can be made to
// receive.
//
// This is the only limit here that protects somebody who is not using the
// product. Every other check defends the demonstration; this one defends a
// stranger whose address was typed into the form by someone else, and who
// gets a "confirm your Portico trial" message for a trial they never asked
// for. The unique index cannot see it, because it is partial on confirmed
// rows and an unconfirmed request has already sent the message.
//
// Three rather than one, because a legitimate visitor asks again: the first
// message went to spam, or they deleted it, or the link expired. One would
// turn a support case into a refusal.
const trialsPerMailboxPerDay = 3

// trialsPerHour bounds the whole deployment.
//
// The caps above are per-something, and anything per-something is defeated by
// having more of that thing. This one is not: a sending quota and a sender
// reputation are shared by every message that leaves, and losing either takes
// down password recovery for the tenants that already exist — which is a much
// worse outcome than a stranger waiting an hour for a demonstration.
const trialsPerHour = 30

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

	// ErrTrialTooManyForMailbox is the same address having been mailed enough
	// times today.
	//
	// Worded for the person most likely to see it, who is somebody legitimate
	// asking a fourth time — not the attacker it exists to stop.
	ErrTrialTooManyForMailbox = httpx.TooManyRequests("TRIAL_TOO_MANY_FOR_EMAIL",
		"That address has already been sent several links today. Check your inbox and spam folder, or try again tomorrow.")

	// ErrTrialBusy is the whole demonstration having sent as much as it may
	// this hour.
	ErrTrialBusy = httpx.TooManyRequests("TRIAL_BUSY",
		"This demonstration is handing out trials faster than it may. Try again in an hour.")

	// ErrTrialEmailDomainBlocked is a throwaway mailbox.
	//
	// The address is the whole of the identity check, and what makes that
	// thin claim worth anything is that somebody could be reached at it
	// afterwards. A mailbox that expires in ten minutes is not that, and a
	// tenant traceable to one is traceable to nobody.
	ErrTrialEmailDomainBlocked = httpx.UnprocessableEntity("TRIAL_EMAIL_DOMAIN_BLOCKED",
		"That email provider is not accepted here. Use an address you can be reached at.")

	// ErrTrialMailFailed is the relay refusing the message.
	//
	// 503 rather than 500: the request was well formed, this server is
	// working, and something outside it is not. The visitor is told to try
	// again because that is genuinely what to do — the reservation is
	// released before this is returned, so the same details are free.
	ErrTrialMailFailed = httpx.ServiceUnavailable("TRIAL_MAIL_FAILED",
		"The confirmation email could not be sent just now. Try again in a minute.")

	// ErrTrialLinkInvalid is a token that names no request.
	ErrTrialLinkInvalid = httpx.BadRequest("TRIAL_LINK_INVALID",
		"That link is not valid. Request a new trial.")

	// ErrTrialLinkExpired is a link that outlived its day, and with it the
	// tenant code it was holding.
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

	// filler is what turns a new tenant from empty into something worth
	// looking at. Optional: without one a trial still produces a working
	// tenant, and the only industry on offer is the generic name.
	filler TenantFiller

	// blockedDomains are the mailbox providers this deployment will not
	// accept. Built once at construction rather than per request.
	blockedDomains map[string]bool

	// messages renders the two mails this service sends, and locale is the
	// language it writes them in. The deployment's default, because a trial
	// applicant has no account and no tenant to take a preference from —
	// having neither is what they are asking to change.
	messages *i18n.Catalog
	locale   string

	// now is replaceable so a test can expire a link without waiting two
	// hours for it.
	now func() time.Time
}

// WithFiller attaches the demonstration packs.
//
// Separate from the constructor, which already takes eight arguments, and
// separate for a second reason: this is the one dependency that reaches back
// into a package that depends on this one, so keeping it visible at the
// assembly site is worth a line.
func (s *TrialService) WithFiller(f TenantFiller) *TrialService {
	s.filler = f
	return s
}

// Industries is what the sign-in screen offers, which is exactly what the
// filler can create.
func (s *TrialService) Industries() []string {
	if s.filler == nil {
		return []string{TrialIndustryGeneric}
	}
	return s.filler.Industries()
}

func (s *TrialService) validIndustry(name string) bool {
	for _, offered := range s.Industries() {
		if offered == name {
			return true
		}
	}
	return false
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
		blockedDomains: blockedEmailDomains(nil),
		messages:       i18n.MustLoad(),
		now:            time.Now,
	}
}

// WithLocale sets the language the trial messages are written in.
//
// The deployment default, for the reason given on the field: there is nobody
// to ask. Unset leaves English.
func (s *TrialService) WithLocale(locale string) *TrialService {
	s.locale = locale
	return s
}

// WithBlockedEmailDomains adds to the built-in list of throwaway mailbox
// providers.
//
// Additive rather than a replacement: an operator adding the provider that
// their own visitors abuse should not have to restate the defaults, and one
// who pastes a short list into the environment would otherwise silently turn
// off everything else.
func (s *TrialService) WithBlockedEmailDomains(extra []string) *TrialService {
	s.blockedDomains = blockedEmailDomains(extra)
	return s
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

	// DemoPassword is what the seeded accounts sign in with, and is empty when
	// nothing was seeded.
	//
	// Given out because looking at the portal as an ordinary person is half of
	// what there is to see, and the administrator's own account cannot show it.
	// Every one of these accounts is an ordinary user, so handing out one
	// password for all of them costs nothing an administrator has.
	DemoPassword string
	// Industry is the pack that was created, or empty if the fill failed. Said
	// out loud rather than assumed from the request: a visitor who asked for a
	// hospital and got an empty tenant should be able to tell.
	Industry string
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
	if domainIsBlocked(s.blockedDomains, email) {
		return ErrTrialEmailDomainBlocked
	}
	// The mailbox this reaches, which is what every per-address rule below
	// counts. See mailboxKey: the address as typed is still what gets mailed.
	mailbox := mailboxKey(email)
	code := strings.ToLower(strings.TrimSpace(in.TenantCode))
	if err := validateTenantCode(code); err != nil {
		return err
	}

	// The display name, which the visitor may not bother with.
	//
	// It used to be required, and it was the one field on the form asking for
	// something the product does not need: a tenant works perfectly with its
	// code as its name, and somebody trying a demonstration has no reason to
	// have decided what to call it. Left blank it becomes the code, which is
	// what they typed one field earlier and can rename in Settings.
	company := strings.TrimSpace(in.CompanyName)
	if company == "" {
		company = code
	}
	industry := strings.TrimSpace(in.Industry)
	if industry == "" {
		industry = TrialIndustryGeneric
	}
	if !s.validIndustry(industry) {
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

	// What the whole deployment has sent this hour. First of the three
	// counts, because it is the one that protects something shared: a
	// sending quota and a sender reputation are spent by every message, and
	// losing either takes password recovery down for the tenants that
	// already exist.
	burst, err := s.store.Queries.CountRecentTrialRequests(ctx, s.now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count recent trials: %w", err)
	}
	if burst >= trialsPerHour {
		return ErrTrialBusy
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

	// How much mail this mailbox has been sent today, whoever asked for it.
	//
	// The only check here that protects somebody who is not using this
	// product: without it, an address nobody controls can be sent a fresh
	// confirmation message as often as anybody likes, each with a different
	// tenant code so that nothing else collides. Counts pending requests as
	// well as confirmed ones, because the pending ones are the messages.
	sent, err := s.store.Queries.CountRecentTrialRequestsForEmail(ctx, sqlcgen.CountRecentTrialRequestsForEmailParams{
		EmailKey:  mailbox,
		CreatedAt: s.now().Add(-24 * time.Hour),
	})
	if err != nil {
		return fmt.Errorf("count recent trials for address: %w", err)
	}
	if sent >= trialsPerMailboxPerDay {
		return ErrTrialTooManyForMailbox
	}

	// Already has one. The partial index enforces this on confirmed rows, but
	// it cannot see a second pending request for the same address — so without
	// this read the visitor gets a link, clicks it, and is refused at the point
	// where they expected credentials.
	used, err := s.store.Queries.CountConfirmedTrialsForEmail(ctx, mailbox)
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
		EmailKey:    mailbox,
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

	// The address that proved itself, kept on the account it created.
	//
	// EnsureInitialAdmin makes an account with a username and a password and
	// nothing else, which is right for the bootstrap administrator of an
	// ordinary deployment: nobody has proved an address at that point. Here
	// somebody has — this tenant exists *because* a link sent to that address
	// was opened — and throwing it away left the one person who can administer
	// the tenant unable to recover it, with the portal saying so on every
	// visit.
	//
	// Done here rather than by giving EnsureInitialAdmin an address parameter,
	// because that function is shared with first-start bootstrap and this has
	// nothing to do with it.
	//
	// Best effort, on the same reasoning as the fill below.
	actor := s.fillActor(ctx, tenant.ID)
	if actor.UserID != "" {
		if _, err := s.users.Update(ctx, actor, actor.UserID, UpdateUserInput{
			DisplayName: "Administrator",
			Email:       row.Email,
			Role:        model.RoleSuperAdmin,
		}); err != nil {
			slog.ErrorContext(ctx, "a trial tenant's administrator did not keep its address",
				"tenant", tenant.Code, "error", err)
		}
	}

	// The pack the visitor asked for.
	//
	// Best effort, and the one place in this method where a failure does not
	// become the visitor's problem. Everything above has already happened: the
	// tenant exists, its administrator can sign in, and the link is spent.
	// Returning an error here would report a failure for something that
	// succeeded and leave them with credentials they were never shown — so the
	// tenant is handed over as it is, empty, and the reason is in the log where
	// whoever runs the demonstration can find it.
	//
	// That is the opposite of the rule internal/seed follows, and deliberately:
	// there, a half-seeded database is worse than none because nobody is
	// waiting and it can simply be run again. Here somebody is standing in
	// front of the page.
	if s.filler != nil {
		demoPassword, fillErr := newTrialPassword()
		if fillErr == nil {
			fillErr = s.filler.Fill(ctx, TenantFill{
				TenantID: tenant.ID,
				Industry: row.Industry,
				Actor:    actor,
				Password: demoPassword,
			})
		}
		if fillErr != nil {
			slog.ErrorContext(ctx, "a trial tenant was created but could not be filled",
				"tenant", tenant.Code, "industry", row.Industry, "error", fillErr)
		} else {
			out.DemoPassword = demoPassword
			out.Industry = row.Industry
		}
	}

	// Best effort, and after the tenant exists. A mail failure here must not
	// undo a tenant somebody can already sign in to — the response carries
	// the same credentials, which is why this is not the only copy.
	if s.mailer != nil {
		msg, err := s.readyMail(out)
		if err != nil {
			slog.ErrorContext(ctx, "could not compose the trial credentials mail",
				"tenant", tenant.Code, "error", err)
		} else {
			msg.To = row.Email
			_ = s.mailer.Send(ctx, msg)
		}
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
	msg, err := s.confirmMail(company, link)
	if err != nil {
		return fmt.Errorf("compose trial link mail: %w", err)
	}
	msg.To = email
	err = s.mailer.Send(ctx, msg)
	if err != nil {
		// Reverse the reservation. Keeping the row would hold the code and the
		// address for a day, so the visitor who was just told the mail
		// failed cannot retry with the same details — the retry would collide
		// with their own abandoned request and report the code as taken. An
		// unsent link reserves nothing.
		if _, del := s.store.Queries.DeleteTrialRequestByToken(ctx, hashTrialToken(token)); del != nil {
			return fmt.Errorf("send trial link: %w; and could not release the reservation: %w", err, del)
		}

		// The real reason to the log, a usable one to the visitor.
		//
		// This used to fall through to a 500 and "an unexpected error
		// occurred", which is wrong twice: nothing about a relay refusing a
		// message is unexpected — a quota, a rotated credential, a network
		// that drops SMTP — and the visitor is told the product is broken
		// when the thing to do is try again in a minute.
		slog.ErrorContext(ctx, "could not send a trial confirmation link",
			"to", email, "error", err)
		return ErrTrialMailFailed
	}
	return nil
}

// fillActor is whose name the audit trail carries for everything the pack
// creates.
//
// The tenant's own administrator, looked up rather than assumed, so that an
// audit entry links to an account somebody can open. If the lookup fails the
// fill still runs under a principal with no account behind it — a trail that
// names "admin" without linking anywhere is worth more than no tenant.
func (s *TrialService) fillActor(ctx context.Context, tenantID string) auth.Principal {
	actor := auth.Principal{TenantID: tenantID, Username: "admin", Role: model.RoleSuperAdmin}
	admin, err := s.store.ForTenant(tenantID).GetUserByUsername(ctx, "admin")
	if err != nil {
		return actor
	}
	actor.UserID = admin.ID
	return actor
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
