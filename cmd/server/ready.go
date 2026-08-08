package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// `portico ready` asks a running instance whether it can serve.
//
// It exists because the release image is FROM scratch: no shell, no curl, no
// wget. A container health check has to be something the image already
// contains, and the only thing it contains is this binary. Without this the
// readiness endpoint would be unreachable from exactly the place that most
// wants it — an orchestrator deciding whether to route traffic here.
//
// It talks to the instance over HTTP rather than opening the database
// itself, deliberately: what matters is whether the process that is serving
// requests can reach its dependencies, not whether a fresh connection from a
// second process could.

// readyProbeTimeout bounds the whole check. A container health check that
// hangs is reported by the runtime as a timeout anyway, but with a less
// useful message and after much longer.
const readyProbeTimeout = 5 * time.Second

func runReady(args []string) error {
	fs := flag.NewFlagSet("ready", flag.ContinueOnError)
	url := fs.String("url", "", "base URL of the instance to check "+
		"(defaults to http://127.0.0.1 plus the port from PORTICO_ADDR)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target := strings.TrimSuffix(*url, "/")
	if target == "" {
		target = localProbeURL()
	}

	ctx, cancel := context.WithTimeout(context.Background(), readyProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		target+"/api/v1/ready", nil)
	if err != nil {
		return fmt.Errorf("ready: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ready: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusOK {
		fmt.Println("ready")
		return nil
	}

	// The envelope carries a code and a message; printing them is what makes
	// `docker inspect` on an unhealthy container say something useful rather
	// than just "exit 1".
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err == nil && envelope.Code != "" {
		return fmt.Errorf("ready: %s (%s)", envelope.Message, envelope.Code)
	}
	return fmt.Errorf("ready: the instance answered %s", res.Status)
}

// localProbeURL builds the address of an instance running in this container,
// from the same environment variable the server listens on.
//
// PORTICO_ADDR is usually ":8410" — a bind address, not a dialable one. The
// host part is replaced rather than used, because a server bound to all
// interfaces is reachable on loopback and a probe should not leave the
// container to check the process inside it.
func localProbeURL() string {
	addr := os.Getenv("PORTICO_ADDR")
	if addr == "" {
		addr = ":8410"
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "8410"
	}
	return "http://127.0.0.1:" + port
}
