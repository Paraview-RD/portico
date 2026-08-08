// Package service holds the business logic that sits between the HTTP
// handlers and the store.
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/store"
)

// Setting keys. These are the runtime-tunable values from §3.10.
const (
	// SettingTokenTTLMinutes is how long an issued token stays valid.
	SettingTokenTTLMinutes = "token_ttl_minutes"
	// SettingRegistrationEnabled gates self-service registration, letting
	// the same build serve a closed intranet and an open internet
	// deployment (§3.10).
	SettingRegistrationEnabled = "registration_enabled"
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
)

// Settings is the full set of runtime settings for one tenant.
type Settings struct {
	TokenTTLMinutes     int    `json:"tokenTtlMinutes"`
	RegistrationEnabled bool   `json:"registrationEnabled"`
	SystemName          string `json:"systemName"`

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

// TokenTTL is the token lifetime as a duration.
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

// SettingsService reads and writes runtime settings, caching them in memory
// because they are read on every login and change rarely.
//
// The cache is keyed by tenant. Settings are per-tenant — one tenant may
// accept sign-ups while another does not, and each names itself — so a
// single cached value would serve one tenant's configuration to another.
type SettingsService struct {
	store    *store.Store
	defaults Settings

	mu    sync.RWMutex
	cache map[string]Settings
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
		},
	}
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
		case SettingRegistrationEnabled:
			loaded.RegistrationEnabled = row.Value == "true"
		case SettingSystemName:
			loaded.SystemName = row.Value
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
		}
	}

	s.cache[tenantID] = loaded
	return loaded, nil
}

// Update writes a tenant's settings and refreshes its cache entry.
func (s *SettingsService) Update(ctx context.Context, tenantID string, next Settings) (Settings, error) {
	if next.TokenTTLMinutes < MinTokenTTLMinutes || next.TokenTTLMinutes > MaxTokenTTLMinutes {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Session lifetime must be between %d and %d minutes.",
				MinTokenTTLMinutes, MaxTokenTTLMinutes))
	}
	if next.SystemName == "" {
		next.SystemName = s.defaults.SystemName
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
	if next.PasswordMaxAgeDays < 0 || next.PasswordMaxAgeDays > MaxPasswordMaxAgeDays {
		return Settings{}, httpx.BadRequest("INVALID_SETTINGS",
			fmt.Sprintf("Password maximum age must be between 0 and %d days; "+
				"0 never expires.", MaxPasswordMaxAgeDays))
	}

	values := map[string]string{
		SettingTokenTTLMinutes:     strconv.Itoa(next.TokenTTLMinutes),
		SettingRegistrationEnabled: strconv.FormatBool(next.RegistrationEnabled),
		SettingSystemName:          next.SystemName,

		SettingLockoutThreshold:       strconv.Itoa(next.LockoutThreshold),
		SettingLockoutDurationMinutes: strconv.Itoa(next.LockoutDurationMinutes),

		SettingPasswordMinLength:        strconv.Itoa(next.PasswordMinLength),
		SettingPasswordRequireUppercase: strconv.FormatBool(next.PasswordRequireUppercase),
		SettingPasswordRequireLowercase: strconv.FormatBool(next.PasswordRequireLowercase),
		SettingPasswordRequireDigit:     strconv.FormatBool(next.PasswordRequireDigit),
		SettingPasswordRequireSymbol:    strconv.FormatBool(next.PasswordRequireSymbol),
		SettingPasswordHistoryDepth:     strconv.Itoa(next.PasswordHistoryDepth),
		SettingPasswordMaxAgeDays:       strconv.Itoa(next.PasswordMaxAgeDays),
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
