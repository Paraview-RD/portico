package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "requestID"

// RequestIDHeader carries the per-request correlation id in and out.
const RequestIDHeader = "X-Request-Id"

// RequestID assigns each request a correlation id, honoring a client-supplied
// one when present, and echoes it back in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the correlation id assigned to ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// statusRecorder captures the status code for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLog emits one structured line per request after it completes.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		// The level follows the outcome: a server error has to stand out
		// from routine traffic, and a rejected request is worth noticing
		// without being an alert.
		level := slog.LevelInfo
		switch {
		case rec.status >= http.StatusInternalServerError:
			level = slog.LevelError
		// Not a rejection: the client navigated away mid-request. Logging it
		// as a warning would put routine browsing in the same bucket as
		// requests this server refused.
		case rec.status == StatusClientClosedRequest:
		case rec.status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "request",
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", ClientIP(r),
		)
	})
}

// Recover turns a panic into a 500 response so one bad handler cannot take
// down the process.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"request_id", RequestIDFrom(r.Context()),
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				Fail(w, r, Internal(nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// trustProxyHeaders controls whether forwarding headers are believed. It is
// set once at startup from configuration.
//
// Off by default on purpose: the leftmost X-Forwarded-For entry is the one
// part of that header a client fully controls, so trusting it unconditionally
// lets anyone stamp their own address into the audit log — and defeats any
// per-IP throttle placed in front of it. Only enable it when a proxy you
// control is guaranteed to be in front and to rewrite the header.
var trustProxyHeaders bool

// TrustProxyHeaders enables the use of X-Forwarded-For and X-Real-Ip.
func TrustProxyHeaders(trust bool) { trustProxyHeaders = trust }

// ClientIP returns the caller's address for logging and auditing.
func ClientIP(r *http.Request) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Right-to-left: entries are appended by each hop, so the
			// rightmost is the one our own proxy wrote and the leftmost is
			// whatever the client claimed.
			parts := strings.Split(xff, ",")
			if candidate := trimSpace(parts[len(parts)-1]); candidate != "" {
				return candidate
			}
		}
		if xrip := trimSpace(r.Header.Get("X-Real-Ip")); xrip != "" {
			return xrip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// SecurityHeaders applies the response headers that cost nothing for a
// self-hosted single-origin app and remove whole classes of attack.
//
// HSTS is deliberately absent: it is only meaningful over TLS, which this
// process does not terminate, and emitting it over plaintext would either be
// ignored or pin a host to HTTPS it cannot serve. The reverse proxy that
// terminates TLS is the right place for it — see docs/access-guide.md.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		// The admin console can disable accounts and reset passwords, so it
		// must never be frameable.
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// The SPA is served from this same origin with no CDN and no inline
		// scripts, so a strict policy costs nothing. style-src allows inline
		// because the bundler emits a style element.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; font-src 'self'; "+
				"object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")

		// API responses carry account data and tokens; keep them out of
		// shared caches.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}

		next.ServeHTTP(w, r)
	})
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
