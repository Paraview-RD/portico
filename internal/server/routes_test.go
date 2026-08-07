package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paraview/portico/internal/config"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/server"
	"github.com/paraview/portico/internal/testdb"
)

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// Each test gets its own database file so runs stay independent.
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = testdb.DSN(t)

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Code != httpx.CodeSuccess {
		t.Errorf("code = %q, want SUCCESS", env.Code)
	}
}

func TestUnknownRouteReturnsEnvelope(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	// Even 404s follow the envelope contract, so clients can parse every
	// response the same way.
	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("404 body is not the standard envelope: %v", err)
	}
	if env.Code != "ROUTE_NOT_FOUND" {
		t.Errorf("code = %q, want ROUTE_NOT_FOUND", env.Code)
	}
}

func TestMethodNotAllowedReturnsEnvelope(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("405 body is not the standard envelope: %v", err)
	}
	if env.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("code = %q, want METHOD_NOT_ALLOWED", env.Code)
	}
}

// Every response must carry a correlation id, generating one when the client
// does not supply it.
func TestRequestIDIsAlwaysPresent(t *testing.T) {
	srv := newTestServer(t)

	t.Run("generated when absent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get(httpx.RequestIDHeader); got == "" {
			t.Error("response has no request id header")
		}
	})

	t.Run("client id is echoed back", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Header.Set(httpx.RequestIDHeader, "client-supplied-id")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get(httpx.RequestIDHeader); got != "client-supplied-id" {
			t.Errorf("request id = %q, want the client-supplied value", got)
		}
	})
}
