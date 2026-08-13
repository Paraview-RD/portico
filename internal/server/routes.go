package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/casp"
	"github.com/Paraview-RD/portico/internal/docs"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/oidcp"
	"github.com/Paraview-RD/portico/internal/samlp"
	"github.com/Paraview-RD/portico/internal/scim"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/web"
)

// Version is the running build's version, overridden at link time:
//
//	go build -ldflags "-X github.com/Paraview-RD/portico/internal/server.Version=v0.1.0"
var Version = "dev"

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)
	// After Recover, so a panicked request is counted as the 500 it becomes
	// rather than escaping the count entirely.
	r.Use(s.metrics.Middleware)
	// After the log and the counter, so a refusal is visible in both — a
	// throttle nobody can see firing is one that gets blamed for outages it
	// did not cause. Before routing, so a refused request never reaches the
	// handler that would have hashed a password for it.
	r.Use(httpx.RateLimitAuth(httpx.NewRateLimiter(s.cfg.AuthRateLimit, s.cfg.AuthRateLimitBurst)))

	h := s.handler
	mw := s.middleware

	r.Route("/api/v1", func(r chi.Router) {
		// --- Public ---------------------------------------------------
		r.Get("/health", s.handleHealth)
		// Liveness and readiness are separate on purpose; see handleReady.
		r.Get("/ready", s.handleReady)
		r.Post("/auth/login", h.Login)
		r.Post("/auth/register", h.Register)
		// Lets the sign-in screen decide whether to offer registration.
		r.Get("/auth/registration-status", h.RegistrationStatus)

		// Password recovery (§3.5). All three are public by necessity: the
		// caller is someone who cannot sign in. None reveals whether an
		// account exists.
		// Signing in through somebody else's provider. Public for the same
		// reason recovery is: the caller cannot sign in yet.
		r.Get("/auth/external/providers", h.ExternalSignInOptions)
		r.Post("/auth/external/start", h.StartExternalSignIn)
		// Where a provider sends the browser back. It carries nothing this
		// server issued but the state, and the state is what judges it.
		r.Get("/auth/external/callback", h.CompleteExternalSignIn)

		r.Get("/auth/recovery-channels", h.RecoveryChannels)
		r.Post("/auth/password-recovery", h.RequestPasswordRecovery)
		r.Post("/auth/password-recovery/confirm", h.ConfirmPasswordRecovery)

		// Also public by necessity: the caller cannot sign in, because
		// Login refuses an expired password rather than issuing a token
		// and trusting the client to act on a flag. It takes the current
		// password and refuses if that password has not expired.
		r.Post("/auth/password/expired", h.ChangeExpiredPassword)

		// Proving the address a self-registration gave. Public by
		// necessity: the account cannot sign in until it succeeds, which is
		// the entire point.
		r.Post("/auth/register/verify", h.ConfirmRegistration)
		r.Post("/auth/register/verify/resend", h.ResendVerification)

		// --- Any signed-in user ---------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireAuth)

			r.Post("/auth/logout", h.Logout)
			// Ends this session. Signing out on a laptop no longer signs
			// you out on a phone; that is what the second one is for.
			r.Post("/auth/logout-everywhere", h.LogoutEverywhere)

			r.Get("/users/me/groups", h.ListOwnGroups)
			r.Get("/users/me/sessions", h.ListOwnSessions)
			r.Delete("/users/me/sessions/{sessionID}", h.RevokeOwnSession)
			// Linking an external identity to one's own account. The account
			// comes from the session; a caller-supplied one here would be the
			// whole vulnerability the journey is arranged to avoid.
			r.Get("/users/me/external-identities", h.ListMyExternalIdentities)
			r.Post("/users/me/external-identities/{id}/start", h.StartExternalBinding)
			r.Delete("/users/me/external-identities/{id}", h.UnlinkMyExternalIdentity)

			// The seam between Portico's own sign-in and the OpenID
			// Provider: the sign-in screen calls this once somebody has
			// authenticated, and is told where to send the browser next.
			r.Post("/oauth/authorize", h.Authorize)
			r.Post("/saml/authenticate", h.Authenticate)
			r.Post("/cas/authorize", h.CASAuthorize)
			r.Get("/users/me", h.Me)
			r.Put("/users/me", h.UpdateOwnProfile)
			// The descriptive attributes, which anybody may maintain about
			// themselves. It cannot reach role, status, or organization —
			// see the handler for why that makes it safe to expose.
			r.Put("/users/me/profile", h.SetOwnProfileAttributes)
			r.Post("/users/me/password", h.ChangeOwnPassword)
			// The one sanctioned way to disable yourself. Everywhere else
			// that is refused; see the handler for why this is not an
			// exception to that rule but the case it was never about.
			r.Post("/users/me/close", h.CloseOwnAccount)

			// Open endpoints for downstream systems (§3.7). They are
			// deliberately readable by any authenticated caller: a
			// downstream service acts with the user's own token.
			r.Get("/auth/permission-check", h.CheckPermission)

			// Reading the organization list is needed by the profile
			// screen; writing is administrator-only, below.
			r.Get("/organizations", h.ListOrganizations)
			r.Get("/organizations/{id}", h.GetOrganization)

			// The home screen for somebody who is not an administrator.
			// Every other application endpoint requires one, which is why
			// this is a separate, narrower view rather than a permission on
			// the existing lists: what it returns is a name and a link, not
			// a registration.
			r.Get("/portal/applications", h.PortalApplications)
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
				// The descriptive attributes, apart from the statement that
				// changes role, status, and organization — a form editing a
				// job title must not be able to send a role at all.
				r.Put("/{id}/profile", h.SetUserProfile)
				// The attributes this tenant defined for itself, kept apart
				// from the profile for the same reason the profile is kept
				// apart from role and status: one form editing one kind of
				// thing cannot reach the others by accident.
				r.Get("/{id}/attributes", h.GetUserAttributeValues)
				r.Put("/{id}/attributes", h.SetUserAttributeValues)
				// The tenant's accounts as a spreadsheet, taking the same
				// filters the listing does. Audited: this is every
				// attribute of every account leaving in one request.
				r.Get("/export", h.ExportUsers)
				// Several at a time, each through the path a single one
				// takes — so the rules that protect the last administrator
				// are not bypassed by selecting more people.
				r.Post("/bulk/status", h.BulkSetUserStatus)
				r.Post("/bulk/organization", h.BulkSetUserOrganization)
				r.Post("/{id}/enable", h.EnableUser)
				r.Post("/{id}/disable", h.DisableUser)
				r.Post("/{id}/password", h.ResetUserPassword)
				// Clearing a lockout without touching the password: the
				// person mistyped, they did not lose the password.
				r.Post("/{id}/unlock", h.UnlockUser)

				// What is signed in as this person, and ending one of them.
				r.Get("/{id}/groups", h.ListUserGroups)
				r.Get("/{id}/sessions", h.ListUserSessions)
				r.Delete("/{id}/sessions/{sessionID}", h.RevokeUserSession)

				// Bulk import (§3.1). The template is generated from the
				// same column list the parser reads, so the two cannot drift.
				r.Post("/import", h.ImportUsers)
				r.Get("/import/template", h.ImportTemplate)
			})

			// Groups are sets of people; organizations below are the org
			// chart. Separate concepts, separate screens.
			r.Route("/groups", func(r chi.Router) {
				r.Get("/", h.ListGroups)
				r.Post("/", h.CreateGroup)
				r.Get("/{id}", h.GetGroup)
				r.Put("/{id}", h.UpdateGroup)
				r.Delete("/{id}", h.DeleteGroup)
				r.Get("/{id}/members", h.ListGroupMembers)
				r.Post("/{id}/members", h.AddGroupMembers)
				r.Delete("/{id}/members/{userID}", h.RemoveGroupMember)
			})

			r.Post("/organizations", h.CreateOrganization)
			// Whoever is responsible for an organization, and the people
			// attached to it beside their primary membership. Neither
			// grants anything; see the handlers.
			r.Put("/organizations/{id}/manager", h.SetOrganizationManager)
			r.Post("/organizations/{id}/attachments", h.AttachUserToOrganization)
			r.Delete("/organizations/{id}/attachments/{userID}", h.DetachUserFromOrganization)
			r.Put("/organizations/{id}", h.UpdateOrganization)
			r.Post("/organizations/{id}/enable", h.EnableOrganization)
			r.Post("/organizations/{id}/disable", h.DisableOrganization)

			r.Get("/audit-logs", h.ListAuditLogs)

			r.Get("/settings", h.GetSettings)
			r.Put("/settings", h.UpdateSettings)

			// Application management, one group per protocol. Everything an
			// integrator needs from this end is at /integration-endpoints,
			// derived from what the server actually serves rather than
			// retyped into a screen.
			r.Get("/applications/integration-endpoints", h.IntegrationEndpoints)

			// Uploading the picture for a tile. Not nested under a protocol,
			// because one picture is one picture whichever of the three the
			// application speaks — and the upload has to happen before the
			// registration form is saved, so it cannot belong to a client
			// that does not exist yet.
			r.Post("/applications/logos", h.UploadApplicationLogo)

			r.Route("/applications/oauth-clients", func(r chi.Router) {
				r.Get("/", h.ListClients)
				r.Post("/", h.CreateClient)
				r.Get("/{clientID}", h.GetClient)
				r.Put("/{clientID}", h.UpdateClient)
				r.Post("/{clientID}/enable", h.EnableClient)
				r.Post("/{clientID}/disable", h.DisableClient)
				r.Post("/{clientID}/rotate-secret", h.RotateClientSecret)

				// What this application receives, and under what name. An
				// empty list means the documented defaults, which is what
				// every application registered before this feature existed
				// has and keeps.
				r.Get("/{clientID}/field-mappings",
					h.ListFieldMappings(service.RecipientOAuthClient, "clientID"))
				r.Put("/{clientID}/field-mappings",
					h.ReplaceFieldMappings(service.RecipientOAuthClient, "clientID"))
			})

			// These two are addressed by the registration's own id, not by
			// its entity id or URL prefix. Those are a URI and a URL, so
			// putting one in a path segment means percent-encoding its
			// slashes — and a proxy that normalizes paths decodes them
			// again, splitting the identifier and 404ing every request. The
			// failure would depend on somebody else's proxy configuration
			// and appear only in production.
			r.Route("/applications/saml-service-providers", func(r chi.Router) {
				r.Get("/", h.ListServiceProviders)
				r.Post("/", h.CreateServiceProvider)
				r.Get("/{id}", h.GetServiceProvider)
				r.Put("/{id}", h.UpdateServiceProvider)
				r.Post("/{id}/enable", h.EnableServiceProvider)
				r.Post("/{id}/disable", h.DisableServiceProvider)

				r.Get("/{id}/field-mappings",
					h.ListFieldMappings(service.RecipientSAMLProvider, "id"))
				r.Put("/{id}/field-mappings",
					h.ReplaceFieldMappings(service.RecipientSAMLProvider, "id"))
			})

			r.Route("/applications/cas-services", func(r chi.Router) {
				r.Get("/", h.ListCASServices)
				r.Post("/", h.CreateCASService)
				r.Get("/{id}", h.GetCASService)
				r.Put("/{id}", h.UpdateCASService)
				r.Post("/{id}/enable", h.EnableCASService)
				r.Post("/{id}/disable", h.DisableCASService)

				r.Get("/{id}/field-mappings",
					h.ListFieldMappings(service.RecipientCASService, "id"))
				r.Put("/{id}/field-mappings",
					h.ReplaceFieldMappings(service.RecipientCASService, "id"))
			})

			// Issuing and revoking the credentials a directory syncs with.
			// The SCIM API itself is elsewhere and authenticates differently;
			// this is only the administrative side of it.
			// Outbound subscriptions, and the delivery history that answers
			// "we are not receiving anything" without asking the receiver.
			r.Route("/external-identity-providers", func(r chi.Router) {
				r.Get("/", h.ListExternalIDPs)
				r.Post("/", h.CreateExternalIDP)
				r.Put("/{id}", h.UpdateExternalIDP)
				r.Post("/{id}/enable", h.EnableExternalIDP)
				r.Post("/{id}/disable", h.DisableExternalIDP)
				r.Delete("/{id}", h.DeleteExternalIDP)
			})

			r.Route("/webhooks", func(r chi.Router) {
				r.Get("/", h.ListWebhooks)
				r.Post("/", h.CreateWebhook)
				r.Get("/events", h.WebhookEvents)
				r.Get("/{id}/deliveries", h.ListWebhookDeliveries)
				r.Post("/{id}/rotate-secret", h.RotateWebhookSecret)
				r.Post("/{id}/enable", h.EnableWebhook)
				r.Post("/{id}/disable", h.DisableWebhook)
				r.Post("/{id}/snapshot", h.SnapshotWebhook)
				r.Delete("/{id}", h.DeleteWebhook)

				// The same rules as an application's, over the event body
				// rather than over a claim set. A subscription with none
				// receives what it always received.
				r.Get("/{id}/field-mappings",
					h.ListFieldMappings(service.RecipientWebhook, "id"))
				r.Put("/{id}/field-mappings",
					h.ReplaceFieldMappings(service.RecipientWebhook, "id"))
			})

			// Directories Portico reads accounts out of, which is the
			// opposite direction from the SCIM credentials below: those let
			// a directory push, these reach out and read.
			r.Route("/directories", func(r chi.Router) {
				r.Get("/", h.ListDirectories)
				r.Post("/", h.CreateDirectory)
				r.Get("/{id}", h.GetDirectory)
				r.Put("/{id}", h.UpdateDirectory)
				r.Post("/{id}/enable", h.EnableDirectory)
				r.Post("/{id}/disable", h.DisableDirectory)
				r.Post("/{id}/sync", h.SyncDirectory)
				r.Get("/{id}/runs", h.ListDirectoryRuns)
			})

			// Everything that may be mapped, in either direction: the
			// built-in vocabulary and this tenant's own together. Read-only,
			// and the picker on every mapping form is drawn from it.
			r.Get("/fields", h.ListFields)

			r.Route("/user-attributes", func(r chi.Router) {
				r.Get("/", h.ListUserAttributes)
				r.Post("/", h.DefineUserAttribute)
				r.Put("/{id}", h.UpdateUserAttribute)
				// Retiring keeps every recorded value; deleting discards
				// them. Two verbs rather than one, because the second is not
				// recoverable and should not be reachable by a checkbox.
				r.Post("/{id}/enable", h.EnableUserAttribute)
				r.Post("/{id}/disable", h.DisableUserAttribute)
				r.Delete("/{id}", h.DeleteUserAttribute)
			})

			r.Route("/scim-credentials", func(r chi.Router) {
				r.Get("/", h.ListSCIMCredentials)
				r.Post("/", h.CreateSCIMCredential)
				r.Post("/{id}/enable", h.EnableSCIMCredential)
				r.Post("/{id}/disable", h.DisableSCIMCredential)
				r.Delete("/{id}", h.DeleteSCIMCredential)
			})
		})
	})

	// SCIM, outside /api/v1 because it is not this project's API: it has its
	// own media type, its own error shape, and its own authentication. A
	// client here holds a provisioning credential, not a session.
	r.Mount(scim.Mount, s.scim.Routes())

	s.mountFederation(r)

	// Anything outside /api/v1 is the single-page app. API 404s keep
	// returning the JSON envelope; only non-API paths fall through to the
	// UI, so a mistyped endpoint never returns HTML to an API client.
	// The manual, before the single-page application's catch-all. Public on
	// purpose: most of what it explains is how to configure a deployment
	// somebody has not signed into yet, and it names where credentials come
	// from rather than what any of them are.
	r.Handle(docs.Prefix, http.RedirectHandler(docs.Prefix+"/", http.StatusMovedPermanently))
	r.Handle(docs.Prefix+"/*", docsPolicy(docs.Handler()))

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

