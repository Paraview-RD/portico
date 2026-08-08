package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Middleware counts and times every request.
//
// It reads the route pattern rather than the path, and it can only do that
// after the router has matched, which is why the read happens on the way out
// rather than on the way in.
func (m *Registry) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		start := time.Now()
		rec := &statusWriter{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		route := routePattern(r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}

		m.requests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
		m.requestDuration.
			WithLabelValues(r.Method, route).
			Observe(time.Since(start).Seconds())
	})
}

// routePattern returns the matched route, or a placeholder.
//
// The point is what it never returns: the request path. That is attacker-
// controlled on every 404, and using it would hand an unbounded label set to
// anyone who can send a request — a few thousand requests for random URLs
// and the metrics endpoint becomes the largest thing this process produces.
// An operator wants the count of misses anyway, not the list; the access log
// has the paths.
//
// In practice chi supplies a wildcard pattern for a miss rather than an
// empty one, so the placeholder below is reached only when this middleware
// runs outside a chi router. It stays because that is exactly the case where
// the absence of a pattern would otherwise be silent.
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "unmatched"
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return "unmatched"
}

// statusWriter records the status code for the counter.
//
// Deliberately not httpx's: metrics is a leaf, and depending on the web
// stack to observe the web stack is how a package stops being substitutable
// in a test.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets the standard library reach the underlying writer for
// interfaces this wrapper does not implement — http.Flusher in particular,
// which a streaming response needs and which would otherwise disappear
// behind the wrapper.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
