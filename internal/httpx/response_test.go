package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Paraview-RD/portico/internal/httpx"
)

func decode(t *testing.T, rec *httptest.ResponseRecorder) httpx.Envelope {
	t.Helper()
	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	return env
}

func TestOKWritesSuccessEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.OK(rec, map[string]string{"name": "alice"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	env := decode(t, rec)
	if env.Code != httpx.CodeSuccess {
		t.Errorf("code = %q, want %q", env.Code, httpx.CodeSuccess)
	}
	if env.Data == nil {
		t.Error("data is null, want the payload")
	}
}

// The envelope contract says data is always present and is null (not omitted,
// not {}) when there is nothing to return.
func TestOKWithNilDataEmitsExplicitNull(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.OK(rec, nil)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, present := raw["data"]
	if !present {
		t.Fatal("data field is missing, want an explicit null")
	}
	if string(data) != "null" {
		t.Errorf("data = %s, want null", data)
	}
}

func TestFailStatusMatchesErrorKind(t *testing.T) {
	tests := []struct {
		name       string
		err        *httpx.Error
		wantStatus int
	}{
		{"bad request", httpx.BadRequest("MALFORMED_BODY", "bad"), http.StatusBadRequest},
		{"unauthorized", httpx.Unauthorized("TOKEN_EXPIRED", "expired"), http.StatusUnauthorized},
		{"forbidden", httpx.Forbidden("ADMIN_REQUIRED", "nope"), http.StatusForbidden},
		{"not found", httpx.NotFound("USER_NOT_FOUND", "missing"), http.StatusNotFound},
		{"conflict", httpx.Conflict("USERNAME_TAKEN", "taken"), http.StatusConflict},
		{"unprocessable", httpx.UnprocessableEntity("REGISTRATION_DISABLED", "closed"), http.StatusUnprocessableEntity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil)

			httpx.Fail(rec, req, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			env := decode(t, rec)
			if env.Code != tt.err.Code {
				t.Errorf("code = %q, want %q", env.Code, tt.err.Code)
			}
			// A failure must never carry a success code — that is the
			// "HTTP 200 means everything" anti-pattern the conventions ban.
			if env.Code == httpx.CodeSuccess {
				t.Error("failure response carries the success code")
			}
		})
	}
}

// A non-*Error must not leak its message to the client.
func TestFailHidesUnexpectedErrorDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil)

	httpx.Fail(rec, req, errors.New("dial tcp 10.0.0.5:5432: connection refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	env := decode(t, rec)
	if env.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Code)
	}
	if got := rec.Body.String(); contains(got, "10.0.0.5") {
		t.Errorf("response leaked internal detail: %s", got)
	}
}

func TestErrorWithCausePreservesClientFacingFields(t *testing.T) {
	base := httpx.NotFound("USER_NOT_FOUND", "No such user.")
	wrapped := base.WithCause(errors.New("sql: no rows in result set"))

	if wrapped.Status != base.Status || wrapped.Code != base.Code || wrapped.Message != base.Message {
		t.Error("WithCause changed the client-facing fields")
	}
	if !errors.Is(wrapped, wrapped.Unwrap()) {
		t.Error("cause is not retrievable via errors.Is")
	}
	// WithCause must not mutate the original, which is often a package-level
	// sentinel shared across requests.
	if base.Unwrap() != nil {
		t.Error("WithCause mutated the receiver")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// A client that navigates away mid-request cancels it, and every operation
// still in flight fails with context.Canceled. Reporting that as a 500 makes
// ordinary browsing indistinguishable from a broken server: it fills the
// error log with entries nobody can act on, and it inflates the 5xx rate
// that an operator alerts on. These tests hold the distinction.

func TestFailReportsAClientThatWentAwaySeparately(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil).WithContext(ctx)
	cancel()

	httpx.Fail(rec, req, fmt.Errorf("list organizations: %w", context.Canceled))

	if rec.Code != httpx.StatusClientClosedRequest {
		t.Errorf("status = %d, want %d (nobody is listening, and nothing failed)",
			rec.Code, httpx.StatusClientClosedRequest)
	}
	if env := decode(t, rec); env.Code != "CLIENT_CLOSED_REQUEST" {
		t.Errorf("code = %q, want CLIENT_CLOSED_REQUEST", env.Code)
	}
}

func TestFailStillReportsCancellationOfALiveRequestAsAFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	// The request context is untouched: the caller is still waiting. A
	// cancelled error here came from somewhere inside — an internal timeout,
	// or one operation cancelling another — and it is a fault of this server.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)

	httpx.Fail(rec, req, fmt.Errorf("query: %w", context.Canceled))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: the client was still waiting, so this "+
			"is a server fault and must not be excused as a disconnect", rec.Code)
	}
}

func TestFailKeepsARealErrorEvenWhenTheClientHasGone(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/x", nil).WithContext(ctx)
	cancel()

	// A handler that decided the answer before the client left still means
	// what it said, and the audit trail and the metrics should say so.
	httpx.Fail(rec, req, httpx.NotFound("USER_NOT_FOUND", "No such user."))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestFailTreatsADeadlineAsAServerFaultEvenIfTheClientAlsoLeft(t *testing.T) {
	rec := httptest.NewRecorder()
	// Both at once, which is the case that decides the rule rather than
	// merely illustrating it: the client gave up *and* an operation this
	// server had put a deadline on missed it. Testing the deadline alone
	// passes for the wrong reason — the request context is not cancelled
	// either, so the second condition rejects it whatever the first does.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil).WithContext(ctx)
	cancel()

	// A deadline this server set and then missed is this server's problem,
	// and it is the more actionable of the two facts. Excusing it as a
	// disconnect would hide every timeout behind the one status code nobody
	// alerts on.
	httpx.Fail(rec, req, fmt.Errorf("query: %w", context.DeadlineExceeded))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: a missed deadline is a server fault "+
			"even when the client had already gone", rec.Code)
	}
}
