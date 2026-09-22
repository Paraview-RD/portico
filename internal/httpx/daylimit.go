package httpx

import (
	"sync"
	"time"
)

// dayLimiterIdleTimeout mirrors bucketIdleTimeout in ratelimit.go: how long
// a key's window outlives its last request, so a flood does not leave a map
// entry per key forever.
const dayLimiterIdleTimeout = 25 * time.Hour

// dayWindow is one key's count within its current window.
type dayWindow struct {
	windowStart time.Time
	count       int
	lastSeen    time.Time
}

// DayLimiter caps how many times a key may be allowed within a fixed
// window, reset entirely once the window passes rather than refilling
// continuously.
//
// This is deliberately not RateLimiter (ratelimit.go): a token bucket
// configured for "N per day" by setting perMinute to N/1440 refills
// continuously, so five uses at 09:00 do not block a sixth at 13:00 -- the
// cap silently stops being a daily cap. A day window resets as a whole,
// which is the right shape for "how many SMS codes has this phone number
// received today."
//
// Per-process, like RateLimiter: counts reset on restart and are not
// shared across instances. That is the same tradeoff RateLimiter already
// makes for password brute-force protection; see its doc comment. These
// counters protect a deployment's shared SMS budget, which is exactly the
// kind of thing that already lives outside the tenant-scoped data model
// (see docs/superpowers/specs/2026-09-21-sms-otp-login-design.md §3.2).
type DayLimiter struct {
	limit  int
	window time.Duration

	// now is time.Now, replaced in tests.
	now func() time.Time

	mu        sync.Mutex
	windows   map[string]*dayWindow
	lastSweep time.Time
}

// NewDayLimiter returns a limiter allowing limit uses of a key within
// window, reset as a whole once window has passed since the key's first use
// in its current window.
func NewDayLimiter(limit int, window time.Duration) *DayLimiter {
	return &DayLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		windows: map[string]*dayWindow{},
	}
}

// Allow reports whether key may be used once more, and counts it if so.
func (l *DayLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	w, ok := l.windows[key]
	if !ok || now.Sub(w.windowStart) >= l.window {
		w = &dayWindow{windowStart: now}
		l.windows[key] = w
	}
	w.lastSeen = now

	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// sweep drops windows nobody has used recently. Called with the lock held.
func (l *DayLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < dayLimiterIdleTimeout {
		return
	}
	l.lastSweep = now
	for key, w := range l.windows {
		if now.Sub(w.lastSeen) > dayLimiterIdleTimeout {
			delete(l.windows, key)
		}
	}
}
