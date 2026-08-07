package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/web"
)

// Version is the running build's version, overridden at link time:
//
//	go build -ldflags "-X github.com/paraview/portico/internal/server.Version=v0.1.0"
var Version = "dev"

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)

	h := s.handler
	mw := s.middleware

	r.Route("/api/v1", func(r chi.Router) {
		// --- Public ---------------------------------------------------
		r.Get("/health", s.handleHealth)
		r.Post("/auth/login", h.Login)
		r.Post("/auth/register", h.Register)
		// Lets the sign-in screen decide whether to offer registration.
		r.Get("/auth/registration-status", h.RegistrationStatus)

		// Password recovery (§3.5). All three are public by necessity: the
		// caller is someone who cannot sign in. None reveals whether an
		// account exists.
		r.Get("/auth/recovery-channels", h.RecoveryChannels)
		r.Post("/auth/password-recovery", h.RequestPasswordRecovery)
		r.Post("/auth/password-recovery/confirm", h.ConfirmPasswordRecovery)

		// --- Any signed-in user ---------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireAuth)

			r.Post("/auth/logout", h.Logout)
			r.Get("/users/me", h.Me)
			r.Put("/users/me", h.UpdateOwnProfile)
			r.Post("/users/me/password", h.ChangeOwnPassword)

			// Open endpoints for downstream systems (§3.7). They are
			// deliberately readable by any authenticated caller: a
			// downstream service acts with the user's own token.
			r.Get("/auth/permission-check", h.CheckPermission)

			// Reading the organization list is needed by the profile
			// screen; writing is administrator-only, below.
			r.Get("/organizations", h.ListOrganizations)
			r.Get("/organizations/{id}", h.GetOrganization)
		})

		// --- Administrators only --------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Use(mw.RequireAdmin)

			r.Route("/users", func(r chi.Router) {
				r.Get("/", h.ListUsers)
				r.Post("/", h.CreateUser)
				r.Get("/{id}", h.GetUser)
				r.Put("/{id}", h.UpdateUser)
				r.Post("/{id}/enable", h.EnableUser)
				r.Post("/{id}/disable", h.DisableUser)
				r.Post("/{id}/password", h.ResetUserPassword)

				// Bulk import (§3.1). The template is generated from the
				// same column list the parser reads, so the two cannot drift.
				r.Post("/import", h.ImportUsers)
				r.Get("/import/template", h.ImportTemplate)
			})

			r.Post("/organizations", h.CreateOrganization)
			r.Put("/organizations/{id}", h.UpdateOrganization)
			r.Post("/organizations/{id}/enable", h.EnableOrganization)
			r.Post("/organizations/{id}/disable", h.DisableOrganization)

			r.Get("/audit-logs", h.ListAuditLogs)

			r.Get("/settings", h.GetSettings)
			r.Put("/settings", h.UpdateSettings)
		})
	})

	// Anything outside /api/v1 is the single-page app. API 404s keep
	// returning the JSON envelope; only non-API paths fall through to the
	// UI, so a mistyped endpoint never returns HTML to an API client.
	uiHandler := web.Handler()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			httpx.Fail(w, r, httpx.NotFound("ROUTE_NOT_FOUND", "No such endpoint."))
			return
		}
		uiHandler.ServeHTTP(w, r)
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
