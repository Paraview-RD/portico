// Package server wires configuration, storage, and HTTP routing together.
package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/casp"
	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/handler"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/metrics"
	"github.com/paraview/portico/internal/notify"
	"github.com/paraview/portico/internal/oidcp"
	"github.com/paraview/portico/internal/samlp"
	"github.com/paraview/portico/internal/scim"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	handler    *handler.Handler
	middleware *auth.Middleware
	metrics    *metrics.Registry
	users      *service.UserService
	tenants    *service.TenantService
	settings   *service.SettingsService
	oidc       *oidcp.Providers
	saml       *samlp.Providers
	cas        *casp.Server
	scim       *scim.Handler
	router     http.Handler
}

// Option overrides a dependency New would otherwise build from cfg.
//
// There are two, both for message delivery, and both exist so a test can
// substitute a recorder. Password recovery's most important property — that
// a token goes to the account's own bound address and to nothing else — can
// only be asserted by something that sees the message, and asserting it
// against a real mail server would be a test of the mail server.
type Option func(*dependencies)

type dependencies struct {
	mailer notify.Mailer
	sms    notify.SMSSender
}

// WithMailer replaces the configured SMTP sender.
func WithMailer(m notify.Mailer) Option {
	return func(d *dependencies) { d.mailer = m }
}

// WithSMSSender replaces the SMS sender.
func WithSMSSender(sender notify.SMSSender) Option {
	return func(d *dependencies) { d.sms = sender }
}

// New builds a Server from cfg, opening the database and applying any
// pending migrations. The caller must call Close when done.
func New(cfg *config.Config, opts ...Option) (*Server, error) {
	st, err := store.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	httpx.TrustProxyHeaders(cfg.TrustProxyHeaders)

	// Always built, even when no endpoint publishes it. The recording calls
	// are a counter increment each, and making them conditional would put an
	// `if metrics != nil` at every call site — which is where a metric
	// quietly stops being recorded and nobody notices, because the only
	// symptom is a flat line.
	registry := metrics.New()
	registry.MustRegister(newDatabaseCollector(st))

	tokens := auth.NewTokenService(cfg.JWTSecret)
	audit := service.NewAuditService(st)
	sessions := service.NewSessionService(st, audit)
	settings := service.NewSettingsService(st, cfg.TokenTTL)
	users := service.NewUserService(st, audit, settings, tokens, registry)
	orgs := service.NewOrganizationService(st, audit)
	tenants := service.NewTenantService(st)

	mailer, err := notify.NewMailer(cfg.SMTP)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	// V0.1 ships the SMS interface and no provider; see internal/notify.
	deps := dependencies{mailer: mailer, sms: notify.NotConfiguredSMS{}}
	for _, opt := range opts {
		opt(&deps)
	}

	recovery := service.NewRecoveryService(
		st, users, audit, deps.mailer, deps.sms, cfg.PublicURL)

	clients := service.NewOAuthClientService(st, audit)
	keys := service.NewSigningKeyService(st)
	providers := oidcp.NewProviders(cfg.PublicURL, federationCryptoKey(cfg.JWTSecret),
		st, tenants, users, clients, keys, audit)

	serviceProviders := service.NewSAMLServiceProviderService(st, audit)
	samlKeys := service.NewSAMLKeyService(st)
	samlProviders := samlp.NewProviders(cfg.PublicURL,
		st, tenants, users, serviceProviders, samlKeys, audit)

	casServices := service.NewCASService(st, users, audit)
	casServer := casp.New(cfg.PublicURL, tenants, casServices, audit)

	scimCredentials := service.NewSCIMCredentialService(st, audit)
	scimHandler := scim.NewHandler(users, scimCredentials, cfg.PublicURL)

	s := &Server{
		cfg:   cfg,
		store: st,
		handler: handler.New(users, orgs, audit, settings, tenants, recovery, sessions,
			clients, serviceProviders, samlKeys, casServices, scimCredentials,
			providers, samlProviders, casServer),
		middleware: auth.NewMiddleware(tokens, users, sessions),
		metrics:    registry,
		scim:       scimHandler,
		users:      users,
		tenants:    tenants,
		settings:   settings,
		oidc:       providers,
		saml:       samlProviders,
		cas:        casServer,
	}
	s.router = s.routes()
	return s, nil
}

