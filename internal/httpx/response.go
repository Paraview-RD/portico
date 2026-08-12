// Package httpx implements the wire format described in
// docs/api-conventions.md: a {code, message, data} envelope, with the HTTP
// status and the envelope code always agreeing on success vs failure.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// StatusClientClosedRequest is nginx's 499. It is not in any RFC, and it is
// used here for the one thing it says that no standard code does: nobody is
// listening any more, and nothing went wrong.
//
// The response never reaches anyone — the connection is gone. It exists so
// that the access log and the metrics can tell a client that navigated away
// from a server that failed, which otherwise look identical and inflate the
// error rate with traffic that was working correctly.
const StatusClientClosedRequest = 499

// CodeSuccess is the envelope code for every successful response.
const CodeSuccess = "SUCCESS"

// Envelope is the response body shape used by every JSON endpoint.
type Envelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Data is always present, and is null (not {} or []) when there is no
	// result to return.
	Data any `json:"data"`
}

// OK writes a 200 response carrying data.
func OK(w http.ResponseWriter, data any) {
	write(w, http.StatusOK, Envelope{Code: CodeSuccess, Message: "", Data: data})
}

// Error is a failure that carries both an HTTP status and a stable,
// machine-readable code. It implements error so it can be returned through
// service layers and rendered once at the HTTP boundary.
type Error struct {
	// Status is the HTTP status code. Always 4xx for expected failures;
	// 5xx is reserved for bugs and infrastructure problems.
	Status int
	// Code is a SCREAMING_SNAKE_CASE identifier, e.g. USER_NOT_FOUND.
	Code string
	// Message is human-readable and safe to show to an end user.
	Message string
	// cause is the underlying error, logged but never sent to the client.
	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an underlying error for logging without changing what
// the client sees.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

// NewError builds an Error. Prefer the constructors below for the common
// statuses so the status/code pairing stays consistent.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// BadRequest reports a malformed request: missing field, wrong type,
// invalid JSON. Not for requests that are well-formed but rejected.
func BadRequest(code, message string) *Error {
	return NewError(http.StatusBadRequest, code, message)
}

// Unauthorized reports that the caller could not be identified: missing,
// invalid, or expired credentials.
func Unauthorized(code, message string) *Error {
	return NewError(http.StatusUnauthorized, code, message)
}

// Forbidden reports that the caller is known but not allowed to perform
// this action.
func Forbidden(code, message string) *Error {
	return NewError(http.StatusForbidden, code, message)
}

// NotFound reports that the addressed resource does not exist.
func NotFound(code, message string) *Error {
	return NewError(http.StatusNotFound, code, message)
}

// Conflict reports a well-formed request that clashes with current state,
// such as reusing a username that already exists.
func Conflict(code, message string) *Error {
	return NewError(http.StatusConflict, code, message)
}

// UnprocessableEntity reports a well-formed, non-conflicting request that a
// business rule rejects, such as registering while registration is closed.
func UnprocessableEntity(code, message string) *Error {
	return NewError(http.StatusUnprocessableEntity, code, message)
}

// TooManyRequests reports a caller who has been refused for their rate
// rather than for anything about the request. It is the one 4xx here that
// says nothing about whether the request would otherwise have succeeded,
// which is the point: an attacker learns only that they were too fast.
func TooManyRequests(code, message string) *Error {
	return NewError(http.StatusTooManyRequests, code, message)
}

// Internal reports a server-side failure. The cause is logged; the client
// gets a generic message so internals are not leaked.
func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "INTERNAL_ERROR",
		Message: "An unexpected error occurred.",
		cause:   err,
	}
}

// Fail renders err to the client. Any error that is not an *Error is
// treated as an internal failure, so handlers can return raw errors without
// risking a detail leak.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	// errors.As, not a type assertion: a service that wraps an *Error with
	// context would otherwise be reported as a generic 500, losing both the
	// intended status and the client-facing code.
	var apiErr *Error
	switch {
	case errors.As(err, &apiErr):
		// Deliberate first: a handler that returns a real *Error while the
		// client happens to have disconnected still means what it says.
	case clientWentAway(r, err):
		apiErr = &Error{
			Status:  StatusClientClosedRequest,
			Code:    "CLIENT_CLOSED_REQUEST",
			Message: "The client closed the request.",
			cause:   err,
		}
	default:
		apiErr = Internal(err)
	}

	if apiErr.Status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed",
			"error_code", apiErr.Code,
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"error", apiErr.Error(),
		)
	}

	write(w, apiErr.Status, Envelope{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Data:    nil,
	})
}

// clientWentAway reports whether err is the wreckage of a request the client
// abandoned, rather than a failure of this server.
//
// Both conditions are required. A cancelled error alone is not enough: an
// internal timeout, or one operation cancelling another, produces the same
// error while the caller is still waiting for an answer — and answering
// "you left" to somebody who is still there would hide a real fault. The
// request context being done is what distinguishes the two.
//
// DeadlineExceeded is deliberately not included. A deadline this server set
// and then missed is a server problem, and it should be counted as one.
func clientWentAway(r *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) &&
		errors.Is(r.Context().Err(), context.Canceled)
}

func write(w http.ResponseWriter, status int, body Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be logged.
		slog.Error("write response body", "error", err)
	}
}
