// Package service holds the business logic that sits between the HTTP
// handlers and the store.
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
)

// Setting keys. These are the runtime-tunable values from §3.10.
const (
	// SettingTokenTTLMinutes is how long a sign-in to Portico's own console
	// lasts. Not the OIDC tokens issued to registered applications — those
	// are the three keys below, and conflating the two is easy enough that
	// the console labels this one "console session".
	SettingTokenTTLMinutes = "token_ttl_minutes"

	// The lifetimes of the tokens Portico issues as an OpenID Provider.
	//
	// These were constants until it became clear what that cost: the only
	// answer to "how long is an access token valid on this deployment" was to
	// read the source, and the only way to change it was to fork. What makes
	// them safe to expose is that each has a ceiling it cannot be set past —
	// see MaxOIDCAccessTokenTTLMinutes and the two beside it.
	//
	// SettingOIDCAccessTokenTTLMinutes governs the ID token as well. They are
	// the same duration, they were the same constant, and a second control
	// would be a second thing to get wrong for no gain: an ID token outliving
	// the access token it arrived with describes an authentication that may
	// already have been withdrawn.
	// #nosec G101 -- a settings key, not a credential. gosec pattern-matches
	// "token" in the name of a string constant; the value is a column key that
	// appears verbatim in the settings table.
	SettingOIDCAccessTokenTTLMinutes = "oidc_access_token_ttl_minutes"
	// SettingOIDCRefreshTokenTTLDays is how long a refresh token stays
	// usable. Each use rotates it and the replacement gets a fresh window, so
	// this bounds inactivity rather than the session — SettingOIDCSessionMaxAgeDays
	// is what bounds the session.
	// #nosec G101 -- a settings key, not a credential; same as above.
	SettingOIDCRefreshTokenTTLDays = "oidc_refresh_token_ttl_days"
	// SettingOIDCSessionMaxAgeDays is the absolute age of a refresh chain,
	// measured from the sign-in that started it rather than from the last
	// refresh. Without it a chain that is refreshed diligently never ends:
	// every rotation extends the window, so "thirty days" means thirty days
	// of silence, not thirty days of access.
	//
	// Zero switches it off and is the default, for the reason audit retention
	// keeps everything by default. This is the one setting here that ends
	// sessions which are working: shipping a cap would sign every long-lived
	// integration out that many days after an upgrade, on a schedule nobody
	// chose.
	SettingOIDCSessionMaxAgeDays = "oidc_session_max_age_days"
	// SettingRegistrationEnabled gates self-service registration, letting
	// the same build serve a closed intranet and an open internet
	// deployment (§3.10).
	SettingRegistrationEnabled = "registration_enabled"
	// SettingRegistrationVerification requires a self-registered account to
	// prove its contact address before it can sign in.
	//
	// Off by default, and a switch rather than a fixed rule: a closed
	// intranet where registration is already behind the network boundary
	// gains nothing from it, while a deployment facing outward cannot do
	// without it. Turning it on is refused where no channel can deliver —
	// see SettingsService.Update.
	SettingRegistrationVerification = "registration_verification"
	// SettingSystemName is shown in the UI header.
	SettingSystemName = "system_name"
	// SettingLockoutThreshold is how many consecutive failed sign-ins lock
	// an account. Zero switches lockout off.
	SettingLockoutThreshold = "lockout_threshold"
	// SettingLockoutDurationMinutes is how long a lock lasts, and also the
	// window failures are counted over — see UserService.Login.
	SettingLockoutDurationMinutes = "lockout_duration_minutes"

	// Password policy. All of these are off or permissive by default; see
	// password_policy.go for why composition rules and expiry are provided
	// but not recommended.
	SettingPasswordMinLength        = "password_min_length"
	SettingPasswordRequireUppercase = "password_require_uppercase"
	SettingPasswordRequireLowercase = "password_require_lowercase"
	SettingPasswordRequireDigit     = "password_require_digit"
	SettingPasswordRequireSymbol    = "password_require_symbol"
	SettingPasswordHistoryDepth     = "password_history_depth"
	SettingPasswordMaxAgeDays       = "password_max_age_days"

	// SettingAuditRetentionDays is how long audit entries are kept. Zero —
	// the default — keeps them indefinitely.
	SettingAuditRetentionDays = "audit_retention_days"

	// SettingDefaultLocale is the language of messages this tenant sends to
	// somebody who has stated no preference of their own. Empty — the
	// default — follows the deployment's PORTICO_DEFAULT_LOCALE.
	//
	// Empty means "unset" rather than "English": one deployment can serve a
	// Chinese tenant and an English one, and a tenant that has said nothing
	// should follow whatever the deployment is changed to later rather than
	// having been frozen at install time.
	SettingDefaultLocale = "default_locale"
)

