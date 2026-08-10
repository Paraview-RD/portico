package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Paraview-RD/portico/internal/store"
)

// MetricsHandler serves the Prometheus endpoint.
//
// Returned rather than mounted on the main router: this must listen
// somewhere else. See config.MetricsAddr for why, and cmd/server for the
// second listener.
func (s *Server) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(s.metrics, promhttp.HandlerOpts{
		// A collector that fails should say so in the response rather than
		// silently publishing a partial scrape that reads as "everything is
		// fine and some numbers are missing".
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))
	// Anything else on this port is a misdirected request — most likely
	// somebody pointing a browser at it expecting the application.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "This port serves /metrics only.", http.StatusNotFound)
	})
	return mux
}

// databaseCollector publishes the connection pool's own counters.
//
// A collector rather than gauges updated on a timer: these numbers already
// exist inside database/sql and are exact at the moment they are read, so
// sampling them on a schedule would add a delay and a goroutine to produce
// something less accurate.
//
// Pool exhaustion is the failure this exists to make visible. It looks like
// slowness spread evenly across every endpoint, with no error anywhere and
// no single slow query to find — `portico_db_connections_wait_total`
// climbing is what names it.
type databaseCollector struct {
	store *store.Store

	inUse       *prometheus.Desc
	idle        *prometheus.Desc
	waitCount   *prometheus.Desc
	waitSeconds *prometheus.Desc
	maxOpen     *prometheus.Desc
}

func newDatabaseCollector(st *store.Store) *databaseCollector {
	return &databaseCollector{
		store: st,
		inUse: prometheus.NewDesc(
			"portico_db_connections_in_use",
			"Database connections currently in use.", nil, nil),
		idle: prometheus.NewDesc(
			"portico_db_connections_idle",
			"Database connections currently idle.", nil, nil),
		waitCount: prometheus.NewDesc(
			"portico_db_connections_wait_total",
			"Total number of times a caller waited for a connection.", nil, nil),
		waitSeconds: prometheus.NewDesc(
			"portico_db_connections_wait_seconds_total",
			"Total time spent waiting for a connection.", nil, nil),
		maxOpen: prometheus.NewDesc(
			"portico_db_connections_max_open",
			"Configured maximum number of open connections.", nil, nil),
	}
}

func (c *databaseCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.inUse
	ch <- c.idle
	ch <- c.waitCount
	ch <- c.waitSeconds
	ch <- c.maxOpen
}

func (c *databaseCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.store.DB().Stats()
	ch <- prometheus.MustNewConstMetric(
		c.inUse, prometheus.GaugeValue, float64(stats.InUse))
	ch <- prometheus.MustNewConstMetric(
		c.idle, prometheus.GaugeValue, float64(stats.Idle))
	ch <- prometheus.MustNewConstMetric(
		c.waitCount, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(
		c.waitSeconds, prometheus.CounterValue, stats.WaitDuration.Seconds())
	ch <- prometheus.MustNewConstMetric(
		c.maxOpen, prometheus.GaugeValue, float64(stats.MaxOpenConnections))
}
