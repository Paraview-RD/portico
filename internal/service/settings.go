// Package service holds the business logic that sits between the HTTP
// handlers and the store.
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

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
)

// Settings is the full set of runtime settings for one tenant.
type Settings struct {
	TokenTTLMinutes     int    `json:"tokenTtlMinutes"`
	RegistrationEnabled bool   `json:"registrationEnabled"`
	SystemName          string `json:"systemName"`
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

	values := map[string]string{
		SettingTokenTTLMinutes:     strconv.Itoa(next.TokenTTLMinutes),
		SettingRegistrationEnabled: strconv.FormatBool(next.RegistrationEnabled),
		SettingSystemName:          next.SystemName,
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