// Settings is the full set of runtime settings for one tenant.
type Settings struct {
	// TokenTTLMinutes is the console's own session, not the OIDC tokens.
	TokenTTLMinutes int `json:"tokenTtlMinutes"`

	// The three OIDC lifetimes. Days rather than minutes for the second and
	// third because minutes for a thirty-day value is a field nobody can read
	// at a glance — 43200 and 432000 differ by a digit and by a factor of ten.
	OIDCAccessTokenTTLMinutes int `json:"oidcAccessTokenTtlMinutes"`
	OIDCRefreshTokenTTLDays   int `json:"oidcRefreshTokenTtlDays"`
	// OIDCSessionMaxAgeDays caps the whole refresh chain. Zero means no cap.
	OIDCSessionMaxAgeDays int `json:"oidcSessionMaxAgeDays"`

	RegistrationEnabled bool `json:"registrationEnabled"`
	// RegistrationVerification requires a self-registered account to prove
	// its email address or phone number before it can sign in. Without it
	// somebody can open an account under a colleague's address — and that
	// address is where a password-reset link would be sent.
	RegistrationVerification bool   `json:"registrationVerification"`
	SystemName               string `json:"systemName"`

	// LockoutThreshold is the number of consecutive failed sign-ins that
	// locks an account. Zero means no lockout.
	LockoutThreshold int `json:"lockoutThreshold"`
	// LockoutDurationMinutes is how long the lock lasts.
	LockoutDurationMinutes int `json:"lockoutDurationMinutes"`

	PasswordMinLength        int  `json:"passwordMinLength"`
	PasswordRequireUppercase bool `json:"passwordRequireUppercase"`
	PasswordRequireLowercase bool `json:"passwordRequireLowercase"`
	PasswordRequireDigit     bool `json:"passwordRequireDigit"`
	PasswordRequireSymbol    bool `json:"passwordRequireSymbol"`
	// PasswordHistoryDepth is how many previous passwords may not be
	// reused. Zero does not check.
	PasswordHistoryDepth int `json:"passwordHistoryDepth"`
	// PasswordMaxAgeDays is how long a password stays usable. Zero never
	// expires.
	PasswordMaxAgeDays int `json:"passwordMaxAgeDays"`

	// DefaultLocale is the language of messages sent to somebody in this
	// tenant who has stated no preference. Empty follows the deployment.
	//
	// It does not affect the console: a reader picks that for themselves and
	// it is remembered in their browser. This is for the text that arrives
	// where there is no menu — a reset link, a confirmation.
	DefaultLocale string `json:"defaultLocale"`

	// AuditRetentionDays is how long audit entries are kept before the
	// periodic sweep removes them. Zero keeps them forever, which is the
	// default and the only safe one to ship: the trail is the record of
	// what happened, and a product that quietly started deleting it on a
	// timer would be doing the worst thing an audit log can do.
	AuditRetentionDays int `json:"auditRetentionDays"`
}

// PasswordPolicy is the password half of these settings.
func (s Settings) PasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:        s.PasswordMinLength,
		RequireUppercase: s.PasswordRequireUppercase,
		RequireLowercase: s.PasswordRequireLowercase,
		RequireDigit:     s.PasswordRequireDigit,
		RequireSymbol:    s.PasswordRequireSymbol,
		HistoryDepth:     s.PasswordHistoryDepth,
		MaxAgeDays:       s.PasswordMaxAgeDays,
	}
}