// Bootstrap performs the one-time setup a brand-new instance needs. It is
// separate from New so tests can skip it.
//
// The default tenant comes first, because everything else belongs to one.
// A deployment that never creates another tenant behaves exactly as it did
// before tenancy existed, which is the point: sign-in resolves to the
// default when no tenant is named.
func (s *Server) Bootstrap(ctx context.Context) error {
	tenant, err := s.tenants.EnsureDefault(ctx)
	if err != nil {
		return err
	}

	created, generatedPassword, err := s.users.EnsureInitialAdmin(
		ctx, tenant.ID, s.cfg.InitialAdminUsername, s.cfg.InitialAdminPassword)
	if err != nil {
		return err
	}

	if created {
		slog.Info("created the initial administrator account",
			"username", s.cfg.InitialAdminUsername, "tenant", tenant.Code)
	}

	if created && generatedPassword != "" {
		// Deliberately not through the structured logger. Under any normal
		// deployment those records are shipped to an aggregator, where a
		// credential would persist indefinitely, be searchable, and be
		// readable by a far wider group than "people who may administer
		// this system". Writing it to stderr as plain text keeps it in the
		// operator's terminal on first run without entering the log
		// pipeline.
		fmt.Fprintf(os.Stderr, `
────────────────────────────────────────────────────────────────
  Initial administrator created

    tenant:    %s
    username:  %s
    password:  %s

  This password is shown once and stored nowhere. Sign in and
  change it. To choose it yourself instead, set
  PORTICO_INITIAL_ADMIN_PASSWORD before first start.
────────────────────────────────────────────────────────────────

`, tenant.Code, s.cfg.InitialAdminUsername, generatedPassword)
	}

	return nil
}

// federationCryptoKey derives the key the OpenID Provider encrypts
// authorization codes with.
//
// Derived from the signing secret rather than generated, because a random
// key would invalidate every sign-in in flight each time the process
// restarted — and because a deployment already has exactly one secret to
// look after. Hashed rather than truncated so that the two uses of the
// secret cannot be played against each other.
func federationCryptoKey(secret []byte) [32]byte {
	return sha256.Sum256(append([]byte("portico/oidc/code-encryption\x00"), secret...))
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.router }

// SweepExpired deletes the rows that exist only until something happens and
// are then dead weight: authorization and authentication requests nobody
// completed, service tickets nobody validated, reset links that have gone
// cold, and refresh-token chains no longer capable of anything.
//
// It is one call rather than several because they share a schedule and a
// failure mode, and because a periodic job that only cleans some of what
// grows is a job somebody will assume covers the rest. The caller decides
// how often; see cmd/server.
//
// Audit entries are removed only where a tenant has explicitly configured a
// retention period. The default is to keep everything: the trail is the
// record of what happened, not an operational buffer, and a product that
// quietly started deleting it on a timer would be doing the worst thing an
// audit log can do. So the timer runs either way and does nothing unless
// somebody asked.
func (s *Server) SweepExpired(ctx context.Context) error {
	if err := s.oidc.SweepExpired(ctx); err != nil {
		return err
	}
	if err := s.saml.SweepExpired(ctx); err != nil {
		return err
	}
	if err := s.cas.SweepExpired(ctx); err != nil {
		return err
	}
	return s.sweepCredentialRemnants(ctx)
}

// Retention windows for rows that are dead but not yet worth deleting.
//
// Both are generous on purpose. These are the rows somebody consults when
// answering "was a reset link issued for this account" or "when did this
// application last refresh", and a sweep that ran to the letter of expiry
// would delete the answer the day the question becomes interesting.
const (
	passwordResetRetention = 30 * 24 * time.Hour
	refreshTokenRetention  = 30 * 24 * time.Hour
	sessionRetention       = 30 * 24 * time.Hour
)

// sweepCredentialRemnants clears spent password resets and dead refresh
// token chains, per tenant.
func (s *Server) sweepCredentialRemnants(ctx context.Context) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	now := store.Now()
	for _, tenant := range tenants {
		q := s.store.ForTenant(tenant.ID)

		if err := q.DeleteExpiredPasswordResets(ctx, now.Add(-passwordResetRetention)); err != nil {
			return fmt.Errorf("sweep password resets for tenant %s: %w", tenant.Code, err)
		}
		if err := q.DeleteDeadRefreshTokenChains(ctx, now.Add(-refreshTokenRetention)); err != nil {
			return fmt.Errorf("sweep refresh tokens for tenant %s: %w", tenant.Code, err)
		}
		if err := q.DeleteExpiredSessions(ctx, now.Add(-sessionRetention)); err != nil {
			return fmt.Errorf("sweep sessions for tenant %s: %w", tenant.Code, err)
		}

		settings, err := s.settings.Get(ctx, tenant.ID)
		if err != nil {
			return fmt.Errorf("read settings for tenant %s: %w", tenant.Code, err)
		}
		if settings.AuditRetentionDays > 0 {
			cutoff := now.AddDate(0, 0, -settings.AuditRetentionDays)
			if err := q.DeleteAuditLogsBefore(ctx, cutoff); err != nil {
				return fmt.Errorf("prune audit log for tenant %s: %w", tenant.Code, err)
			}
		}
	}
	return nil
}

// Close releases resources held by the server.
func (s *Server) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}
