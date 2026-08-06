// Package server wires configuration, storage, and HTTP routing together.
package server

import (
	"net/http"

	"github.com/paraview/keylite/internal/config"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg    *config.Config
	router http.Handler
}

// New builds a Server from cfg, opening any resources it needs. The caller
// must call Close when done.
func New(cfg *config.Config) (*Server, error) {
	s := &Server{cfg: cfg}
	s.router = s.routes()
	return s, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.router }

// Close releases resources held by the server.
func (s *Server) Close() error { return nil }
