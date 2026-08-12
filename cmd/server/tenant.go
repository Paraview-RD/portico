package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/provision"
)

// runTenant dispatches `portico tenant <subcommand>`.
//
// Tenant provisioning is a command-line operation because no account can act
// outside its own tenant, so there is no one the API could authorize to do
// it. See internal/provision.
func runTenant(args []string) error {
	if len(args) == 0 {
		tenantUsage()
		return fmt.Errorf("tenant: a subcommand is required")
	}

	switch args[0] {
	case "create":
		return runTenantCreate(args[1:])
	case "list":
		return runTenantList()
	case "enable":
		return runTenantStatus(args[1:], model.StatusActive)
	case "disable":
		return runTenantStatus(args[1:], model.StatusDisabled)
	case "--help", "-h", "help":
		tenantUsage()
		return nil
	default:
		tenantUsage()
		return fmt.Errorf("tenant: unknown subcommand %q", args[0])
	}
}

func tenantUsage() {
	fmt.Print(`portico tenant — provision tenants

Usage:
  portico tenant create --code <code> [--name <name>]
                        [--admin-username <name>] [--admin-password <password>]
  portico tenant list
  portico tenant enable  --code <code>
  portico tenant disable --code <code>

Every tenant gets its own administrator when it is created; there is no
account that can administer more than one. When --admin-password is omitted
a password is generated and printed once.

Disabling a tenant refuses sign-in without deleting anything, and can be
undone with enable.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

func runTenantCreate(args []string) error {
	fs := flag.NewFlagSet("tenant create", flag.ContinueOnError)
	code := fs.String("code", "", "tenant code, used at sign-in (required)")
	name := fs.String("name", "", "display name (defaults to the code)")
	adminUsername := fs.String("admin-username", "admin", "the tenant's first administrator")
	adminPassword := fs.String("admin-password", "", "their password (generated when omitted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("tenant create: --code is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	created, err := p.CreateTenant(context.Background(), *code, *name, *adminUsername, *adminPassword)
	if err != nil {
		return err
	}

	fmt.Printf("Created tenant %q (%s).\n", created.Tenant.Code, created.Tenant.Name)
	fmt.Printf("Sign in with the tenant code %q and username %q.\n",
		created.Tenant.Code, created.AdminUsername)

	if created.AdminPassword != "" {
		// To stderr, and not through the structured logger: log records are
		// usually shipped to an aggregator, where a credential would persist,
		// be searchable, and be readable by far more people than should have
		// it. The same reasoning as the server's first-run banner.
		fmt.Fprintf(os.Stderr, `
────────────────────────────────────────────────────────────────
  Administrator password for tenant %s

    username:  %s
    password:  %s

  This is the documented default, the same on every installation.
  Sign in now: the account is refused until it is replaced, and
  the sign-in screen asks for the replacement straight away.
────────────────────────────────────────────────────────────────

`, created.Tenant.Code, created.AdminUsername, created.AdminPassword)
	}
	return nil
}

func runTenantList() error {
	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	tenants, err := p.ListTenants(context.Background())
	if err != nil {
		return err
	}
	if len(tenants) == 0 {
		fmt.Println("No tenants yet. Starting the server creates the default one.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Writes to a tabwriter are buffered; Flush below reports any failure.
	_, _ = fmt.Fprintln(w, "CODE\tNAME\tSTATUS\tCREATED")
	for _, t := range tenants {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			t.Code, t.Name, t.Status, t.CreatedAt.Format("2006-01-02"))
	}
	return w.Flush()
}

func runTenantStatus(args []string, status model.Status) error {
	fs := flag.NewFlagSet("tenant status", flag.ContinueOnError)
	code := fs.String("code", "", "tenant code (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("--code is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	tenant, err := p.SetTenantStatus(context.Background(), *code, status)
	if err != nil {
		return err
	}

	fmt.Printf("Tenant %q is now %s.\n", tenant.Code, tenant.Status)
	return nil
}

func openProvisioner() (*provision.Provisioner, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return provision.Open(cfg)
}