// LockoutDuration is the lock length as a duration. It doubles as the window
// failures are counted over, so that "five failures in fifteen minutes locks
// for fifteen minutes" is one number rather than two that have to be kept in
// a sensible relationship.
func (s Settings) LockoutDuration() time.Duration {
	return time.Duration(s.LockoutDurationMinutes) * time.Minute
}

// LockoutEnabled reports whether this tenant locks accounts at all.
func (s Settings) LockoutEnabled() bool {
	return s.LockoutThreshold > 0 && s.LockoutDurationMinutes > 0
}

// TokenTTL is the console session's lifetime as a duration.
func (s Settings) TokenTTL() time.Duration {
	return time.Duration(s.TokenTTLMinutes) * time.Minute
}

// Bounds on the token lifetime. A value outside this range is almost
// certainly a mistake: too short locks everyone out, too long defeats
// expiry entirely.
const (
	MinTokenTTLMinutes = 5
	MaxTokenTTLMinutes = 60 * 24 * 30 // 30 days
)

// OIDCAccessTokenLifetime is how long an access token this server issues
// stays valid. The ID token gets the same, deliberately; see
// SettingOIDCAccessTokenTTLMinutes.
func (s Settings) OIDCAccessTokenLifetime() time.Duration {
	return time.Duration(s.OIDCAccessTokenTTLMinutes) * time.Minute
}

// OIDCRefreshTokenLifetime is how long a refresh token stays usable before it
// has to be exchanged. Rotation resets it.
func (s Settings) OIDCRefreshTokenLifetime() time.Duration {
	return time.Duration(s.OIDCRefreshTokenTTLDays) * 24 * time.Hour
}

// OIDCSessionMaxAge is the absolute age a refresh chain may reach, counted
// from the sign-in that began it. Zero means no cap, which is what a caller
// has to check for: passing it to a comparison unchecked would expire every
// session immediately.
func (s Settings) OIDCSessionMaxAge() time.Duration {
	return time.Duration(s.OIDCSessionMaxAgeDays) * 24 * time.Hour
}

// OIDCSessionCapped reports whether this tenant ends refresh chains by age at
// all. Named rather than left as a `> 0` at each use, because the two places
// that ask are a signing path and a UI hint and they must agree.
func (s Settings) OIDCSessionCapped() bool {
	return s.OIDCSessionMaxAgeDays > 0
}

// Bounds on the OIDC token lifetimes.
//
// The access token's ceiling is the load-bearing one. That token is verified
// offline by a resource server that never calls back here, so it cannot be
// revoked: how soon it expires is the only thing that limits how long a
// withdrawn permission keeps working. An hour is already generous. A day —
// which is what somebody reaching for "make this less annoying" would
// pick — would mean a disabled account still being served for a day, and the
// administrator who disabled it would have no way to tell.
//
// The refresh ceiling is ninety days because a refresh token that lives a
// year is a password that never rotates, held by a client that was never
// designed to protect one that long.
//
// The session cap's ceiling is a year, and its floor is not 1 but 0: zero is
// the off switch and has to stay reachable, since turning a control off is a
// decision an operator is entitled to make deliberately.
const (
	MinOIDCAccessTokenTTLMinutes = 1
	MaxOIDCAccessTokenTTLMinutes = 60

	MinOIDCRefreshTokenTTLDays = 1
	MaxOIDCRefreshTokenTTLDays = 90

	MaxOIDCSessionMaxAgeDays = 365
)

// Bounds on lockout.
//
// The maximum threshold is deliberately low: a threshold of a thousand is
// not a lockout, it is a lockout that never fires, and an operator who set
// one would believe they had the control. The maximum duration is a day —
// anything longer is really "disable the account", which is a decision an
// administrator should make rather than a counter.
const (
	MaxLockoutThreshold       = 100
	MaxLockoutDurationMinutes = 60 * 24
)

