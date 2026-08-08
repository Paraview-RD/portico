package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Delivery attempt policy.
//
// Five attempts over roughly half an hour, then the delivery is marked
// failed and left in the table for somebody to look at. The alternative —
// retrying indefinitely — turns one broken subscriber into an unbounded
// queue and a permanent load on their server, and the operator finds out
// from them rather than from the delivery list.
const (
	MaxAttempts = 5
	// The request timeout. Generous enough for a receiver doing real work,
	// short enough that a hung endpoint does not hold a worker slot for
	// minutes.
	RequestTimeout = 20 * time.Second
	// How many deliveries one pass takes. A bound rather than "everything
	// due": a backlog is drained over several passes instead of one pass
	// that runs for an unbounded time holding rows locked.
	BatchSize = 20
)

// Backoff returns how long to wait before attempt n (1-based, for the
// attempt that just failed).
//
// Exponential, and deliberately without jitter for a reason worth stating:
// deliveries here are already spread out by when their events happened, and
// every subscription has its own row. The thundering-herd problem jitter
// solves needs many clients waking together, which is not this shape.
func Backoff(attempt int32) time.Duration {
	switch {
	case attempt <= 1:
		return 30 * time.Second
	case attempt == 2:
		return 2 * time.Minute
	case attempt == 3:
		return 5 * time.Minute
	default:
		return 20 * time.Minute
	}
}

// Result is what one attempt produced.
type Result struct {
	StatusCode int
	Err        error
	// Retryable is false for the failures that will not become successes:
	// a 400 means the receiver understood and refused, and sending it four
	// more times produces four more refusals. A 500 or a timeout is worth
	// retrying — that is what retries are for.
	Retryable bool
}

// Deliver sends one event and reports what happened.
//
// The client is the one from NewClient, whose dialer refuses local
// addresses: this function does not re-validate the URL, because the check
// that matters is at connection time and re-checking here would only add a
// resolution whose answer could differ from the one used.
func Deliver(ctx context.Context, client *http.Client, url, secret, eventType, deliveryID string, body []byte) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{Err: err, Retryable: false}
	}

	now := time.Now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Portico-Webhook/1")
	req.Header.Set(HeaderEvent, eventType)
	req.Header.Set(HeaderDelivery, deliveryID)
	req.Header.Set(HeaderTimestamp, formatTimestamp(now))
	req.Header.Set(HeaderSignature, Sign(secret, now, body))

	resp, err := client.Do(req)
	if err != nil {
		// Network-level: unreachable, TLS failure, timeout, or the dialer
		// refusing an address. Retryable — except that a refused address
		// stays refused, which the attempt limit takes care of.
		return Result{Err: err, Retryable: true}
	}
	defer func() { _ = resp.Body.Close() }()

	// Drained and discarded so the connection can be reused, with a cap so a
	// receiver answering with a gigabyte cannot make this the problem.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return Result{StatusCode: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode >= 500:
		return Result{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("receiver returned %d", resp.StatusCode),
			Retryable:  true,
		}
	default:
		// 3xx included: redirects are not followed, so one arriving here
		// means the receiver is pointing somewhere else and somebody has to
		// update the subscription.
		return Result{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("receiver returned %d", resp.StatusCode),
			Retryable:  false,
		}
	}
}

// LogAttempt records an attempt at debug level.
//
// Deliberately not an error log for a failed delivery: a subscriber being
// down is their operational problem, not this server's, and logging it at
// error level would make Portico's error rate a function of somebody else's
// uptime. The delivery list is where an operator looks.
func LogAttempt(ctx context.Context, subscriptionID, eventType string, result Result) {
	slog.DebugContext(ctx, "webhook delivery attempt",
		"subscription", subscriptionID,
		"event", eventType,
		"status", result.StatusCode,
		"error", result.Err,
		"retryable", result.Retryable,
	)
}
