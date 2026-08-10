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

	"github.com/Paraview-RD/portico/internal/config"
	"github.com/Paraview-RD/portico/internal/server"
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
		case "sp":
			if err := runSP(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "portico:", err)
				os.Exit(1)
			}
			return
		case "cas":
			if err := runCAS(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "portico:", err)
				os.Exit(1)
			}
			return
		case "ready":
			// Exit status is the whole answer here: a container runtime
			// reads it and nothing else.
			if err := runReady(os.Args[2:]); err != nil {
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
  portico client ...   register OAuth/OIDC applications (see: portico client --help)
  portico sp ...       register SAML service providers (see: portico sp --help)
  portico cas ...      register CAS services (see: portico cas --help)
  portico ready        ask a running instance whether it can serve, and exit
                       0 or 1. The release image is FROM scratch, so this is
                       what a container health check has to run.
  portico --version    print the version
  portico --help       print this message

Configuration is entirely environment variables:

  PORTICO_ADDR                     listen address (default ":8410")
  PORTICO_METRICS_ADDR             Prometheus listener, e.g. 127.0.0.1:9410.
                                   Unset means no metrics endpoint at all. It
                                   is not authenticated, so bind it where only
                                   your monitoring can reach it
  PORTICO_DB_DRIVER                storage driver (default "postgres")
  PORTICO_DB_DSN                   required. PostgreSQL connection string, e.g.
                                   postgres://portico:secret@localhost:5432/portico?sslmode=disable
  PORTICO_JWT_SECRET               token signing secret; at least %d bytes.
                                   Generate with: openssl rand -hex 32
  PORTICO_TOKEN_TTL                token lifetime, e.g. "2h" (default "2h")
  PORTICO_ENCRYPTION_KEY           32 bytes of hex protecting credentials the
                                   server stores and later uses, such as a
                                   directory bind password. Unset means such a
                                   credential cannot be saved at all — it is
                                   never stored in the clear instead. Must
                                   differ from PORTICO_JWT_SECRET. Generate
                                   with: openssl rand -hex 32
  PORTICO_TRUST_PROXY_HEADERS      trust X-Forwarded-For (default false; only
                                   enable behind a proxy you control)
  PORTICO_PUBLIC_URL               where people reach this deployment. Used for
                                   password-recovery links and as the OpenID
                                   Connect issuer identifier
                                   (default "http://localhost:8410")
  PORTICO_SMTP_HOST                mail relay for password recovery. Unset
                                   means email recovery is unavailable.
  PORTICO_SMTP_PORT                default 587
  PORTICO_SMTP_USERNAME            omit both to connect unauthenticated
  PORTICO_SMTP_PASSWORD
  PORTICO_SMTP_FROM                required once a host is set
  PORTICO_SMTP_ENCRYPTION          starttls (default) | tls | none
  PORTICO_DEFAULT_LOCALE           language of messages sent to somebody whose
                                   own preference and whose tenant's default
                                   both say nothing: en-US (default) | zh-CN.
                                   A tag with no messages is refused at start
  PORTICO_INITIAL_ADMIN_USERNAME   bootstrap admin name (default "admin")
  PORTICO_INITIAL_ADMIN_PASSWORD   bootstrap admin password; generated and
                                   printed once if unset
  PORTICO_LOG_LEVEL                debug | info | warn | error (default "info")

Portico serves plain HTTP and does not rate-limit sign-in attempts. Run it
behind a reverse proxy that terminates TLS and throttles /api/v1/auth/*.

Documentation: https://github.com/Paraview-RD/portico
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

	go sweepExpired(ctx, srv)
	go dispatchWebhooks(ctx, srv)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// The metrics endpoint, when one is configured, on a listener of its
	// own. See config.MetricsAddr: it is unauthenticated by design, so it
	// must not share a port with anything a proxy publishes.
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		metricsServer = &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           srv.MetricsHandler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			slog.Info("serving metrics", "addr", cfg.MetricsAddr)
			err := metricsServer.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				// Fatal, like the main listener. An operator who configured
				// this address expects to be able to scrape it, and a
				// process that runs happily while its monitoring is silently
				// unreachable is how an outage gets noticed by a customer
				// instead of by a graph.
				errCh <- fmt.Errorf("metrics listener: %w", err)
			}
		}()
	}

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
	// After the application listener, so a scrape during shutdown still sees
	// the final state. Its failure is not returned: the application has
	// already stopped cleanly by this point, and reporting a shutdown error
	// from the monitoring port as the process's exit status would turn a
	// clean stop into a failed one.
	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("metrics listener did not shut down cleanly", "error", err)
		}
	}
	slog.Info("shutdown complete")
	return nil
}

// webhookInterval is how often queued events are delivered.
//
// Fifteen seconds, against the sweep's hour. The two are on different
// timescales because they answer to different things: the sweep tidies rows
// nobody is waiting for, while a queued event is somebody waiting to be told
// that an account was disabled. A minute of latency there would be noticed;
// an hour would make the feature pointless.
const webhookInterval = 15 * time.Second

// dispatchWebhooks delivers due events until the process is asked to stop.
func dispatchWebhooks(ctx context.Context, srv *server.Server) {
	ticker := time.NewTicker(webhookInterval)
	defer ticker.Stop()

	for {
		if err := srv.DispatchWebhooks(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("could not dispatch webhooks", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepInterval is how often the expired-row cleanup runs.
//
// Paced by the fastest-growing table: every arrival at /authorize writes a
// row and most sign-ins that start are never finished, and a request lives
// fifteen minutes. Hourly keeps that table small and is infrequent enough
// to be invisible. The other things swept — spent reset links, dead refresh
// chains, unvalidated service tickets — accumulate far more slowly and are
// happy to ride along.
const sweepInterval = time.Hour

// sweepExpired runs the periodic cleanup until the process is asked to
// stop. A failure is logged and the next tick tries again: nothing else
// depends on it, and a server that refused to serve because it could not
// delete expired rows would be worse than a table that grew.
func sweepExpired(ctx context.Context, srv *server.Server) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		if err := srv.SweepExpired(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("could not clear expired rows", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