// Bounds on the password policy.
//
// The history depth is capped low because each entry costs a bcrypt
// comparison on every password change, and a change is exactly when somebody
// is waiting on a form. The minimum length cannot go below auth's floor,
// which applies whatever a tenant configures, and cannot exceed bcrypt's
// 72-byte limit — a policy demanding more than can be hashed would refuse
// every password.
const (
	MaxPasswordHistoryDepth = 24
	MaxPasswordMinLength    = 72
	MaxPasswordMaxAgeDays   = 3650
)

// Bounds on audit retention.
//
// The minimum is not zero-to-anything: a retention of one day is
// indistinguishable from an accident, and the difference between "we keep
// nothing" and "we keep a week" is the difference between an incident nobody
// can reconstruct and one somebody can. Zero is still available and still
// means keep everything — what is refused is the range where a typo destroys
// the trail. The maximum is ten years, past which nobody is deleting on a
// schedule anyway.
const (
	MinAuditRetentionDays = 7
	MaxAuditRetentionDays = 3650
)

// SettingsService reads and writes runtime settings, caching them in memory
// because they are read on every login and change rarely.
//
// The cache is keyed by tenant. Settings are per-tenant — one tenant may
// accept sign-ups while another does not, and each names itself — so a
// single cached value would serve one tenant's configuration to another.
type SettingsService struct {
	store    *store.Store
	defaults Settings

	// deliverable reports which channels this deployment can actually reach
	// somebody on. Attached after construction rather than injected,
	// because the service that knows is built later — the same arrangement
	// as users.WithEvents.
	//
	// Nil means "unknown", which is treated as none: a deployment that has
	// not wired this up should not be able to turn on a requirement it
	// cannot satisfy.
	deliverable func() []model.RecoveryChannel

	// deploymentLocale is PORTICO_DEFAULT_LOCALE: the language a message
	// falls back to when neither the account nor its tenant has said
	// anything. Empty is treated as English.
	deploymentLocale string

	mu    sync.RWMutex
	cache map[string]Settings
}

// WithDefaultLocale tells the settings service what the deployment was
// configured with, which is the last stop before English.
//
// Attached after construction for the same reason WithDeliveryChannels is:
// it comes from process configuration rather than from anything this
// service can work out, and every test that builds a settings service
// should not have to know about it.
func (s *SettingsService) WithDefaultLocale(locale string) {
	s.deploymentLocale = locale
}

// MessageLocale picks the language for a message Portico sends to somebody.
//
// One function so the chain exists once. Both mailers ask this rather than
// each resolving for itself, because two implementations of "which language"
// is how a confirmation arrives in one language and the reset link that
// follows it arrives in another.
//
// A tenant whose settings cannot be read falls back rather than failing: a
// person waiting for a reset link should get one in English, not nothing.
func (s *SettingsService) MessageLocale(ctx context.Context, tenantID, accountPreference string) i18n.Locale {
	tenantDefault := ""
	if set, err := s.Get(ctx, tenantID); err == nil {
		tenantDefault = set.DefaultLocale
	}
	return i18n.Resolve(accountPreference, tenantDefault, s.deploymentLocale)
}

// WithDeliveryChannels tells the settings service what this deployment can
// send, so it can refuse a setting that depends on being able to.
func (s *SettingsService) WithDeliveryChannels(channels func() []model.RecoveryChannel) {
	s.deliverable = channels
}

// CanDeliver reports whether any channel is configured.
func (s *SettingsService) CanDeliver() bool {
	return s.deliverable != nil && len(s.deliverable()) > 0
}

