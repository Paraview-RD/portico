// Command server runs the Portico API and serves the embedded web UI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/server"
)

func main() {
	// A downloaded binary is the primary distribution, so --version and
	// --help have to do the obvious thing. Without them, someone checking
	// what they just downloaded accidentally starts a server that creates a
	// database in the current directory.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("portico", server.Version)
			return
		case "--help", "-h", "help":
			usage()
			return
		case "tenant":
			if err := runTenant(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "portico:", err)
				os.Exit(1)
			}
			return
		case "client":
			if err := runClient(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "portico:", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprintf(os.Stderr, "portico: unknown argument %q\n\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}

	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// usage prints the whole interface, which is environment variables — there
// are no flags beyond the two above.
func usage() {
	fmt.Printf(`portico %s — lightweight identity and access management

Usage:
  portico              start the server
  portico tenant ...   provision tenants (see: portico tenant --help)
  portico client ...   register applications (see: portico client --help)
  portico --version    print the version
  portico --help       print this message

Configuration is entirely environment variables:

  PORTICO_ADDR                     listen address (default ":8410")
  PORTICO_DB_DRIVER                storage driver (default "postgres")
  PORTICO_DB_DSN                   required. PostgreSQL connection string, e.g.
                                   postgres://portico:secret@localhost:5432/portico?sslmode=disable
  PORTICO_JWT_SECRET               token signing secret; at least %d bytes.
                                   Generate with: openssl rand -hex 32
  PORTICO_TOKEN_TTL                token lifetime, e.g. "2h" (default "2h")
  PORTICO_TRUST_PROXY_HEADERS      trust X-Forwarded-For (default false; only
                                   enable behind a proxy you control)
  PORTICO_PUBLIC_URL               where people reach this deployment, used to
                                   build password-recovery links
                                   (default "http://localhost:8410")
  PORTICO_SMTP_HOST                mail relay for password recovery. Unset
                                   means email recovery is unavailable.
  PORTICO_SMTP_PORT                default 587
  PORTICO_SMTP_USERNAME            omit both to connect unauthenticated
  PORTICO_SMTP_PASSWORD
  PORTICO_SMTP_FROM                required once a host is set
  PORTICO_SMTP_ENCRYPTION          starttls (default) | tls | none
  PORTICO_INITIAL_ADMIN_USERNAME   bootstrap admin name (default "admin")
  PORTICO_INITIAL_ADMIN_PASSWORD   bootstrap admin password; generated and
                                   printed once if unset
  PORTICO_LOG_LEVEL                debug | info | warn | error (default "info")

Portico serves plain HTTP and does not rate-limit sign-in attempts. Run it
behind a reverse proxy that terminates TLS and throttles /api/v1/auth/*.

Documentation: https://github.com/paraview/portico
`, server.Version, config.MinJWTSecretLength)
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	setupLogging(cfg.LogLevel)

	if cfg.JWTSecretGenerated {
		slog.Warn("PORTICO_JWT_SECRET is not set; generated a random one. " +
			"All sessions will be invalidated on restart — set it explicitly in production.")
	}

	srv, err := server.New(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	if err := srv.Bootstrap(context.Background()); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout and WriteTimeout bound the whole exchange, not just
		// the headers, so a slow-body client cannot hold a connection open
		// indefinitely. They are generous because bulk import legitimately
		// takes a while.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	// Shut down cleanly on SIGINT/SIGTERM so in-flight requests finish.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("shutdown complete")
	return nil
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	// JSON to stdout, one record per line: the container runtime or process
	// supervisor owns collection, so the application never writes files.
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})

	// service_name is attached once rather than at each call site, so it can
	// never be forgotten.
	slog.SetDefault(slog.New(handler).With("service_name", "portico"))
}
