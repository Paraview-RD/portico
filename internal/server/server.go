// Package server wires configuration, storage, and HTTP routing together.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/paraview/keylite/internal/auth"
	"github.com/paraview/keylite/internal/config"
	"github.com/paraview/keylite/internal/handler"
	"github.com/paraview/keylite/internal/httpx"
	"github.com/paraview/keylite/internal/service"
	"github.com/paraview/keylite/internal/store"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	handler    *handler.Handler
	middleware *auth.Middleware
	users      *service.UserService
	router     http.Handler
}

// New builds a Server from cfg, opening the database and applying any
// pending migrations. The caller must call Close when done.
func New(cfg *config.Config) (*Server, error) {
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

	s := &Server{
		cfg:        cfg,
		store:      st,
		handler:    handler.New(users, orgs, audit, settings),
		middleware: auth.NewMiddleware(tokens, users),
		users:      users,
	}
	s.router = s.routes()
	return s, nil
}

// Bootstrap performs the one-time setup a brand-new instance needs. It is
// separate from New so tests can skip it.
func (s *Server) Bootstrap(ctx context.Context) error {
	created, generatedPassword, err := s.users.EnsureInitialAdmin(
		ctx, s.cfg.InitialAdminUsername, s.cfg.InitialAdminPassword)
	if err != nil {
		return err
	}

	if created {
		slog.Info("created the initial administrator account",
			"username", s.cfg.InitialAdminUsername)
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

    username:  %s
    password:  %s

  This password is shown once and stored nowhere. Sign in and
  change it. To choose it yourself instead, set
  KEYLITE_INITIAL_ADMIN_PASSWORD before first start.
────────────────────────────────────────────────────────────────

`, s.cfg.InitialAdminUsername, generatedPassword)
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