// NewSettingsService returns a service whose defaults come from the process
// configuration, so an operator can set a starting value via environment
// variable and adjust it later from the UI.
func NewSettingsService(st *store.Store, defaultTokenTTL time.Duration) *SettingsService {
	ttlMinutes := int(defaultTokenTTL.Minutes())
	if ttlMinutes < MinTokenTTLMinutes {
		ttlMinutes = MinTokenTTLMinutes
	}

	return &SettingsService{
		store: st,
		cache: map[string]Settings{},
		defaults: Settings{
			TokenTTLMinutes: ttlMinutes,
			// Fifteen minutes and thirty days, which is what the constants
			// these replaced held. A default that shifted on the way to
			// being configurable would change every deployment that upgrades
			// without anybody asking for it.
			OIDCAccessTokenTTLMinutes: 15,
			OIDCRefreshTokenTTLDays:   30,
			// No cap. This is the only setting on this page that ends
			// sessions which are working, so it has to be asked for — see
			// SettingOIDCSessionMaxAgeDays.
			OIDCSessionMaxAgeDays: 0,
			// Registration is off by default: an instance that is exposed
			// before anyone configures it should not accept sign-ups.
			RegistrationEnabled: false,
			SystemName:          "Portico",
			// On by default. An instance exposed before anyone configures it
			// should already resist online guessing, and five in fifteen
			// minutes is loose enough that a person mistyping their password
			// a few times does not notice it exists.
			LockoutThreshold:       5,
			LockoutDurationMinutes: 15,
			// Composition rules and expiry default to off. They make
			// passwords more guessable rather than less — see
			// password_policy.go — and exist for deployments audited
			// against regimes that require them. Length is the lever that
			// actually helps, so that is the one with a real default.
			PasswordMinLength:    auth.MinPasswordLength,
			PasswordHistoryDepth: 0,
			PasswordMaxAgeDays:   0,
			// Keep everything. Anything else has to be asked for.
			AuditRetentionDays: 0,
		},
	}
}

// Defaults returns the settings a tenant has before anybody changes any of
// them.
//
// Exposed for one purpose: a caller that needs a lifetime on a path where
// failing is worse than being slightly wrong. Signing would otherwise have to
// take a whole tenant's sign-in down over an unreadable settings row, when the
// values it wanted are constants that most deployments never touch.
func (s *SettingsService) Defaults() Settings {
	return s.defaults
}

// Get returns a tenant's current settings, reading from the database on
// first use.
func (s *SettingsService) Get(ctx context.Context, tenantID string) (Settings, error) {
	s.mu.RLock()
	cached, ok := s.cache[tenantID]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another goroutine may have loaded while we waited for the write lock.
	if cached, ok := s.cache[tenantID]; ok {
		return cached, nil
	}

	loaded := s.defaults
	rows, err := s.store.ForTenant(tenantID).ListSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	for _, row := range rows {
		switch row.Key {
		case SettingTokenTTLMinutes:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.TokenTTLMinutes = n
			}
		case SettingOIDCAccessTokenTTLMinutes:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.OIDCAccessTokenTTLMinutes = n
			}
		case SettingOIDCRefreshTokenTTLDays:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.OIDCRefreshTokenTTLDays = n
			}
		case SettingOIDCSessionMaxAgeDays:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.OIDCSessionMaxAgeDays = n
			}
		case SettingRegistrationEnabled:
			loaded.RegistrationEnabled = row.Value == "true"
		case SettingRegistrationVerification:
			loaded.RegistrationVerification = row.Value == "true"
		case SettingSystemName:
			loaded.SystemName = row.Value
		case SettingDefaultLocale:
			loaded.DefaultLocale = row.Value
		case SettingLockoutThreshold:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.LockoutThreshold = n
			}
		case SettingLockoutDurationMinutes:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.LockoutDurationMinutes = n
			}
		case SettingPasswordMinLength:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.PasswordMinLength = n
			}
		case SettingPasswordRequireUppercase:
			loaded.PasswordRequireUppercase = row.Value == "true"
		case SettingPasswordRequireLowercase:
			loaded.PasswordRequireLowercase = row.Value == "true"
		case SettingPasswordRequireDigit:
			loaded.PasswordRequireDigit = row.Value == "true"
		case SettingPasswordRequireSymbol:
			loaded.PasswordRequireSymbol = row.Value == "true"
		case SettingPasswordHistoryDepth:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.PasswordHistoryDepth = n
			}
		case SettingPasswordMaxAgeDays:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.PasswordMaxAgeDays = n
			}
		case SettingAuditRetentionDays:
			if n, err := strconv.Atoi(row.Value); err == nil {
				loaded.AuditRetentionDays = n
			}
		}
	}

	s.cache[tenantID] = loaded
	return loaded, nil
}

