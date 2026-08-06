package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
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
		slog.InfoContext(r.Context(), "request",
			"requestId", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"durationMs", time.Since(start).Milliseconds(),
			"ip", ClientIP(r),
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
					"requestId", RequestIDFrom(r.Context()),
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

// ClientIP extracts the caller's address, trusting X-Forwarded-For only for
// its first entry (the original client) when present.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return trimSpace(xff[:i])
			}
		}
		return trimSpace(xff)
	}
	if xrip := r.Header.Get("X-Real-Ip"); xrip != "" {
		return trimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
