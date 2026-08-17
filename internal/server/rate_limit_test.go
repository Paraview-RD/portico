package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// The throttle, through the whole stack rather than against the middleware
// on its own.
//
// internal/httpx proves the arithmetic: the burst, the refill, one address
// not affecting another. What it cannot prove is that the limiter is wired
// into the router the requests actually go through, in front of the endpoint
// that costs a password hash and behind the log that says it fired. That is
// what these check, and it is the part that would silently stop being true —
// the middleware would still pass its own tests with the r.Use line deleted.

func TestSignInRefusesAFloodFromOneAddress(t *testing.T) {
	cfg := testConfig(t)
	// One a minute, so nothing refills while this runs.
	//
	// 60 was the first version and it was a race against the bucket: at one
	// token a second, four requests that take longer than a second between
	// them see the fourth allowed, correctly. That passed on its own branch
	// and failed on main under the race detector, where every bcrypt is slow
	// and the package it shares is slower — the exact shape of test that is
	// green until it is somebody else's problem. What is being asserted here
	// is that the burst is finite, not how fast it refills; internal/httpx
	// asserts the refill against a clock it controls.
	cfg.AuthRateLimit = 1
	cfg.AuthRateLimitBurst = 3
	api := newAPITestWithConfig(t, cfg)

	credentials := map[string]string{"identifier": "nobody", "password": "wrong-password-guess"}

	// The burst, spent on an account that does not exist: the answer is the
	// same either way, and the cost to the server is a password hash either
	// way, which is the point of throttling this rather than counting
	// failures.
	for i := 1; i <= 3; i++ {
		res := api.do(http.MethodPost, "/api/v1/auth/login", "", credentials)
		if res.Status == http.StatusTooManyRequests {
			t.Fatalf("attempt %d of a burst of 3 was throttled; the burst is "+
				"the number that may arrive at once", i)
		}
	}

	res := api.do(http.MethodPost, "/api/v1/auth/login", "", credentials)
	if res.Status != http.StatusTooManyRequests {
		t.Fatalf("the fourth attempt got %d %s, want 429.\n"+
			"Either the limiter is no longer in the router's middleware chain, "+
			"or it is not in front of /api/v1/auth/.", res.Status, res.Code)
	}
	if res.Code != "TOO_MANY_ATTEMPTS" {
		t.Errorf("refused with code %q, want TOO_MANY_ATTEMPTS", res.Code)
	}
}

// A throttled sign-in endpoint must not throttle liveness or readiness. An
// orchestrator polls those every few seconds and acts on the answer; an
// instance that answers its own probe with 429 takes itself out of service
// under exactly the load the throttle exists for.
func TestHealthChecksAreNeverThrottled(t *testing.T) {
	cfg := testConfig(t)
	// One a minute again, for the reason above: the refusal this test needs
	// must not expire while the test is running.
	cfg.AuthRateLimit = 1
	cfg.AuthRateLimitBurst = 1
	api := newAPITestWithConfig(t, cfg)

	// Spend the allowance on the endpoint that has one.
	api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"identifier": "nobody", "password": "wrong-password-guess"})
	if res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"identifier": "nobody", "password": "wrong-password-guess"}); res.Status != http.StatusTooManyRequests {
		t.Fatalf("sign-in was not throttled (%d), so what follows proves nothing", res.Status)
	}

	for i := 0; i < 10; i++ {
		if res := api.do(http.MethodGet, "/api/v1/health", "", nil); res.Status != http.StatusOK {
			t.Fatalf("liveness probe %d answered %d while sign-in was throttled", i, res.Status)
		}
		if res := api.do(http.MethodGet, "/api/v1/ready", "", nil); res.Status != http.StatusOK {
			t.Fatalf("readiness probe %d answered %d while sign-in was throttled", i, res.Status)
		}
	}
}

// Signing in normally is not affected by the shipped default. A limit that
// gets in the way of ordinary use is one somebody turns off.
func TestOrdinarySignInIsNotThrottledAtTheDefault(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthRateLimit = 60
	cfg.AuthRateLimitBurst = 10
	api := newAPITestWithConfig(t, cfg)

	// Three wrong guesses and then the right password, which is what a person
	// who mistypes looks like.
	for i := 0; i < 3; i++ {
		api.do(http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"identifier": adminUsername, "password": "wrong-password-guess"})
	}

	res := api.do(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"identifier": adminUsername, "password": adminPassword})
	if res.Status != http.StatusOK {
		t.Errorf("signing in after three mistyped passwords answered %d %s, want 200",
			res.Status, res.Code)
	}
}

// The trial endpoints spend their own budget, not sign-in's.
//
// They shared one at first, which meant sixty tenant-creation attempts a
// minute from one address — right for a password somebody keeps mistyping and
// far too generous for an endpoint that creates a tenant and mails a stranger.
//
// This is the only test that can catch a return to the shared budget. Every
// trial fixture raises both limits out of the way in order to submit the form
// a dozen times in a millisecond, so a routes.go that read AuthRateLimit again
// would pass all of them: here the two numbers are deliberately far apart, and
// only the trial one is small.
func TestTheTrialEndpointsHaveTheirOwnBudget(t *testing.T) {
	silenceLogs(t)

	cfg := testConfig(t)
	cfg.TrialSignup = true
	cfg.TrialMaxTenants = 50
	// Generous for sign-in, and tight for trials. If the trial route is wired
	// to the wrong pair, nothing below is refused.
	cfg.AuthRateLimit, cfg.AuthRateLimitBurst = 100000, 100000
	cfg.TrialRateLimit, cfg.TrialRateLimitBurst = 1, 2
	api := newAPITestWithConfig(t, cfg)

	body := func(n int) map[string]string {
		return map[string]string{
			"email":      fmt.Sprintf("burst%d@example.test", n),
			"tenantCode": fmt.Sprintf("burst%d", n),
			"industry":   "generic",
		}
	}

	for i := 1; i <= 2; i++ {
		res := api.do(http.MethodPost, "/api/v1/trial", "", body(i))
		if res.Status == http.StatusTooManyRequests {
			t.Fatalf("request %d of a burst of 2 was throttled", i)
		}
	}

	res := api.do(http.MethodPost, "/api/v1/trial", "", body(3))
	if res.Status != http.StatusTooManyRequests {
		t.Fatalf("the third trial request got %d %s, want 429 — the trial "+
			"endpoints are spending sign-in's budget rather than their own",
			res.Status, res.Code)
	}

	// And sign-in is untouched by it: one limiter per group, not one shared
	// bucket that either endpoint can empty for the other.
	signIn := api.do(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"identifier": "nobody", "password": "wrong"})
	if signIn.Status == http.StatusTooManyRequests {
		t.Error("a spent trial budget also refused a sign-in; the two groups " +
			"share a bucket")
	}
}