// Update writes a tenant's settings and refreshes its cache entry.
func (s *SettingsService) Update(ctx context.Context, tenantID string, next Settings) (Settings, error) {
	if next.TokenTTLMinutes < MinTokenTTLMinutes || next.TokenTTLMinutes > MaxTokenTTLMinutes {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Console session lifetime must be between %d and %d minutes.",
				MinTokenTTLMinutes, MaxTokenTTLMinutes))
	}
	// Refused rather than clamped, like everything else on this endpoint. An
	// administrator who asked for a day and silently got an hour would
	// believe they had set something they had not, and would find out from a
	// support ticket about tokens expiring.
	if next.OIDCAccessTokenTTLMinutes < MinOIDCAccessTokenTTLMinutes ||
		next.OIDCAccessTokenTTLMinutes > MaxOIDCAccessTokenTTLMinutes {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Access token lifetime must be between %d and %d minutes. "+
				"An access token is verified without calling back here, so it cannot be "+
				"revoked — how soon it expires is the only limit on a permission that has "+
				"been withdrawn.",
				MinOIDCAccessTokenTTLMinutes, MaxOIDCAccessTokenTTLMinutes))
	}
	if next.OIDCRefreshTokenTTLDays < MinOIDCRefreshTokenTTLDays ||
		next.OIDCRefreshTokenTTLDays > MaxOIDCRefreshTokenTTLDays {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Refresh token lifetime must be between %d and %d days.",
				MinOIDCRefreshTokenTTLDays, MaxOIDCRefreshTokenTTLDays))
	}
	// Zero is the off switch and is allowed; anything between there and the
	// ceiling is a cap. Negative is neither.
	if next.OIDCSessionMaxAgeDays < 0 || next.OIDCSessionMaxAgeDays > MaxOIDCSessionMaxAgeDays {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Maximum session age must be 0 to switch it off, or between 1 and %d days.",
				MaxOIDCSessionMaxAgeDays))
	}
	if next.SystemName == "" {
		next.SystemName = s.defaults.SystemName
	}
	// Requiring verification on a deployment that cannot send anything would
	// accept the setting and then strand every registration on a message
	// that never arrives. Refused at the point of turning it on, with the
	// reason — the same principle as the home screen only suggesting a
	// recovery channel that exists.
	//
	// Checked when set rather than continuously: removing SMTP from the
	// environment afterwards leaves the setting standing, so registration
	// checks again at the moment it would need to send.
	if next.RegistrationVerification && !s.CanDeliver() {
		return Settings{}, httpx.UnprocessableEntity("NO_DELIVERY_CHANNEL",
			"Requiring verification needs a way to send it. Configure PORTICO_SMTP_HOST and restart, "+
				"or leave verification off.")
	}
	if next.LockoutThreshold < 0 || next.LockoutThreshold > MaxLockoutThreshold {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Lockout threshold must be between 0 and %d; 0 switches lockout off.",
				MaxLockoutThreshold))
	}
	if next.LockoutDurationMinutes < 0 || next.LockoutDurationMinutes > MaxLockoutDurationMinutes {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Lockout duration must be between 0 and %d minutes.",
				MaxLockoutDurationMinutes))
	}
	if next.PasswordMinLength < auth.MinPasswordLength || next.PasswordMinLength > MaxPasswordMinLength {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Minimum password length must be between %d and %d. "+
				"The floor is not configurable: it applies whatever this is set to.",
				auth.MinPasswordLength, MaxPasswordMinLength))
	}
	if next.PasswordHistoryDepth < 0 || next.PasswordHistoryDepth > MaxPasswordHistoryDepth {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Password history depth must be between 0 and %d; "+
				"0 does not check reuse. Each remembered password costs a hash "+
				"comparison on every change.", MaxPasswordHistoryDepth))
	}
	if next.AuditRetentionDays != 0 &&
		(next.AuditRetentionDays < MinAuditRetentionDays || next.AuditRetentionDays > MaxAuditRetentionDays) {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Audit retention must be 0, which keeps everything, or "+
				"between %d and %d days.", MinAuditRetentionDays, MaxAuditRetentionDays))
	}
	if next.PasswordMaxAgeDays < 0 || next.PasswordMaxAgeDays > MaxPasswordMaxAgeDays {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Password maximum age must be between 0 and %d days; "+
				"0 never expires.", MaxPasswordMaxAgeDays))
	}
	// Empty is allowed and means "follow the deployment". Anything else has
	// to be a language this build actually has messages for — accepting a
	// tag with nothing behind it would store a setting that silently does
	// nothing, which is worse than refusing it.
	if next.DefaultLocale != "" {
		if _, ok := i18n.Parse(next.DefaultLocale); !ok {
			return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
				fmt.Sprintf("This build has no messages for %q. Leave it empty to "+
					"follow the deployment default, or choose one of: %s.",
					next.DefaultLocale, localeList()))
		}
	}

	values := map[string]string{
		SettingTokenTTLMinutes:           strconv.Itoa(next.TokenTTLMinutes),
		SettingOIDCAccessTokenTTLMinutes: strconv.Itoa(next.OIDCAccessTokenTTLMinutes),
		SettingOIDCRefreshTokenTTLDays:   strconv.Itoa(next.OIDCRefreshTokenTTLDays),
		SettingOIDCSessionMaxAgeDays:     strconv.Itoa(next.OIDCSessionMaxAgeDays),
		SettingRegistrationEnabled:       strconv.FormatBool(next.RegistrationEnabled),
		SettingRegistrationVerification:  strconv.FormatBool(next.RegistrationVerification),
		SettingSystemName:                next.SystemName,

		SettingLockoutThreshold:       strconv.Itoa(next.LockoutThreshold),
		SettingLockoutDurationMinutes: strconv.Itoa(next.LockoutDurationMinutes),

		SettingPasswordMinLength:        strconv.Itoa(next.PasswordMinLength),
		SettingPasswordRequireUppercase: strconv.FormatBool(next.PasswordRequireUppercase),
		SettingPasswordRequireLowercase: strconv.FormatBool(next.PasswordRequireLowercase),
		SettingPasswordRequireDigit:     strconv.FormatBool(next.PasswordRequireDigit),
		SettingPasswordRequireSymbol:    strconv.FormatBool(next.PasswordRequireSymbol),
		SettingPasswordHistoryDepth:     strconv.Itoa(next.PasswordHistoryDepth),
		SettingPasswordMaxAgeDays:       strconv.Itoa(next.PasswordMaxAgeDays),

		SettingAuditRetentionDays: strconv.Itoa(next.AuditRetentionDays),

		SettingDefaultLocale: next.DefaultLocale,
	}

	if err := s.store.ForTenant(tenantID).UpsertSettings(ctx, values, store.Now()); err != nil {
		return Settings{}, fmt.Errorf("save settings: %w", err)
	}

	s.mu.Lock()
	s.cache[tenantID] = next
	s.mu.Unlock()

	return next, nil
}

// RegistrationEnabled is a convenience read used by the registration path.
func (s *SettingsService) RegistrationEnabled(ctx context.Context, tenantID string) (bool, error) {
	settings, err := s.Get(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return settings.RegistrationEnabled, nil
}

// localeList renders the locales this build ships, for an error message that
// tells somebody what they could have chosen instead.
func localeList() string {
	locales := i18n.Supported()
	out := make([]string, len(locales))
	for i, locale := range locales {
		out[i] = string(locale)
	}
	return strings.Join(out, ", ")
}
