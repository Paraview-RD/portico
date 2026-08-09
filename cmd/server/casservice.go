package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// runCAS dispatches `portico cas <subcommand>`.
func runCAS(args []string) error {
	if len(args) == 0 {
		casUsage()
		return fmt.Errorf("cas: a subcommand is required")
	}

	switch args[0] {
	case "register":
		return runCASRegister(args[1:])
	case "list":
		return runCASList(args[1:])
	case "enable":
		return runCASStatus(args[1:], model.StatusActive)
	case "disable":
		return runCASStatus(args[1:], model.StatusDisabled)
	case "--help", "-h", "help":
		casUsage()
		return nil
	default:
		casUsage()
		return fmt.Errorf("cas: unknown subcommand %q", args[0])
	}
}

func casUsage() {
	fmt.Print(`portico cas — register the CAS services that sign in through Portico

Usage:
  portico cas register --url <prefix> [--tenant <code>] [--name <name>]
                       [--launch-url <url>] [--logo-uri <url|path>]
  portico cas list     [--tenant <code>]
  portico cas enable   --url <prefix> [--tenant <code>]
  portico cas disable  --url <prefix> [--tenant <code>]

--tenant defaults to the default tenant, which is all a single-tenant
deployment ever needs.

--url is a URL prefix, not a pattern. A service parameter matches when it
begins with the registered value; there are no wildcards, and a registration
always covers a path boundary, so https://app.example.com/ can never match
https://app.example.com.somewhere-else.test.

Register the prefix rather than the exact URL because CAS clients append
their own return-to parameters:

  portico cas register --url https://wiki.example.com/ --name Wiki

The CAS endpoints are at {PORTICO_PUBLIC_URL}/cas/... for the default tenant
and /t/<code>/cas/... for any other. Point the client's "CAS server URL" at
the part before /login.

These commands need PORTICO_DB_DSN, the same as starting the server.
`)
}

func runCASRegister(args []string) error {
	fs := flag.NewFlagSet("cas register", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	prefix := fs.String("url", "", "service URL prefix (required)")
	name := fs.String("name", "", "display name (defaults to the prefix)")
	// The two the portal needs. Without them an application registered from
	// here signs people in and then never appears on the home screen, which
	// looks like the portal being broken rather than like a field nobody
	// filled in.
	launchURL := fs.String("launch-url", "", "where a person opens it, for the home screen")
	logoURI := fs.String("logo-uri", "", "its icon: an https URL, or a path on this server such as /icons/wiki.svg")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prefix == "" {
		return fmt.Errorf("cas register: --url is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	registered, err := p.RegisterCASService(context.Background(), *tenant, service.RegisterCASInput{
		URLPrefix: *prefix,
		Name:      *name,
		LaunchURL: *launchURL,
		LogoURI:   *logoURI,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Registered %q (%s) in tenant %q.\n",
		registered.URLPrefix, registered.Name, tenantLabel(*tenant))
	if registered.URLPrefix != *prefix {
		fmt.Printf("Normalized from %q; a prefix always ends at a path boundary.\n", *prefix)
	}
	return nil
}

func runCASList(args []string) error {
	fs := flag.NewFlagSet("cas list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	services, err := p.ListCASServices(context.Background(), *tenant)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		fmt.Printf("No CAS services registered in tenant %q.\n", tenantLabel(*tenant))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "URL PREFIX\tNAME\tSTATUS")
	for _, svc := range services {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", svc.URLPrefix, svc.Name, svc.Status)
	}
	return w.Flush()
}

func runCASStatus(args []string, status model.Status) error {
	fs := flag.NewFlagSet("cas status", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant code (defaults to the default tenant)")
	prefix := fs.String("url", "", "the registered URL prefix (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prefix == "" {
		return fmt.Errorf("cas: --url is required")
	}

	p, err := openProvisioner()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	svc, err := p.SetCASServiceStatus(context.Background(), *tenant, *prefix, status)
	if err != nil {
		return err
	}

	verb := "Enabled"
	if status == model.StatusDisabled {
		verb = "Disabled"
	}
	fmt.Printf("%s %q in tenant %q. Nothing was deleted.\n",
		verb, svc.URLPrefix, tenantLabel(*tenant))
	return nil
}
