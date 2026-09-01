// Command seed fills a development database with data that looks used.
//
// A binary of its own rather than a `portico seed` subcommand, and that is the
// point rather than an accident of layout: the release image copies `portico`
// and nothing else, so there is no build of the product in which this can be
// pointed at somebody's production database.
//
//	docker compose -f deploy/dev-stack/compose.yml up -d   # optional, for LDAP
//	PORTICO_DB_DSN=postgres://portico:portico@localhost:5432/portico?sslmode=disable \
//	  go run ./cmd/seed
//
// It refuses a database that already holds accounts. --force says you know
// which database this is.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/seed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	force := flag.Bool("force", false,
		"seed a database that already holds accounts (you are certain it is a development one)")
	quiet := flag.Bool("quiet", false, "only report the summary")
	// For a demonstration on a public address. Unset means the published
	// DemoPassword, which is right for a Codespace and wrong the moment the
	// address can be reached by somebody who did not deploy it — see
	// seed.Options.Password.
	password := flag.String("password", "",
		"the password every seeded account signs in with (default: the published demo password)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if *quiet {
		level = slog.LevelWarn
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	seeder, err := seed.Open(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = seeder.Close() }()

	summary, err := seeder.Run(context.Background(), seed.Options{
		Force: *force, Password: *password, Log: log,
	})
	if err != nil {
		if errors.Is(err, seed.ErrNotEmpty) {
			return err
		}
		return fmt.Errorf("seeding stopped: %w", err)
	}

	signInWith := *password
	if signInWith == "" {
		signInWith = seed.DemoPassword
	}
	report(summary, signInWith)
	return nil
}

// report prints what was created and how to sign in. The password is printed
// because a seed nobody can sign in to is a database, not a demonstration.
func report(s seed.Summary, password string) {
	fmt.Printf(`Seeded.

  tenants        %d
  accounts       %d
  organizations  %d
  groups         %d
  invitations    %d
  applications   %d
  directories    %d
  subscriptions  %d
  field mappings %d
  idp providers  %d
  linked identities %d
  audit entries  %d
  sessions       %d
  deliveries     %d
  sync runs      %d

Sign in as any seeded account with the password %q — %q in the default
tenant, or the same name in tenant %q, which is a different person entirely
and shows how little carries across.
`,
		s.Tenants, s.Users, s.Organizations, s.Groups, s.Invitations, s.Applications,
		s.Directories, s.Subscriptions, s.FieldMappings,
		s.IdentityProviders, s.ExternalIdentities, s.AuditEntries, s.Sessions,
		s.Deliveries, s.SyncRuns,
		password, "admin", seed.TenantSecond)
}
