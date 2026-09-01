package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
)

// runInvitation dispatches `portico invitation <subcommand>`.
//
// The console can do all of this too; this is the command-line equivalent,
// for a first deployment before anybody has signed in, for scripting, and
// for when the console cannot be reached.
func runInvitation(args []string) error {
	if len(args) == 0 {
		invitationUsage()
		return fmt.Errorf("invitation: a subcommand is required")
	}

	switch args[0] {
	case "create":
		return runInvitationCreate(args[1:])
	case "list":
		return runInvitationList(args[1:])
	case "disable":
		return runInvitationDisable(args[1:])
	case "--help", "-h", "help":
		invitationUsage()
		return nil
	default:
		invitationUsage()
		return fmt.Errorf("invitation: unknown subcommand %q", args[0])
	}
}

func invitationUsage() {
	fmt.Print(`portico invitation — issue codes that gate self-registration

Usage:
  portico invitation create  --code <code> --quota <n> [--tenant <code>]
                              [--organization-id <id>] [--group-id <id>]
                              [--expires-in <duration>]
  portico invitation list    [--tenant <code>]
  portico invitation disable --id <invitation-id> [--tenant <code>]

--tenant defaults to the default tenant, which is all a single-tenant
deployment ever needs.

Disabling is terminal: there is no command that returns a code to ACTIVE.
Issue a new one instead.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

func runInvitationCreate(args []string) error {
	fs := flag.NewFlagSet("invitation create", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	code := fs.String("code", "", "the code somebody types at registration (required)")
	quota := fs.Int("quota", 0, "maximum number of successful registrations (required, at least 1)")
	orgID := fs.String("organization-id", "", "organization to assign on redemption (optional)")

	var groupIDs repeatable
	fs.Var(&groupIDs, "group-id", "group to assign on redemption (repeatable, optional)")

	expiresIn := fs.String("expires-in", "", "how long the code stays valid, e.g. 720h (optional; empty never expires)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("invitation create: --code is required")
	}

	var expiresAt *time.Time
	if *expiresIn != "" {
		d, err := time.ParseDuration(*expiresIn)
		if err != nil {
			return fmt.Errorf("invitation create: --expires-in: %w", err)
		}
		t := store.Now().Add(d)
		expiresAt = &t
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	invitation, err := p.CreateInvitation(context.Background(), *tenant, service.CreateInvitationInput{
		Code:           *code,
		OrganizationID: *orgID,
		GroupIDs:       groupIDs,
		Quota:          *quota,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Issued %q in tenant %q, quota %d.\n",
		invitation.Code, tenantLabel(*tenant), invitation.Quota)
	if expiresAt != nil {
		fmt.Printf("Expires: %s\n", expiresAt.Format(time.RFC3339))
	}
	return nil
}

func runInvitationList(args []string) error {
	fs := flag.NewFlagSet("invitation list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	invitations, err := p.ListInvitations(context.Background(), *tenant)
	if err != nil {
		return err
	}
	if len(invitations) == 0 {
		fmt.Printf("No invitations issued in tenant %q.\n", tenantLabel(*tenant))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Writes to a tabwriter are buffered; Flush below reports any failure.
	_, _ = fmt.Fprintln(w, "ID\tCODE\tQUOTA\tUSED\tSTATUS\tEXPIRES")
	for _, inv := range invitations {
		expires := "never"
		if inv.ExpiresAt != nil {
			expires = inv.ExpiresAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			inv.ID, inv.Code, strconv.Itoa(inv.Quota), strconv.Itoa(inv.UsedCount), inv.Status, expires)
	}
	return w.Flush()
}

func runInvitationDisable(args []string) error {
	fs := flag.NewFlagSet("invitation disable", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	id := fs.String("id", "", "invitation id (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("invitation disable: --id is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	invitation, err := p.DisableInvitation(context.Background(), *tenant, *id)
	if err != nil {
		return err
	}
	fmt.Printf("Invitation %q is now %s.\n", invitation.Code, invitation.Status)
	return nil
}
