package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/keylite/internal/httpx"
)

// Version is the running build's version, overridden at link time:
//
//	go build -ldflags "-X github.com/paraview/keylite/internal/server.Version=v0.1.0"
var Version = "dev"

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.NotFound("ROUTE_NOT_FOUND", "No such endpoint."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.NewError(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"This endpoint does not support "+r.Method+".",
		))
	})

	return r
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// handleHealth reports process liveness. It stays dependency-free so that a
// failing database does not make the process look dead to an orchestrator.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.OK(w, healthResponse{Status: "ok", Version: Version})
}
