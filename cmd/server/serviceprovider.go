package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// runSP dispatches `portico sp <subcommand>`.
//
// The command-line equivalent of the console's SAML tab, on the same terms
// as `portico client`.
func runSP(args []string) error {
	if len(args) == 0 {
		spUsage()
		return fmt.Errorf("sp: a subcommand is required")
	}

	switch args[0] {
	case "register":
		return runSPRegister(args[1:])
	case "list":
		return runSPList(args[1:])
	case "enable":
		return runSPStatus(args[1:], model.StatusActive)
	case "disable":
		return runSPStatus(args[1:], model.StatusDisabled)
	case "certificate":
		return runSPCertificate(args[1:], false)
	case "rotate-certificate":
		return runSPCertificate(args[1:], true)
	case "--help", "-h", "help":
		spUsage()
		return nil
	default:
		spUsage()
		return fmt.Errorf("sp: unknown subcommand %q", args[0])
	}
}

func spUsage() {
	fmt.Print(`portico sp — register the SAML service providers that sign in through Portico

Usage:
  portico sp register           --metadata <file|url> [--tenant <code>] [--name <name>]
                                [--launch-url <url>] [--logo-uri <url|path>]
  portico sp list               [--tenant <code>]
  portico sp enable             --entity-id <id> [--tenant <code>]
  portico sp disable            --entity-id <id> [--tenant <code>]
  portico sp certificate        [--tenant <code>]
  portico sp rotate-certificate [--tenant <code>]

--tenant defaults to the default tenant, which is all a single-tenant
deployment ever needs.

Registration takes the service provider's own metadata document, whole,
rather than a list of fields to retype. Everything the protocol needs is in
there — the entity id, the assertion consumer service endpoints, the NameID
formats it accepts — published by the service provider itself.

Portico's own metadata is served at {PORTICO_PUBLIC_URL}/saml/metadata for
the default tenant and /t/<code>/saml/metadata for any other. Hand that to
the service provider; it is the other half of the exchange.

rotate-certificate generates a new signing certificate and retires the old
one. Nothing is deleted and nothing happens automatically: every service
provider has to be reconfigured by hand, and until each one has been, the
previous certificate is what you need to be able to look up.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

func runSPRegister(args []string) error {
	fs := flag.NewFlagSet("sp register", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	metadata := fs.String("metadata", "", "path or https:// URL of the service provider's metadata (required)")
	name := fs.String("name", "", "display name (defaults to the entity id)")
	// The two the portal needs. Without them an application registered from
	// here signs people in and then never appears on the home screen, which
	// looks like the portal being broken rather than like a field nobody
	// filled in.
	launchURL := fs.String("launch-url", "", "where a person opens it, for the home screen")
	logoURI := fs.String("logo-uri", "", "its icon: an https URL, or a path on this server such as /icons/wiki.svg")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *metadata == "" {
		return fmt.Errorf("sp register: --metadata is required")
	}

	document, err := readMetadata(*metadata)
	if err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	provider, err := p.RegisterServiceProvider(context.Background(), *tenant, service.RegisterSPInput{
		MetadataXML: document,
		Name:        *name,
		LaunchURL:   *launchURL,
		LogoURI:     *logoURI,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Registered %q (%s) in tenant %q.\n",
		provider.EntityID, provider.Name, tenantLabel(*tenant))
	fmt.Println("Assertion consumer services:")
	for _, acs := range provider.ACSURLs {
		fmt.Printf("  %s\n", acs)
	}
	return nil
}

// readMetadata loads a metadata document from a file or over https.
//
// Plain http is refused. The document carries the certificate Portico will
// check the service provider's own signatures against and the address
// assertions are delivered to; fetching it over a channel anybody on the
// path can rewrite would make the registration meaningless.
func readMetadata(source string) (string, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		// The path comes from an operator's own command line, which is the
		// same trust level as PORTICO_DB_DSN — anybody who can pass it can
		// already read the database.
		content, err := os.ReadFile(source) //nolint:gosec // operator-supplied path
		if err != nil {
			return "", fmt.Errorf("read metadata: %w", err)
		}
		return string(content), nil
	}

	if strings.HasPrefix(source, "http://") {
		return "", fmt.Errorf("sp register: refusing to fetch metadata over plain http — " +
			"the document names where assertions are delivered, so anybody on the path could redirect them. " +
			"Download it another way and pass the file")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(source)
	if err != nil {
		return "", fmt.Errorf("fetch metadata: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch metadata: %s returned %s", source, res.Status)
	}
	// A metadata document is kilobytes. The cap is here so that a URL
	// pointing at something enormous fails rather than filling memory.
	content, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read metadata: %w", err)
	}
	return string(content), nil
}

func runSPList(args []string) error {
	fs := flag.NewFlagSet("sp list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	providers, err := p.ListServiceProviders(context.Background(), *tenant)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		fmt.Printf("No service providers registered in tenant %q.\n", tenantLabel(*tenant))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ENTITY ID\tNAME\tSTATUS\tASSERTION CONSUMER SERVICES")
	for _, sp := range providers {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			sp.EntityID, sp.Name, sp.Status, strings.Join(sp.ACSURLs, " "))
	}
	return w.Flush()
}

func runSPStatus(args []string, status model.Status) error {
	fs := flag.NewFlagSet("sp status", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	entityID := fs.String("entity-id", "", "the service provider's entity id (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *entityID == "" {
		return fmt.Errorf("sp %s: --entity-id is required", strings.ToLower(string(status)))
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	provider, err := p.SetServiceProviderStatus(context.Background(), *tenant, *entityID, status)
	if err != nil {
		return err
	}

	verb := "Enabled"
	if status == model.StatusDisabled {
		verb = "Disabled"
	}
	fmt.Printf("%s %q in tenant %q. Nothing was deleted.\n",
		verb, provider.EntityID, tenantLabel(*tenant))
	return nil
}

func runSPCertificate(args []string, rotate bool) error {
	fs := flag.NewFlagSet("sp certificate", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	var key service.SAMLKey
	if rotate {
		key, err = p.RotateSAMLCertificate(context.Background(), *tenant)
	} else {
		key, err = p.SAMLCertificate(context.Background(), *tenant)
	}
	if err != nil {
		return err
	}

	if rotate {
		fmt.Fprintf(os.Stderr,
			"Rotated. Every service provider in tenant %q must be reconfigured with the\n"+
				"certificate below before it will accept another assertion. The previous one is\n"+
				"still listed as retired; nothing was deleted.\n\n", tenantLabel(*tenant))
	}
	fmt.Printf("# Tenant %s — SAML signing certificate, valid until %s\n",
		tenantLabel(*tenant), key.ExpiresAt.Format("2006-01-02"))
	fmt.Print(key.CertificatePEM)
	return nil
}
