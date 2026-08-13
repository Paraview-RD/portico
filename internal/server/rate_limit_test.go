package server_test

import (
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
