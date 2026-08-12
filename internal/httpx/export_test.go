package httpx

import "time"

// SetClockForTests replaces a limiter's clock.
//
// It lives in a _test.go file, which is what makes it unshippable: the
// identifier does not exist in a build of the product, so there is no
// version of "somebody called the test hook in production" to guard
// against.
func SetClockForTests(l *RateLimiter, now func() time.Time) {
	l.now = now
}
