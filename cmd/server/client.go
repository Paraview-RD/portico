package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// runClient dispatches `portico client <subcommand>`.
//
// Relying parties are registered from the command line for the same reason
// tenants are: deciding who may ask this server for tokens about its users
// is an administrative act, and no account can act outside its own tenant,
// so there is nobody the API could authorize to do it.
func runClient(args []string) error {
	if len(args) == 0 {
		clientUsage()
		return fmt.Errorf("client: a subcommand is required")
	}

	switch args[0] {
	case "register":
		return runClientRegister(args[1:])
	case "list":
		return runClientList(args[1:])
	case "enable":
		return runClientStatus(args[1:], model.StatusActive)
	case "disable":
		return runClientStatus(args[1:], model.StatusDisabled)
	case "rotate-key":
		return runRotateKey(args[1:])
	case "--help", "-h", "help":
		clientUsage()
		return nil
	default:
		clientUsage()
		return fmt.Errorf("client: unknown subcommand %q", args[0])
	}
}

func clientUsage() {
	fmt.Print(`portico client — register the applications that sign in through Portico

Usage:
  portico client register --id <client-id> --redirect-uri <uri> [--redirect-uri <uri>]
                          [--tenant <code>] [--name <name>] [--public]
                          [--type WEB|NATIVE|USER_AGENT]
                          [--post-logout-redirect-uri <uri>] [--scope <scope>]
  portico client list        [--tenant <code>]
  portico client enable      --id <client-id> [--tenant <code>]
  portico client disable     --id <client-id> [--tenant <code>]
  portico client rotate-key  [--tenant <code>]

--tenant defaults to the default tenant, which is all a single-tenant
deployment ever needs.

A confidential client is given a secret, printed once. Use --public for a
browser or mobile application: it genuinely cannot keep a secret, and it
authenticates with PKCE instead — which OAuth 2.1 requires of every client
anyway.

Redirect URIs are matched exactly. Register each one; there are no
wildcards, because loose matching is how an authorization code ends up
delivered to somebody else's endpoint.

rotate-key replaces the tenant's signing key. The old key stays in the
published key set until the tokens it signed have expired, so rotating does
not sign anyone out.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

// repeatable collects a flag that may be given more than once.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ",") }

func (r *repeatable) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func runClientRegister(args []string) error {
	fs := flag.NewFlagSet("client register", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	clientID := fs.String("id", "", "the client_id the application will send (required)")
	name := fs.String("name", "", "display name (defaults to the id)")
	public := fs.Bool("public", false, "the application cannot keep a secret")
	appType := fs.String("type", "WEB", "WEB | NATIVE | USER_AGENT")

	var redirects, postLogout, scopes repeatable
	fs.Var(&redirects, "redirect-uri", "where authorization codes are delivered (repeatable, required)")
	fs.Var(&postLogout, "post-logout-redirect-uri", "where to return after sign-out (repeatable)")
	fs.Var(&scopes, "scope", "an allowed scope (repeatable; defaults to openid profile email)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *clientID == "" {
		return fmt.Errorf("client register: --id is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	registered, err := p.RegisterClient(context.Background(), *tenant, service.RegisterClientInput{
		ClientID:               *clientID,
		Name:                   *name,
		Public:                 *public,
		ApplicationType:        *appType,
		RedirectURIs:           redirects,
		PostLogoutRedirectURIs: postLogout,
		Scopes:                 scopes,
	})
	if err != nil {
		return err
	}

	client := registered.Client
	fmt.Printf("Registered %q (%s) in tenant %q.\n", client.ClientID, client.Name, tenantLabel(*tenant))
	fmt.Printf("Scopes: %s\n", strings.Join(client.Scopes, " "))
	fmt.Printf("Redirect URIs:\n")
	for _, uri := range client.RedirectURIs {
		fmt.Printf("  %s\n", uri)
	}

	if registered.Secret == "" {
		fmt.Println("\nPublic client: no secret. It must use PKCE, which is required regardless.")
		return nil
	}

	// To stderr and not through the structured logger, for the same reason
	// as the bootstrap administrator password: log records are usually
	// shipped somewhere a credential would persist and be searchable.
	fmt.Fprintf(os.Stderr, `
────────────────────────────────────────────────────────────────
  Client secret for %s

    client_id:      %s
    client_secret:  %s

  Shown once and stored nowhere — what is kept is a hash.
────────────────────────────────────────────────────────────────

`, client.Name, client.ClientID, registered.Secret)
	return nil
}

func runClientList(args []string) error {
	fs := flag.NewFlagSet("client list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	clients, err := p.ListClients(context.Background(), *tenant)
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		fmt.Printf("No clients registered in tenant %q.\n", tenantLabel(*tenant))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Writes to a tabwriter are buffered; Flush below reports any failure.
	_, _ = fmt.Fprintln(w, "CLIENT ID\tNAME\tKIND\tSTATUS\tREDIRECT URIS")
	for _, c := range clients {
		kind := "public"
		if c.Confidential {
			kind = "confidential"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			c.ClientID, c.Name, kind, c.Status, strings.Join(c.RedirectURIs, " "))
	}
	return w.Flush()
}

func runClientStatus(args []string, status model.Status) error {
	fs := flag.NewFlagSet("client status", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	clientID := fs.String("id", "", "client id (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *clientID == "" {
		return fmt.Errorf("--id is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	client, err := p.SetClientStatus(context.Background(), *tenant, *clientID, status)
	if err != nil {
		return err
	}
	fmt.Printf("Client %q is now %s.\n", client.ClientID, client.Status)
	return nil
}

func runRotateKey(args []string) error {
	fs := flag.NewFlagSet("client rotate-key", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	kid, err := p.RotateSigningKey(context.Background(), *tenant)
	if err != nil {
		return err
	}
	fmt.Printf("Tenant %q now signs with key %s.\n", tenantLabel(*tenant), kid)
	fmt.Println("The previous key stays in the published key set until its tokens expire.")
	return nil
}

func tenantLabel(code string) string {
	if code == "" {
		return model.DefaultTenantCode
	}
	return code
}
