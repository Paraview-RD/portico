package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The metrics endpoint is the one surface of this application that is
// unauthenticated by design, so these tests are mostly about where it is and
// what it does not say.

// scrape reads the metrics endpoint through its own handler, which is where
// it lives — deliberately not through the application router.
func (a *apiTest) scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	a.srv.MetricsHandler().ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape returned %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricsAreNotOnTheApplicationPort(t *testing.T) {
	api := newAPITest(t)

	// The entire security model for /metrics is that it lives somewhere
	// else — it is unauthenticated, because no Prometheus deployment
	// authenticates. If it ever appeared on the application router, anyone
	// publishing that router would publish the metrics with it, and nothing
	// about the scrape config would look wrong.
	//
	// The assertion is on the body rather than the status. `/metrics` is not
	// under /api/, so it falls through to the single-page app and returns
	// 200 with an HTML shell — which is not a leak, and which a 404 check
	// would have called a failure while a real leak also returned 200.
	for _, path := range []string{"/metrics", "/api/v1/metrics"} {
		rec := httptest.NewRecorder()
		api.srv.Handler().ServeHTTP(
			rec, httptest.NewRequest(http.MethodGet, path, nil))

		if strings.Contains(rec.Body.String(), "portico_http_requests_total") {
			t.Errorf("GET %s on the application port served metrics", path)
		}
	}
}

func TestMetricsPublishesWhatAnOperatorNeeds(t *testing.T) {
	api := newAPITest(t)

	// Something to count.
	api.do(http.MethodGet, "/api/v1/health", "", nil)

	body := api.scrape(t)
	for _, want := range []string{
		"portico_http_requests_total",
		"portico_http_request_duration_seconds",
		"portico_sign_in_attempts_total",
		"portico_account_lockouts_total",
		// The pool counters name the failure that looks like nothing else:
		// everything slow, no errors, and no single slow query to find.
		"portico_db_connections_in_use",
		"portico_db_connections_wait_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape does not publish %s", want)
		}
	}
}

func TestSignInCountersExistBeforeAnybodySignsIn(t *testing.T) {
	api := newAPITest(t)
	body := api.scrape(t)

	// Without this, rate() over a window with no failed sign-ins returns
	// nothing rather than zero, so an alert on "failed sign-ins are
	// climbing" cannot tell a quiet instance from one that is not reporting.
	// Initialising the series is what makes the absence of attacks visible.
	for _, want := range []string{
		`portico_sign_in_attempts_total{outcome="success"} 0`,
		`portico_sign_in_attempts_total{outcome="bad_credentials"} 0`,
		`portico_sign_in_attempts_total{outcome="locked"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected this series initialised to zero: %s", want)
		}
	}
}

func TestSignInOutcomesAreCounted(t *testing.T) {
	api := newAPITest(t)

	api.login(adminUsername, adminPassword)
	api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": adminUsername,
		"password":   "not-the-password",
	})

	body := api.scrape(t)
	if !strings.Contains(body, `portico_sign_in_attempts_total{outcome="success"} 1`) {
		t.Error("a successful sign-in was not counted")
	}
	if !strings.Contains(body, `portico_sign_in_attempts_total{outcome="bad_credentials"} 1`) {
		t.Error("a failed sign-in was not counted")
	}
	if !strings.Contains(body, `portico_tokens_issued_total{kind="session"} 1`) {
		t.Error("an issued session token was not counted")
	}
}

func TestMetricsPortServesNothingElse(t *testing.T) {
	api := newAPITest(t)

	rec := httptest.NewRecorder()
	api.srv.MetricsHandler().ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("the metrics port answered an application path with %d; "+
			"it serves /metrics and nothing else", rec.Code)
	}
}

func TestRequestMetricsUseTheRoutePatternNotThePath(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Two requests for different ids must produce one series. Labelling by
	// path would let anyone holding a token create unbounded series from
	// outside — a denial of service against the monitoring, not untidiness.
	api.do(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000001", token, nil)
	api.do(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000002", token, nil)

	body := api.scrape(t)
	if !strings.Contains(body, `route="/api/v1/users/{id}"`) {
		t.Error("expected the route pattern as the label value")
	}
	if strings.Contains(body, "00000000-0000-0000-0000-000000000001") {
		t.Error("a request path reached a metric label; " +
			"this is how a label set grows without bound")
	}
}

func TestUnmatchedPathsDoNotBecomeLabels(t *testing.T) {
	api := newAPITest(t)

	// The raw path of a 404 is exactly what an unauthenticated caller
	// controls, so this is the label that would grow without bound: a few
	// thousand requests for random URLs and the metrics endpoint becomes the
	// largest thing this process produces.
	//
	// Not asserting which pattern they collapse into — chi supplies a
	// wildcard and that is its business. Asserting that the path is not in
	// there is the property that matters, and it stays true if the router
	// changes how it reports a miss.
	for _, path := range []string{
		"/api/v1/no-such-thing-aaaa",
		"/api/v1/no-such-thing-bbbb",
		"/some-ui-path-cccc",
	} {
		rec := httptest.NewRecorder()
		api.srv.Handler().ServeHTTP(
			rec, httptest.NewRequest(http.MethodGet, path, nil))
	}

	body := api.scrape(t)
	for _, leaked := range []string{"no-such-thing-aaaa", "no-such-thing-bbbb", "some-ui-path-cccc"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the request path %q reached a metric label", leaked)
		}
	}
}
