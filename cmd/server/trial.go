package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/Paraview-RD/portico/internal/provision"
)

// runTrial dispatches `portico trial <subcommand>`.
//
// Looking after the tenants a public demonstration handed out. On the command
// line for the same reason tenant provisioning is: no account can act outside
// its own tenant, so there is nobody the API could authorize to list or delete
// one. See internal/provision.
func runTrial(args []string) error {
	if len(args) == 0 {
		trialUsage()
		return fmt.Errorf("trial: a subcommand is required")
	}

	switch args[0] {
	case "list":
		return runTrialList()
	case "delete":
		return runTrialDelete(args[1:])
	case "prune":
		return runTrialPrune()
	case "--help", "-h", "help":
		trialUsage()
		return nil
	default:
		trialUsage()
		return fmt.Errorf("trial: unknown subcommand %q", args[0])
	}
}

func trialUsage() {
	fmt.Print(`portico trial — look after tenants self-service trials created

Usage:
  portico trial list
  portico trial delete --code <code> --yes
  portico trial prune

list    every tenant a confirmed trial produced, with the address that
        asked for it. Tenants provisioned by hand are not shown.

delete  removes a trial tenant and everything in it: accounts,
        organizations, applications, the audit trail, all of it. This
        cannot be undone, which is why --yes is required and why it
        refuses any tenant no trial created — the default tenant and
        anything you provisioned yourself are out of reach.

prune   deletes trial requests whose links expired without being
        confirmed, releasing the tenant codes they were holding. A
        running server already does this every hour; this is for when
        there is not one, or when you want the names back now.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

func runTrialList() error {
	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	trials, err := p.ListTrials(context.Background())
	if err != nil {
		return err
	}
	if len(trials) == 0 {
		fmt.Println("No trial tenants. Either nobody has asked, or this deployment does not offer them.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CODE\tNAME\tSTATUS\tINDUSTRY\tREQUESTED BY\tCONFIRMED")
	for _, t := range trials {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.TenantCode, t.TenantName, t.Status, t.Industry, t.Email,
			t.ConfirmedAt.Format("2006-01-02 15:04"))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Printf("\n%d trial tenant(s).\n", len(trials))
	return nil
}

func runTrialDelete(args []string) error {
	fs := flag.NewFlagSet("trial delete", flag.ContinueOnError)
	code := fs.String("code", "", "tenant code (required)")
	// Not a confirmation prompt. This is meant to be runnable from a script
	// and over ssh, where a prompt is either answered by accident or hangs
	// forever; a flag somebody had to type is the same protection without
	// either failure.
	yes := fs.Bool("yes", false, "confirm that everything in the tenant is to be deleted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("trial delete: --code is required")
	}
	if !*yes {
		return fmt.Errorf(
			"trial delete: this deletes tenant %q and everything in it, and cannot be "+
				"undone. Re-run with --yes if that is what you want", *code)
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	deleted, err := p.DeleteTrialTenant(context.Background(), *code)
	if errors.Is(err, provision.ErrNotATrialTenant) {
		// Said in full rather than as "not found", because the two are
		// different situations and only one of them is a typo.
		return fmt.Errorf("trial delete: %w. `portico trial list` shows the ones "+
			"this command can remove", err)
	} else if err != nil {
		return err
	}

	fmt.Printf("Deleted trial tenant %q (%d rows), requested by %s.\n",
		deleted.Code, deleted.Rows, deleted.Email)
	fmt.Println("That address and that tenant code are both free again.")
	return nil
}

func runTrialPrune() error {
	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	removed, err := p.PruneRequests(context.Background())
	if err != nil {
		return err
	}
	if removed == 0 {
		fmt.Println("Nothing to prune: no expired request is holding a tenant code.")
		return nil
	}
	fmt.Printf("Released %d tenant code(s) held by links nobody opened.\n", removed)
	return nil
}
