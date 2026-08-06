// Package server wires configuration, storage, and HTTP routing together.
package server

import (
	"net/http"

	"github.com/paraview/keylite/internal/config"
	"github.com/paraview/keylite/internal/store"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	router http.Handler
}

// New builds a Server from cfg, opening the database and applying any
// pending migrations. The caller must call Close when done.
func New(cfg *config.Config) (*Server, error) {
	st, err := store.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	s := &Server{cfg: cfg, store: st}
	s.router = s.routes()
	return s, nil
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
