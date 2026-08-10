package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Paraview-RD/portico/internal/httpx"
)

func TestSecurityHeadersAreSet(t *testing.T) {
	handler := httpx.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		// The console can disable accounts and reset passwords, so it must
		// never be frameable.
		"X-Frame-Options": "DENY",
		"Referrer-Policy": "no-referrer",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy; this is what limits the blast radius of an XSS")
	}
	for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !contains(csp, directive) {
			t.Errorf("CSP is missing %q: %s", directive, csp)
		}
	}
}

// Responses carrying account data must not land in a shared cache.
func TestAPIResponsesAreNotCacheable(t *testing.T) {
	handler := httpx.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("api path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})

	// Static assets are hashed and should stay cacheable.
	t.Run("static asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/index-abc123.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Cache-Control"); got == "no-store" {
			t.Error("static assets were marked no-store, defeating browser caching")
		}
	})
}

// The audit log's IP column is only meaningful if a caller cannot choose it.
func TestClientIPIgnoresForwardingHeadersByDefault(t *testing.T) {
	httpx.TrustProxyHeaders(false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("X-Forwarded-For", "10.0.0.5")
	req.Header.Set("X-Real-Ip", "10.0.0.6")

	if got := httpx.ClientIP(req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want the real peer address; a caller must not be able to forge it", got)
	}
}

func TestClientIPUsesForwardingHeadersWhenTrusted(t *testing.T) {
	httpx.TrustProxyHeaders(true)
	t.Cleanup(func() { httpx.TrustProxyHeaders(false) })

	t.Run("single hop", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.9")

		if got := httpx.ClientIP(req); got != "203.0.113.9" {
			t.Errorf("ClientIP = %q, want 203.0.113.9", got)
		}
	})

	// The leftmost entry is client-supplied. Taking the rightmost means a
	// spoofed prefix cannot displace what our own proxy appended.
	t.Run("spoofed prefix is ignored", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")

		if got := httpx.ClientIP(req); got != "203.0.113.9" {
			t.Errorf("ClientIP = %q, want the proxy-appended 203.0.113.9, not the client-claimed 1.2.3.4", got)
		}
	})

	t.Run("falls back to peer when no header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.7:9999"

		if got := httpx.ClientIP(req); got != "198.51.100.7" {
			t.Errorf("ClientIP = %q, want 198.51.100.7", got)
		}
	})
}
