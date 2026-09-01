// Package seed fills a development database with data that looks like a
// deployment somebody has been using for three months.
//
// It exists because an empty Portico cannot be looked at. Every screen has an
// empty-state message, every list paginates at twenty rows it does not have,
// and the questions the product is built to answer — when did this directory
// stop working, which delivery is stuck, who changed that — need a past to
// answer from. Registering three accounts by hand produces none of it.
//
// # Two ways in, deliberately
//
// Entities go through the service layer: accounts, organizations, groups,
// applications, credentials. That is not ceremony. It is what makes the seed
// data *possible* data — password hashing, bind-password sealing, uniqueness,
// the attribute rules, the organization cycle check. A seeder that wrote rows
// directly would eventually write a row the application cannot produce, and
// then a screen would be exercised against a state that never occurs.
//
// History is written directly: audit entries, sessions, webhook deliveries,
// synchronization runs. Those tables are append-only records of moments, and
// the moment is the point. The service layer stamps store.Now() on
// everything, which would put ninety days of history in the same second — so
// the timestamp has to be chosen rather than observed. Nothing is bypassed
// that has a rule attached; these rows carry foreign keys and nothing else.
//
// See history.go, which is where that distinction is made concrete.
//
// # Not in the release image
//
// This is reached only through cmd/seed, a binary of its own. The release
// image copies `portico` and nothing else (deploy/Dockerfile.release), so
// there is no build in which a `seed` subcommand could be run against
// somebody's production database by accident.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/secrets"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
)

// Seeder holds everything the seed needs. It opens the database directly and
// never builds an HTTP stack, on the same terms as internal/provision.
type Seeder struct {
	store *store.Store

	tenants     *service.TenantService
	settings    *service.SettingsService
	users       *service.UserService
	orgs        *service.OrganizationService
	groups      *service.GroupService
	clients     *service.OAuthClientService
	sps         *service.SAMLServiceProviderService
	cas         *service.CASService
	scim        *service.SCIMCredentialService
	webhooks    *service.WebhookService
	dirs        *service.DirectoryService
	attrs       *service.UserAttributeService
	mappings    *service.FieldMappingService
	audit       *service.AuditService
	invitations *service.InvitationService

	// password is Options.Password, resolved once at the start of Run so that
	// every account created below gets the same one without threading it
	// through each stage.
	password string

	// canSeal records whether this deployment has PORTICO_ENCRYPTION_KEY.
	// Without it the services refuse to store a bind password or a webhook
	// header, which is correct; the seed registers those things without the
	// credential rather than skipping them.
	canSeal bool
}

// canSealSecrets reports whether credentials the server later presents can be
// stored at all.
func (s *Seeder) canSealSecrets() bool { return s.canSeal }

// Options control what the seed produces.
type Options struct {
	// Now anchors the timeline. Everything is placed relative to it, so the
	// data stays meaningful whenever it is run — an account whose password
	// expires in three days has to expire three days from today, not three
	// days from whenever this was written.
	//
	// Passed in rather than read here so that a test can seed a fixed
	// timeline and assert against it.
	Now time.Time

	// Force seeds a database that already holds accounts. Off by default:
	// the usual mistake is pointing a development tool at the wrong DSN, and
	// the usual cost of that mistake is a real user list with fifty-five
	// invented colleagues in it.
	Force bool

	// Password is what every seeded account signs in with. Empty means
	// DemoPassword, which is published and belongs in a Codespace or on a
	// laptop.
	//
	// It exists for the one deployment where neither is true: a demonstration
	// on a public address. There, a published password means the first
	// visitor to read the README is an administrator, and by the second day
	// the demonstration is of whatever they left behind. A value passed in
	// here is known only to whoever deployed it, so the address can be public
	// while the way in is not.
	//
	// Not read from the environment on purpose. .env.example describes what
	// the *server* reads, and a test holds that correspondence in both
	// directions; a seed-only variable would have to be excluded from it by
	// hand, which is how a list stops being trustworthy. A flag is also
	// simply harder to leave set by accident.
	Password string

	// Log receives one line per stage. Nil discards them.
	Log *slog.Logger
}

// Open connects to the database named by cfg and applies any pending
// migrations, exactly as starting the server would. The caller must Close.
func Open(cfg *config.Config) (*Seeder, error) {
	st, err := store.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}
	return New(st, cfg), nil
}

