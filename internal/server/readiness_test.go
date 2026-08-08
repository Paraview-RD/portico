package server_test

import (
	"net/http"
	"testing"
)

// Liveness and readiness answer different questions, and an orchestrator
// does different things with the answers. These hold them apart.

func TestHealthAnswersWithoutTheDatabase(t *testing.T) {
	api := newAPITest(t)

	// Break the database underneath a running server, exactly as an outage
	// would. Liveness must still say the process is fine: restarting every
	// instance does not fix a database outage, it turns one failing
	// dependency into a fleet-wide restart loop at the worst moment.
	api.closeDatabase(t)

	res := api.do(http.MethodGet, "/api/v1/health", "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("health = %d %s, want 200 — liveness must not depend on the database",
			res.Status, res.Code)
	}

	var health struct {
		Status string `json:"status"`
	}
	res.into(t, &health)
	if health.Status != "ok" {
		t.Errorf("status = %q, want ok", health.Status)
	}
}

func TestReadinessReportsTheDatabase(t *testing.T) {
	api := newAPITest(t)

	res := api.do(http.MethodGet, "/api/v1/ready", "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("ready = %d %s %s, want 200", res.Status, res.Code, res.Message)
	}

	var ready struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
	res.into(t, &ready)
	if ready.Status != "ready" || ready.Database != "ok" {
		t.Errorf("ready = %+v, want ready/ok", ready)
	}
}

func TestReadinessFailsWhenTheDatabaseIsGone(t *testing.T) {
	api := newAPITest(t)
	api.closeDatabase(t)

	res := api.do(http.MethodGet, "/api/v1/ready", "", nil)
	if res.Status != http.StatusServiceUnavailable {
		t.Fatalf("ready with no database = %d %s, want 503", res.Status, res.Code)
	}
	if res.Code != "NOT_READY" {
		t.Errorf("code = %q, want NOT_READY", res.Code)
	}

	// The driver's error can carry a host, a port, and sometimes a user, and
	// this endpoint needs no authentication. It goes to the log, not the
	// response.
	for _, leak := range []string{"postgres://", "password", "sslmode", "dial tcp"} {
		if contains([]string{res.Message}, leak) {
			t.Errorf("the response leaks connection detail: %q", res.Message)
		}
	}
}

// Both probes are reachable without credentials. An orchestrator has none,
// and a probe that needed them would be configured with a long-lived token
// sitting in a manifest.
func TestProbesNeedNoCredentials(t *testing.T) {
	api := newAPITest(t)

	for _, path := range []string{"/api/v1/health", "/api/v1/ready"} {
		if res := api.do(http.MethodGet, path, "", nil); res.Status == http.StatusUnauthorized {
			t.Errorf("%s requires authentication", path)
		}
	}
}