// federationEndpoints are the OpenID Provider paths Portico serves,
// relative to an issuer.
//
// They are listed rather than delegated wholesale to the protocol library's
// router, so that the surface is a decision recorded here. The library also
// routes a device-authorization endpoint and its own liveness probes; the
// first is a grant this version does not implement, and the second would be
// a second health endpoint saying something different from /api/v1/health.
var federationEndpoints = []string{
	"/.well-known/openid-configuration",
	"/authorize",
	"/authorize/callback",
	"/oauth/token",
	"/oauth/introspect",
	"/userinfo",
	"/revoke",
	"/end_session",
	"/keys",
}

// mountFederation serves each tenant's OpenID Provider under /t/<code>, and
// the default tenant's additionally at the root.
//
// The root alias exists so that a deployment with one tenant has the issuer
// people expect — https://id.example.com, not https://id.example.com/t/default
// — and never has to explain tenants to an integrator. The two are separate
// issuers over the same accounts and the same keys, which is why an
// authorization request records the one it arrived at.
func (s *Server) mountFederation(r chi.Router) {
	root := s.oidc.Handler("")
	for _, path := range federationEndpoints {
		r.Handle(path, root)
	}

	samlRoot := s.saml.Handler("")
	for _, path := range samlEndpoints {
		r.Handle(path, samlRoot)
	}

	casRoot := s.cas.Handler("")
	for _, path := range casp.Paths() {
		r.Handle(path, casRoot)
	}

	r.Route(oidcp.TenantPathPrefix+"{tenant}", func(r chi.Router) {
		byTenant := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// The mount comes off the path before the provider sees it,
			// because the provider routes relative to its own issuer.
			s.oidc.Handler(oidcp.TenantMount(chi.URLParam(req, "tenant"))).ServeHTTP(w, req)
		})
		for _, path := range federationEndpoints {
			r.Handle(path, byTenant)
		}

		samlByTenant := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			s.saml.Handler(samlp.TenantMount(chi.URLParam(req, "tenant"))).ServeHTTP(w, req)
		})
		for _, path := range samlEndpoints {
			r.Handle(path, samlByTenant)
		}

		casByTenant := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			s.cas.Handler(casp.TenantMount(chi.URLParam(req, "tenant"))).ServeHTTP(w, req)
		})
		for _, path := range casp.Paths() {
			r.Handle(path, casByTenant)
		}

		// An uploaded tile picture, served without credentials because a tile
		// is drawn on the sign-in screen before anybody has any. It is under
		// this prefix rather than at the root for the reason the federation
		// endpoints are: the row belongs to a tenant, and a request with no
		// principal has nowhere else to learn which one. The lookup filters on
		// it — see store.Scoped.GetApplicationLogo.
		r.Get("/logos/{logoID}", s.handler.ApplicationLogo)
	})
}

