package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/httpx"
)

// A limiter is arithmetic over elapsed time, and a test that proved anything
// about refilling by sleeping for it would be a slow test that still could
// not prove the arithmetic. So the clock is substituted and moved by hand,
// which is also the only way to assert what happens after an hour.
func atFixedTime(t *testing.T, perMinute, burst int) (*httpx.RateLimiter, func(time.Duration)) {
	t.Helper()

	limiter := httpx.NewRateLimiter(perMinute, burst)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	httpx.SetClockForTests(limiter, func() time.Time { return now })

	return limiter, func(d time.Duration) { now = now.Add(d) }
}

func TestBurstIsSpentThenRefused(t *testing.T) {
	limiter, _ := atFixedTime(t, 60, 5)

	for i := 1; i <= 5; i++ {
		if allowed, _ := limiter.Allow("10.0.0.1"); !allowed {
			t.Fatalf("request %d of the burst was refused; the burst is the "+
				"number that may arrive at once", i)
		}
	}

	allowed, retryAfter := limiter.Allow("10.0.0.1")
	if allowed {
		t.Fatal("the sixth request was allowed, so nothing is being limited")
	}
	if retryAfter <= 0 {
		t.Errorf("refused with Retry-After %v; a caller told to wait needs to "+
			"know how long", retryAfter)
	}
}

// The refill is continuous rather than a window that resets. A fixed window
// hands an attacker who knows where the edge is two full allowances back to
// back, and refuses a person who arrived just after one for the rest of it.
func TestTheBucketRefillsWithTime(t *testing.T) {
	limiter, advance := atFixedTime(t, 60, 5)

	for i := 0; i < 5; i++ {
		limiter.Allow("10.0.0.1")
	}
	if allowed, _ := limiter.Allow("10.0.0.1"); allowed {
		t.Fatal("the burst was not spent, so what follows proves nothing")
	}

	// 60 a minute is one a second.
	advance(time.Second)
	if allowed, _ := limiter.Allow("10.0.0.1"); !allowed {
		t.Error("still refused a second later, at a rate of one per second")
	}

	// And the ceiling holds: an address quiet for an hour gets a burst, not
	// an hour's worth of tokens.
	advance(time.Hour)
	for i := 1; i <= 5; i++ {
		if allowed, _ := limiter.Allow("10.0.0.1"); !allowed {
			t.Fatalf("request %d after an idle hour was refused", i)
		}
	}
	if allowed, _ := limiter.Allow("10.0.0.1"); allowed {
		t.Error("an idle hour restored more than the burst; the bucket has no ceiling")
	}
}

// One address being throttled must not throttle anybody else. This is the
// property that makes the limiter usable at all: without it, one attacker
// takes the sign-in page away from every user.
func TestAddressesAreLimitedSeparately(t *testing.T) {
	limiter, _ := atFixedTime(t, 60, 2)

	limiter.Allow("10.0.0.1")
	limiter.Allow("10.0.0.1")
	if allowed, _ := limiter.Allow("10.0.0.1"); allowed {
		t.Fatal("the first address was not exhausted")
	}

	if allowed, _ := limiter.Allow("10.0.0.2"); !allowed {
		t.Error("a second address was refused because the first had been; " +
			"one caller can take sign-in away from everybody")
	}
}

// Zero means off, and off has to be a value rather than a flag every caller
// checks — otherwise the check is what gets forgotten.
func TestZeroPerMinuteDisablesTheLimiter(t *testing.T) {
	limiter := httpx.NewRateLimiter(0, 10)
	if limiter != nil {
		t.Fatal("a limit of zero should build no limiter")
	}

	for i := 0; i < 1000; i++ {
		if allowed, _ := limiter.Allow("10.0.0.1"); !allowed {
			t.Fatalf("a nil limiter refused request %d; it must permit everything", i)
		}
	}
}

// The middleware applies to the sign-in endpoints and to nothing else. The
// readiness probe is the case that matters: an orchestrator polling every
// second must never be told to come back later, because the instance it
// marks unhealthy is the one that answered honestly.
func TestOnlyTheAuthPathIsLimited(t *testing.T) {
	limiter := httpx.NewRateLimiter(60, 1)
	handler := httpx.RateLimitAuth(limiter)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	status := func(method, path string) int {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder.Code
	}

	if got := status(http.MethodPost, "/api/v1/auth/login"); got != http.StatusOK {
		t.Fatalf("first request to the sign-in endpoint: got %d, want 200", got)
	}
	if got := status(http.MethodPost, "/api/v1/auth/login"); got != http.StatusTooManyRequests {
		t.Errorf("second request to the sign-in endpoint: got %d, want 429", got)
	}

	for i := 0; i < 20; i++ {
		if got := status(http.MethodGet, "/api/v1/ready"); got != http.StatusOK {
			t.Fatalf("readiness probe %d: got %d, want 200. A throttle that "+
				"answers a health check with 429 marks its own instance down.",
				i, got)
		}
	}
}

// The reads under the same prefix are not rationed. Both are what the
// sign-in screen asks on load — whether registration is open, which recovery
// channels exist — and neither costs more than a cheap lookup. Counting them
// meant a person opening the page a few times, or several colleagues behind
// one address doing so, spent the allowance meant for password attempts.
func TestReadsUnderTheAuthPathAreNotLimited(t *testing.T) {
	limiter := httpx.NewRateLimiter(60, 1)
	handler := httpx.RateLimitAuth(limiter)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	for i := 0; i < 20; i++ {
		for _, path := range []string{
			"/api/v1/auth/registration-status",
			"/api/v1/auth/recovery-channels",
		} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("%s on load %d: got %d, want 200. Opening the sign-in "+
					"screen must not spend the allowance that exists for "+
					"password attempts.", path, i, recorder.Code)
			}
		}
	}

	// And the writes still are, on the same limiter.
	post := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, post)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("sign-in got %d after forty reads; the reads consumed the "+
			"allowance or the limiter stopped applying", second.Code)
	}
}

func TestRefusalCarriesRetryAfter(t *testing.T) {
	limiter := httpx.NewRateLimiter(60, 1)
	handler := httpx.RateLimitAuth(limiter)(http.HandlerFunc(
		func(_ http.ResponseWriter, _ *http.Request) {}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After header; it is the only part of this refusal a " +
			"client can act on without a person reading the message")
	}
}
