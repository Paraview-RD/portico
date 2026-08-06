package httpx_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paraview/keylite/internal/httpx"
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