// New builds a Seeder over an already-open store, which is what lets a test
// seed the database it already has rather than opening a second one.
func New(st *store.Store, cfg *config.Config) *Seeder {
	audit := service.NewAuditService(st)
	settings := service.NewSettingsService(st, cfg.TokenTTL)
	// A token service is required to construct the user service and is never
	// exercised: seeding issues no sessions through it. History writes the
	// session rows it wants directly, because it wants them dated.
	tokens := auth.NewTokenService(cfg.JWTSecret)
	// No metrics registry. This is a process that exits, and a counter
	// nothing will scrape is not worth the allocation; the service tolerates
	// nil for exactly this case.
	users := service.NewUserService(st, audit, settings, tokens, nil)
	fields := service.NewFieldCatalogue(st)
	mappings := service.NewFieldMappingService(st, audit, fields)
	webhooks := service.NewWebhookService(st, audit).WithFieldMappings(fields, mappings)

	// A vault only when a key is configured. Without one the directory
	// service refuses to store a bind password, which is correct behaviour
	// and would leave the seed with a directory it cannot describe — so the
	// bind password is skipped rather than the directory, and the seed says
	// so. See integrations.go.
	var vault *secrets.Vault
	if len(cfg.EncryptionKey) > 0 {
		if v, err := secrets.NewVault(cfg.EncryptionKey); err == nil {
			vault = v
			webhooks = webhooks.WithVault(v)
		}
	}

	return &Seeder{
		store:       st,
		tenants:     service.NewTenantService(st),
		settings:    settings,
		users:       users,
		orgs:        service.NewOrganizationService(st, audit),
		groups:      service.NewGroupService(st, audit),
		clients:     service.NewOAuthClientService(st, audit),
		sps:         service.NewSAMLServiceProviderService(st, audit),
		cas:         service.NewCASService(st, users, audit),
		scim:        service.NewSCIMCredentialService(st, audit),
		webhooks:    webhooks,
		dirs:        service.NewDirectoryService(st, users, audit, webhooks, vault),
		attrs:       service.NewUserAttributeService(st, audit),
		mappings:    mappings,
		audit:       audit,
		invitations: service.NewInvitationService(st, audit),
		canSeal:     vault != nil,
	}
}

// Close releases the database handle.
func (s *Seeder) Close() error { return s.store.Close() }

// Summary is what a run produced, for the caller to print and for a test to
// assert against.
type Summary struct {
	Tenants       int
	Users         int
	Organizations int
	Groups        int
	Applications  int
	Subscriptions int
	// FieldMappings is rules, not recipients: the interesting number is how
	// much has been decided, not how many things could have decided something.
	FieldMappings int
	Directories   int
	// IdentityProviders is providers configured, and ExternalIdentities is
	// accounts linked to one. Both, because the first is what an
	// administrator set up and the second is what people did with it, and a
	// seed that reported only the first would look identical whether or not
	// anybody had ever used the button.
	IdentityProviders  int
	ExternalIdentities int
	AuditEntries       int
	Sessions           int
	Deliveries         int
	SyncRuns           int
	Invitations        int
}

// ErrNotEmpty is returned when the database already holds accounts and Force
// was not set.
var ErrNotEmpty = fmt.Errorf("this database already holds accounts; " +
	"re-run with --force if you are certain it is a development database")

// Run seeds the database.
//
// Staged, and each stage depends only on the ones before it: tenants, then
// what belongs to a tenant, then the history that refers to all of it. A
// failure stops the run rather than continuing — half-seeded data is worse
// than none, because it looks like data.
func (s *Seeder) Run(ctx context.Context, opts Options) (Summary, error) {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if opts.Now.IsZero() {
		opts.Now = store.Now()
	}
	if opts.Password == "" {
		opts.Password = DemoPassword
	}
	s.password = opts.Password

	if err := s.checkEmpty(ctx, opts.Force); err != nil {
		return Summary{}, err
	}

	var summary Summary
	w := &world{opts: opts, summary: &summary}

	for _, stage := range []struct {
		name string
		run  func(context.Context, *world) error
	}{
		{"tenants and settings", s.seedTenants},
		{"organizations", s.seedOrganizations},
		{"groups", s.seedGroups},
		{"invitations", s.seedInvitations},
		{"accounts", s.seedUsers},
		{"applications", s.seedApplications},
		{"integrations", s.seedIntegrations},
		{"federation", s.seedFederation},
		{"history", s.seedHistory},
	} {
		if err := stage.run(ctx, w); err != nil {
			return summary, fmt.Errorf("seed %s: %w", stage.name, err)
		}
		log.Info("seeded", "stage", stage.name)
	}

	return summary, nil
}

// checkEmpty refuses a database that somebody is using.
//
// "Empty" means no accounts beyond the bootstrap administrator, which is what
// a freshly started server leaves behind. Counting accounts rather than
// checking for the seed's own marker is deliberate: the question is not "has
// this been seeded" but "is anybody relying on what is in here".
func (s *Seeder) checkEmpty(ctx context.Context, force bool) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	for _, tenant := range tenants {
		_, total, err := s.users.List(ctx, tenant.ID, service.UserQuery{}, service.Page{Limit: 1})
		if err != nil {
			return fmt.Errorf("count accounts in tenant %s: %w", tenant.Code, err)
		}
		// One is the bootstrap administrator. More than that is somebody's
		// data.
		if total > 1 && !force {
			return ErrNotEmpty
		}
	}
	return nil
}

// world is what one stage hands to the next: the identifiers created so far.
// Stages are separate functions for readability, not isolation — an account
// has to land in an organization that another stage created.
type world struct {
	opts    Options
	summary *Summary

	tenants []seededTenant
}

// seededTenant is one tenant and everything created inside it.
type seededTenant struct {
	tenant model.Tenant
	// actor is the administrator whose name the audit trail will carry for
	// everything this seed does inside the tenant. Real, so that clicking
	// through to it from an audit entry lands somewhere.
	actor auth.Principal

	orgs   map[string]model.Organization
	groups []model.Group
	users  []model.User

	clients []model.OAuthClient
	sps     []model.SAMLServiceProvider
	casSvcs []model.CASService

	directories   []model.LDAPSource
	subscriptions []service.Subscription
}

// tenantByCode finds a tenant a later stage needs to add to.
func (w *world) tenantByCode(code string) *seededTenant {
	for i := range w.tenants {
		if w.tenants[i].tenant.Code == code {
			return &w.tenants[i]
		}
	}
	return nil
}
