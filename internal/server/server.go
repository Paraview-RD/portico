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

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/casp"
	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/demo"
	"github.com/Paraview-RD/portico/internal/handler"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/notify"
	"github.com/Paraview-RD/portico/internal/oidcp"
	"github.com/Paraview-RD/portico/internal/samlp"
	"github.com/Paraview-RD/portico/internal/scim"
	"github.com/Paraview-RD/portico/internal/secrets"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// Server owns every long-lived dependency the HTTP layer needs.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	handler    *handler.Handler
	middleware *auth.Middleware
	metrics    *metrics.Registry
	webhooks   *service.WebhookService
	logos      *service.ApplicationLogoService
	// Held for the scheduled synchronization pass, which is the only thing
	// outside the HTTP layer that needs it.
	directories *service.DirectoryService
	// The client webhook deliveries go out through. One per server: its
	// transport pools connections, and its dialer is what refuses a
	// destination that has started resolving somewhere local.
	webhookClient *http.Client
	users         *service.UserService
	tenants       *service.TenantService
	settings      *service.SettingsService
	// Held for the sweep. A trial request reserves its tenant code from the
	// moment it is made, so an abandoned one keeps a name until something
	// takes it back — see SweepExpired.
	trials *service.TrialService
	oidc   *oidcp.Providers
	saml   *samlp.Providers
	cas    *casp.Server
	scim   *scim.Handler
	router http.Handler
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
	// The last stop before English for any message this deployment sends.
	settings.WithDefaultLocale(cfg.DefaultLocale)
	users := service.NewUserService(st, audit, settings, tokens, registry)
	orgs := service.NewOrganizationService(st, audit)
	tenants := service.NewTenantService(st).WithOperatorConsole(cfg.TenantConsole)

	mailer, err := notify.NewMailer(cfg.Mail)
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
		st, users, audit, settings, deps.mailer, deps.sms, cfg.PublicURL)

	// Attached after construction because the service that knows what this
	// deployment can send is built later — the same arrangement as
	// users.WithEvents below. It is what lets the settings service refuse a
	// verification requirement it could not satisfy.
	settings.WithDeliveryChannels(recovery.AvailableChannels)

	verification := service.NewVerificationService(
		st, users, settings, audit, deps.mailer, deps.sms, cfg.PublicURL)

	// Self-service trials. Constructed whether or not they are enabled — the
	// service refuses every call when they are not, and the routes are not
	// registered either, so this is a backstop rather than the gate.
	trials := service.NewTrialService(
		st, tenants, users, deps.mailer, audit,
		cfg.TrialSignup, cfg.TrialMaxTenants, cfg.TrialRatePerHour, cfg.PublicURL).
		WithBlockedEmailDomains(cfg.TrialBlockedEmailDomains).
		WithLocale(cfg.DefaultLocale).
		WithMetrics(registry).
		// One at a time. Filling a tenant is the heaviest thing this process
		// does and the free instance this demonstration runs on has a tenth of
		// a CPU; several at once is how it gets killed, which loses the fills
		// in flight along with everything else being served.
		WithFillLimit(1)

	clients := service.NewOAuthClientService(st, audit)
	keys := service.NewSigningKeyService(st)
	// Built here rather than beside the other application services below,
	// because the OpenID Provider needs them: what a client receives is
	// decided while its token is being assembled.
	fields := service.NewFieldCatalogue(st)
	fieldMappings := service.NewFieldMappingService(st, audit, fields)
	providers := oidcp.NewProviders(cfg.PublicURL, federationCryptoKey(cfg.JWTSecret),
		st, tenants, users, clients, keys, settings, fields, fieldMappings, audit)

	serviceProviders := service.NewSAMLServiceProviderService(st, audit)
	samlKeys := service.NewSAMLKeyService(st)
	samlProviders := samlp.NewProviders(cfg.PublicURL,
		st, tenants, users, serviceProviders, samlKeys, fields, fieldMappings, audit)

	casServices := service.NewCASService(st, users, audit)
	casServer := casp.New(cfg.PublicURL, tenants, casServices, fields, fieldMappings, audit)

	groups := service.NewGroupService(st, audit)
	logos := service.NewApplicationLogoService(st)
	attributes := service.NewUserAttributeService(st, audit)
	webhooks := service.NewWebhookService(st, audit).WithFieldMappings(fields, fieldMappings)
	// Attached after construction: the webhook service is built from the same
	// store and the account operations only need to know it exists.
	users.WithEvents(webhooks)
	groups.WithEvents(webhooks)
	orgs.WithEvents(webhooks)

	// The demonstration packs, attached here rather than at construction
	// because they need every service a pack creates something with, and those
	// are built above.
	//
	// Attached whether or not trials are enabled. What it changes when they are
	// not is nothing: no route reaches the trial service, and it refuses every
	// call anyway. What it would cost to make this conditional is a second
	// place where "are trials on" is decided.
	trials.WithFiller(demo.NewFiller(
		orgs, groups, users, attributes, clients, serviceProviders, casServices))

	scimCredentials := service.NewSCIMCredentialService(st, audit)

	// A nil vault when no key is configured, which is the default: the
	// deployment runs, and refuses to store a bind password rather than
	// storing one in the clear. See internal/secrets.
	var vault *secrets.Vault
	if len(cfg.EncryptionKey) > 0 {
		vault, err = secrets.NewVault(cfg.EncryptionKey)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
	}
	// The same key a bind password is sealed under. Attached here rather
	// than at construction because the vault is built above, after the
	// service that needs it.
	webhooks.WithVault(vault)
	// What a snapshot reads. Attached here rather than at construction
	// because the account, organization and group services are built above
	// it — the same arrangement as the vault and the field mappings.
	webhooks.WithSnapshotSource(service.NewSnapshotSource(users, orgs, groups))
	directories := service.NewDirectoryService(st, users, audit, webhooks, vault)
	scimHandler := scim.NewHandler(users, groups, scimCredentials, cfg.PublicURL)
	// Portico as the relying party: the providers a tenant sends people to.
	// Built here rather than beside the other services because it needs the
	// vault, which is built above.
	externalIDP := service.NewExternalIDPService(st, users, audit, vault, cfg.PublicURL)

	s := &Server{
		cfg:   cfg,
		store: st,
		handler: handler.New(users, orgs, audit, settings, tenants, recovery, verification, sessions,
			clients, serviceProviders, samlKeys, casServices, scimCredentials,
			directories, webhooks, externalIDP, groups, logos, attributes, fields, fieldMappings,
			providers, samlProviders, casServer, trials),
		middleware:    auth.NewMiddleware(tokens, users, sessions),
		metrics:       registry,
		scim:          scimHandler,
		webhooks:      webhooks,
		logos:         logos,
		directories:   directories,
		webhookClient: webhook.NewClient(webhook.RequestTimeout),
		users:         users,
		tenants:       tenants,
		settings:      settings,
		trials:        trials,
		oidc:          providers,
		saml:          samlProviders,
		cas:           casServer,
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

	created, mustChange, err := s.users.EnsureInitialAdmin(
		ctx, tenant.ID, s.cfg.InitialAdminUsername, s.cfg.InitialAdminPassword)
	if err != nil {
		return err
	}

	if created {
		slog.Info("created the initial administrator account",
			"username", s.cfg.InitialAdminUsername, "tenant", tenant.Code)
	}

	if created && mustChange {
		// On stderr rather than through the structured logger, as the
		// generated password this replaced was: those records are normally
		// shipped to an aggregator, and this notice is for whoever is
		// watching the terminal on first run.
		//
		// It says "now" and means it. The password below is in the manual
		// and identical on every installation, so the window between this
		// line and the first sign-in is the window in which anyone who can
		// reach the port can claim the account — and having claimed it,
		// they would set a password the operator does not know, on an
		// account with no address to recover through.
		fmt.Fprintf(os.Stderr, `
────────────────────────────────────────────────────────────────
  Initial administrator created

    tenant:    %s
    username:  %s
    password:  %s

  This is the documented default. Sign in NOW: the account is
  refused until the password is replaced, and the replacement is
  the first thing the sign-in screen will ask for.

  To choose the password yourself instead — and skip the forced
  change — set PORTICO_INITIAL_ADMIN_PASSWORD before first start.
────────────────────────────────────────────────────────────────

`, tenant.Code, s.cfg.InitialAdminUsername, service.DefaultInitialAdminPassword)
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
// Plus one that is not dead weight — a trial request nobody confirmed, which
// costs a tenant code rather than a page of disk. It is swept on the same
// schedule because that is where anybody would look for it, not because it
// has the same reason.
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
	// Trial requests nobody confirmed, which is the only one of these that
	// holds something a person can be refused.
	//
	// The others are dead weight: a row nobody will look at again, costing
	// disk. This one reserves a tenant code from the moment it is made and
	// keeps it whether or not the link is ever opened — so on a public
	// demonstration, where the first names typed are `demo`, `test` and the
	// visitor's own company, leaving them uncollected means those names are
	// refused forever against tenants that do not exist.
	//
	// It was missing here, and the comment above this function is why that
	// mattered: a sweep that covers some of what grows is one everybody
	// assumes covers the rest, so nothing else was ever going to collect them.
	if _, err := s.trials.SweepExpired(ctx); err != nil {
		return err
	}

	// And the tenants those requests grew into. Uncollected links hold a name;
	// uncollected tenants hold the quota itself, so leaving these is what
	// eventually refuses every new visitor — see TrialService.SweepTenants.
	if _, _, err := s.trials.SweepTenants(ctx); err != nil {
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
		if err := s.webhooks.SweepDeliveries(ctx, tenant.ID, now); err != nil {
			return fmt.Errorf("sweep webhook deliveries for tenant %s: %w", tenant.Code, err)
		}
		// Uploaded tile pictures nothing points at. A logo has to be stored
		// before the registration form naming it is saved, so cancelling that
		// form strands one, and replacing a logo strands the one it replaced.
		if _, err := s.logos.SweepOrphans(ctx, tenant.ID, now); err != nil {
			return fmt.Errorf("sweep orphaned logos for tenant %s: %w", tenant.Code, err)
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

// DispatchWebhooks delivers whatever is due, across every tenant.
//
// Separate from SweepExpired and on a much shorter timer: a sweep tidies up
// after things, while this is how an event reaches anybody at all. An hourly
// pass would mean a deprovisioning notice arriving up to an hour after the
// account was disabled, which is exactly the delay a webhook exists to
// remove.
//
// A tenant whose pass fails does not stop the others. One unreachable
// subscriber is that tenant's problem, and letting it abort the loop would
// make it everybody's.
func (s *Server) DispatchWebhooks(ctx context.Context) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	for _, tenant := range tenants {
		if _, err := s.webhooks.DispatchDue(ctx, tenant.ID, s.webhookClient); err != nil {
			slog.WarnContext(ctx, "webhook dispatch failed for tenant",
				"tenant", tenant.Code, "error", err)
		}
	}
	return nil
}

// SyncDirectories reads the directories whose interval has elapsed, across
// every tenant.
//
// In-process rather than something an operator schedules externally, and that
// is the whole point of it. The documented workaround before this existed was
// a cron job calling POST /api/v1/directories/{id}/sync, which needs an access
// token, which expires and is revoked by a password change — so the job had to
// sign in each time, which meant an administrator's password sitting in the
// cron environment. This path holds no credential at all.
//
// A tenant whose pass fails does not stop the others, for the same reason
// webhook delivery works that way: one unreachable directory is one tenant's
// problem until an aborted loop makes it everybody's.
func (s *Server) SyncDirectories(ctx context.Context) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	now := store.Now()
	for _, tenant := range tenants {
		runs, err := s.directories.SyncDue(ctx, tenant.ID, now)
		if err != nil {
			slog.WarnContext(ctx, "scheduled directory synchronization failed for tenant",
				"tenant", tenant.Code, "error", err)
			continue
		}
		for _, run := range runs {
			// Logged at info because it is unattended: nobody is watching a
			// screen for the result, and the run record is only consulted
			// once somebody already suspects something is wrong.
			slog.InfoContext(ctx, "directory synchronized on schedule",
				"tenant", tenant.Code, "source", run.SourceID, "outcome", run.Outcome,
				"created", run.CreatedCount, "updated", run.UpdatedCount,
				"deactivated", run.DeactivatedCount, "skipped", run.SkippedCount,
				"error", run.Error)
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