// samlEndpoints are the SAML paths Portico serves, relative to an issuer.
//
// Three, and no single logout. SAML's logout profile requires the identity
// provider to reach every service provider a person signed in to, in the
// browser, and to cope with any of them being unreachable; a half-working
// one is worse than none, because it reports having ended sessions it did
// not. See docs/federation.md.
var samlEndpoints = []string{
	samlp.MetadataPath,
	samlp.SSOPath,
	samlp.CallbackPath,
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

// readinessResponse says whether this instance can actually serve.
type readinessResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	// Database is "ok" or the reason it is not.
	Database string `json:"database"`
}

// readinessProbeTimeout bounds the check. A database that is reachable but
// wedged should read as not ready rather than hang the probe until the
// orchestrator's own timeout, which is usually much longer and looks like a
// stuck process rather than a failing dependency.
const readinessProbeTimeout = 2 * time.Second

// handleReady reports whether this instance can serve requests, which means
// whether it can reach its database.
//
// Separate from /health rather than folded into it, because the two answer
// different questions and an orchestrator does different things with them.
// Liveness asks "is this process broken, should I restart it" — and a
// database outage is not fixed by restarting every instance; doing so turns
// one failing dependency into a restart loop across the fleet at the moment
// it is least able to cope. Readiness asks "should I send traffic here", and
// there the answer during an outage is no.
//
// That is why /health stays dependency-free. This endpoint is the place to
// add a dependency check, not that one.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessProbeTimeout)
	defer cancel()

	if err := s.store.DB().PingContext(ctx); err != nil {
		// The reason is logged with the request, not returned: this endpoint
		// is reachable without authentication, and a driver error can carry
		// a host name, a port, and sometimes a user.
		slog.WarnContext(ctx, "readiness probe failed", "error", err)
		httpx.Fail(w, r, httpx.NewError(
			http.StatusServiceUnavailable,
			"NOT_READY",
			"This instance cannot reach its database.",
		))
		return
	}

	httpx.OK(w, readinessResponse{
		Status: "ready", Version: Version, Database: "ok",
	})
}

// docsPolicy widens script-src for the manual, and only for the manual.
//
// MkDocs Material puts three inline scripts on every page it builds, and the
// application's policy — script-src 'self' — blocks all three. One of them
// defines __md_get, so blocking it made Material's bundle throw on load: the
// manual has been served with a dead search box and an unresponsive
// light/dark toggle, reporting nothing, because a Content-Security-Policy is
// enforced by a browser and by no test that runs without one.
//
// The header is replaced rather than added to. SecurityHeaders has already
// set the application's policy by the time this runs, and two
// Content-Security-Policy headers are intersected rather than merged — the
// stricter one still blocks, so appending would look like a fix and change
// nothing.
//
// Hashes rather than 'unsafe-inline', and scoped to this subtree rather than
// set globally, because /docs is same-origin with a console holding a session
// token: permitting arbitrary inline script anywhere on this origin would
// spend exactly the protection the policy is there to buy.
func docsPolicy(next http.Handler) http.Handler {
	policy := httpx.ContentSecurityPolicy(docs.InlineScriptHashes()...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		next.ServeHTTP(w, r)
	})
}
