// Package server wires configuration, storage, and HTTP routing together.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/handler"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/notify"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	handler    *handler.Handler
	middleware *auth.Middleware
	users      *service.UserService
	tenants    *service.TenantService
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

	tokens := auth.NewTokenService(cfg.JWTSecret)
	audit := service.NewAuditService(st)
	settings := service.NewSettingsService(st, cfg.TokenTTL)
	users := service.NewUserService(st, audit, settings, tokens)
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

	s := &Server{
		cfg:        cfg,
		store:      st,
		handler:    handler.New(users, orgs, audit, settings, tenants, recovery),
		middleware: auth.NewMiddleware(tokens, users),
		users:      users,
		tenants:    tenants,
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

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.router }

// Close releases resources held by the server.
func (s *Server) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}
