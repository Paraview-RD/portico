package httpx

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The floor under the sign-in endpoints, and it is a floor rather than a
// solution.
//
// Everything under /api/v1/auth/ is reachable without a credential, because
// the caller is somebody who does not have one yet. Two of those endpoints
// cost a bcrypt comparison whatever the answer — that is deliberate, and it
// is what stops the response time from saying whether an account exists —
// and one sends mail. Unthrottled, that is an unauthenticated request that
// buys tens of milliseconds of somebody else's CPU, repeatable as fast as a
// socket can be opened.
//
// The documented answer has been "put a throttle in the reverse proxy", and
// it stays the answer: a proxy sees the traffic before it reaches this
// process, survives a restart, and can be shared across instances, none of
// which is true here. What that answer did not cover is that the proxy is a
// deployment decision and this is not — a first run, a demonstration, a
// container somebody exposed to try it out, all of them reach the sign-in
// endpoint with nothing in front. The cost of the floor is a hundred lines
// and no dependency; the cost of not having one is that "unprotected" is the
// default state.
//
// Per address, and only per address. A per-account limit sounds like the
// obvious companion and is a different control with a different failure
// mode: it needs the identifier, which lives in the request body, and it
// hands anybody who knows a username a way to keep that account's owner out.
// Portico already has the per-account control — the lockout in
// service.PasswordPolicy, which locks after a threshold and unlocks on a
// timer — and it is deliberately not this.
//
// So what remains uncovered, and should be said plainly rather than left for
// somebody to discover: an attacker with many addresses is not slowed by
// this, and a single address behind a corporate NAT shares one bucket with
// everybody else there. Both are the proxy's job. This makes the floor
// non-zero.

// AuthRateLimitPath is the prefix the limiter applies to. Everything under
// it is reachable without a credential; nothing else here is.
const AuthRateLimitPath = "/api/v1/auth/"

// bucketIdleTimeout is how long an address's bucket outlives its last
// request. Long enough that a person filling in a form is still recognized,
// short enough that a flood from a botnet does not leave a map entry per
// address forever.
const bucketIdleTimeout = 10 * time.Minute

// bucket is one address's allowance, as a token bucket.
//
// Not a fixed window: a window resets on a clock edge, so an attacker who
// knows where the edge is gets two full allowances back to back, and a
// person who arrives just after one resets is refused for the rest of it.
// The bucket refills continuously, which is both fairer to the person and
// tighter against the attacker.
type bucket struct {
	tokens   float64
	lastFill time.Time
	lastSeen time.Time
}

// RateLimiter throttles a path prefix per client address.
//
// The zero value is not usable; NewRateLimiter builds one. A nil
// *RateLimiter permits everything, which is what a deployment that set the
// limit to zero gets — the same shape secrets.Vault uses for "not
// configured", so an unset limiter is a value rather than a branch at every
// call site.
type RateLimiter struct {
	// perSecond is the sustained rate; burst is how much of it may arrive at
	// once, and therefore also the ceiling on the bucket.
	perSecond float64
	burst     float64

	// now is time.Now, replaced in tests. A limiter is about elapsed time,
	// and a test that proved anything about refilling by sleeping for it
	// would be a slow test that still could not prove the arithmetic.
	now func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	// lastSweep bounds how often the map is walked. Sweeping on every
	// request would make a flood O(addresses) per request, which is the
	// wrong shape for the case that matters.
	lastSweep time.Time
}

// NewRateLimiter returns a limiter allowing perMinute requests per address
// per minute, of which burst may arrive at once. A perMinute of zero returns
// nil, which permits everything.
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 1
	}
	return &RateLimiter{
		perSecond: float64(perMinute) / 60,
		burst:     float64(burst),
		now:       time.Now,
		buckets:   map[string]*bucket{},
	}
}

// Allow reports whether a request from key may proceed, and if not, how long
// the caller should wait before the next one would.
func (l *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	if l == nil {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, lastFill: now}
		l.buckets[key] = b
	}

	// Refill for the time since the last request, capped at the burst: an
	// address that has been quiet for an hour gets a full allowance, not an
	// hour's worth.
	b.tokens += now.Sub(b.lastFill).Seconds() * l.perSecond
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastFill = now
	b.lastSeen = now

	if b.tokens < 1 {
		// How long until one whole token exists. Rounded up, because
		// answering with the exact fraction tells a caller to retry at the
		// instant it would still be refused.
		wait := time.Duration((1 - b.tokens) / l.perSecond * float64(time.Second))
		return false, wait.Round(time.Second) + time.Second
	}

	b.tokens--
	return true, 0
}

// sweep drops buckets nobody has used recently. Called with the lock held.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < bucketIdleTimeout {
		return
	}
	l.lastSweep = now

	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > bucketIdleTimeout {
			delete(l.buckets, key)
		}
	}
}

// RateLimitAuth throttles the sign-in endpoints and leaves everything else
// alone.
//
// The prefix check is here rather than in the router because the group that
// holds these routes also holds /health and /ready, and an orchestrator
// polling readiness every second must never be told to come back later —
// that is an instance marked unhealthy by its own throttle.
func RateLimitAuth(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil || !isAuthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			allowed, retryAfter := limiter.Allow(ClientIP(r))
			if !allowed {
				// Retry-After is the standard way to say it and the only part
				// of this a client can act on automatically.
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				Fail(w, r, TooManyRequests("TOO_MANY_ATTEMPTS",
					"Too many attempts from this address. Please wait and try again."))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAuthPath(path string) bool {
	return len(path) >= len(AuthRateLimitPath) && path[:len(AuthRateLimitPath)] == AuthRateLimitPath
}
