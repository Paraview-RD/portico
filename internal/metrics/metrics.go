// Package metrics exposes what an operator needs to answer "is this
// instance healthy, and is anybody failing to sign in".
//
// Two decisions shape everything here, and both are about cardinality,
// because a metrics endpoint that degrades the process it measures is worse
// than no metrics at all.
//
// Routes are labelled with the chi route pattern, not the request path.
// `/api/v1/users/{id}` is one series; the path would be one series per user
// and would grow without bound from the outside.
//
// Nothing is labelled with a tenant. A deployment may have many, an operator
// wanting per-tenant figures can get them from the audit trail, and a label
// whose values are created by the people being measured is the same
// unbounded-growth problem wearing a different hat.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry is the set of collectors this process publishes.
//
// A registry of its own rather than the default one: the default is global
// and anything linked into the binary can register into it, which makes the
// published set a property of the dependency tree rather than a decision.
type Registry struct {
	*prometheus.Registry

	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	inFlight        prometheus.Gauge

	signIns  *prometheus.CounterVec
	lockouts prometheus.Counter
	tokens   *prometheus.CounterVec

	trialTenants prometheus.Gauge
	trialQuota   prometheus.Gauge
}

// New builds a registry with the process and Go collectors plus this
// application's own.
func New() *Registry {
	reg := prometheus.NewRegistry()

	m := &Registry{
		Registry: reg,

		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "portico_http_requests_total",
			Help: "HTTP requests by method, route pattern, and status code.",
		}, []string{"method", "route", "status"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "portico_http_request_duration_seconds",
			Help: "HTTP request duration by method and route pattern.",
			// The default buckets top out at 10s and start at 5ms. Sign-in
			// costs a deliberate bcrypt evaluation, so the interesting range
			// here is tens to hundreds of milliseconds, and a bucket
			// boundary near a second is what tells an operator the work
			// factor is set too high for their hardware.
			Buckets: []float64{
				0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
			},
		}, []string{"method", "route"}),

		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "portico_http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),

		signIns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "portico_sign_in_attempts_total",
			Help: "Sign-in attempts by outcome.",
		}, []string{"outcome"}),

		lockouts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "portico_account_lockouts_total",
			Help: "Accounts locked after repeated failed sign-ins.",
		}),

		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "portico_tokens_issued_total",
			Help: "Tokens issued, by kind.",
		}, []string{"kind"}),

		// Two gauges rather than one ratio, so an alert can be written on
		// either the headroom or the ceiling without the other having to be
		// guessed. Zero on a deployment with no trials, which is most of them.
		//
		// This exists because a demonstration can close itself silently. The
		// quota counts confirmed trial requests; when it is reached, every new
		// visitor is refused, and nothing about that is an error — no log line
		// worth reading, nothing on any screen. It is the one failure here
		// that looks exactly like nobody being interested.
		trialTenants: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "portico_trial_tenants",
			Help: "Trial tenants that currently exist.",
		}),
		trialQuota: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "portico_trial_tenants_max",
			Help: "How many trial tenants may exist at once. Zero when self-service trials are off.",
		}),
	}

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.requestDuration, m.inFlight,
		m.signIns, m.lockouts, m.tokens,
		m.trialTenants, m.trialQuota,
	)

	// Registered with zero rather than created on first use. A counter that
	// does not exist until it happens makes `rate(...[5m])` return nothing
	// instead of zero, so an alert on "failed sign-ins are climbing" cannot
	// distinguish a healthy instance from one that has not reported yet.
	for _, outcome := range []string{
		OutcomeSuccess, OutcomeBadCredentials, OutcomeLocked,
		OutcomeDisabled, OutcomeExpired, OutcomeChangeRequired, OutcomeError,
	} {
		m.signIns.WithLabelValues(outcome)
	}
	m.lockouts.Add(0)

	return m
}

// ObserveTrialQuota records how much of the trial allowance is used.
//
// Called from the sweep rather than on every request: the number changes when
// a tenant is created or reclaimed, and reading it costs a count over
// trial_requests, which is not worth doing on a page load.
func (m *Registry) ObserveTrialQuota(existing, allowed int) {
	if m == nil {
		return
	}
	m.trialTenants.Set(float64(existing))
	m.trialQuota.Set(float64(allowed))
}

// Sign-in outcomes. Deliberately coarser than the reasons the code
// distinguishes internally: an unknown username and a wrong password are one
// value here, because they are one value to the person signing in and
// separating them in a public metric would undo the work sign-in does to
// keep them indistinguishable.
const (
	OutcomeSuccess        = "success"
	OutcomeBadCredentials = "bad_credentials"
	OutcomeLocked         = "locked"
	OutcomeDisabled       = "disabled"
	// Its own value rather than folded into bad_credentials: somebody whose
	// password expired typed the right one, and an operator who has just
	// enabled expiry needs to see how many people that is without it looking
	// like an attack.
	OutcomeExpired = "password_expired"
	// Somebody signed in with the default password a release bootstraps its
	// first administrator with. Separate from password_expired because it
	// answers a different question, and one worth an alert: on a deployment
	// past its first day this counting up means the default is still in
	// place and being found.
	OutcomeChangeRequired = "password_change_required"
	// The attempt could not be judged — the database was unreachable, or
	// something else failed before an outcome existed. Counted rather than
	// dropped, so that "sign-ins stopped" is visibly different from
	// "sign-ins started failing".
	OutcomeError = "error"
)

// Token kinds.
const (
	TokenSession = "session"
	TokenAccess  = "access"
	TokenRefresh = "refresh"
	TokenID      = "id"
)

// RecordSignIn counts one sign-in attempt.
func (m *Registry) RecordSignIn(outcome string) {
	if m == nil {
		return
	}
	m.signIns.WithLabelValues(outcome).Inc()
}

// RecordLockout counts one account reaching the lockout threshold.
func (m *Registry) RecordLockout() {
	if m == nil {
		return
	}
	m.lockouts.Inc()
}

// RecordTokenIssued counts one issued token of the given kind.
func (m *Registry) RecordTokenIssued(kind string) {
	if m == nil {
		return
	}
	m.tokens.WithLabelValues(kind).Inc()
}
