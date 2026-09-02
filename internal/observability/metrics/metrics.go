// Package metrics owns ThinkPixelAG's private Prometheus registry and bounded
// metric vocabulary.
package metrics

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "thinkpixelag"

// BuildInfo contains immutable build labels. Empty values become "unknown".
type BuildInfo struct {
	Version  string
	Revision string
}

// Metrics is safe for concurrent use. When disabled, observations are no-ops
// and the registry remains empty.
type Metrics struct {
	enabled               bool
	registry              *prometheus.Registry
	httpRequests          *prometheus.CounterVec
	httpRequestDuration   *prometheus.HistogramVec
	databaseOperations    *prometheus.CounterVec
	databaseDuration      *prometheus.HistogramVec
	databaseHealth        prometheus.Gauge
	policyDecisions       *prometheus.CounterVec
	policyDuration        prometheus.Histogram
	outboxPending         prometheus.Gauge
	outboxOldest          prometheus.Gauge
	allocationOperations  *prometheus.CounterVec
	runAdmissions         *prometheus.CounterVec
	settlementLag         prometheus.Gauge
	cacheOperations       *prometheus.CounterVec
	revocationCollectors  []prometheus.Collector
	databasePoolCollector prometheus.Collector
}

type DatabasePoolSource func() (acquired, total, maximum int32)

type databasePoolCollector struct {
	source                        DatabasePoolSource
	acquired, total, maximum, use *prometheus.Desc
}

func newDatabasePoolCollector(source DatabasePoolSource) *databasePoolCollector {
	return &databasePoolCollector{source: source,
		acquired: prometheus.NewDesc(namespace+"_database_pool_acquired_connections", "Connections currently acquired from the PostgreSQL pool.", nil, nil),
		total:    prometheus.NewDesc(namespace+"_database_pool_connections", "Connections currently held by the PostgreSQL pool.", nil, nil),
		maximum:  prometheus.NewDesc(namespace+"_database_pool_max_connections", "Configured maximum PostgreSQL pool connections.", nil, nil),
		use:      prometheus.NewDesc(namespace+"_database_pool_utilization_ratio", "Fraction of configured PostgreSQL connections currently acquired.", nil, nil)}
}
func (c *databasePoolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.acquired, c.total, c.maximum, c.use} {
		ch <- desc
	}
}
func (c *databasePoolCollector) Collect(ch chan<- prometheus.Metric) {
	acquired, total, maximum := c.source()
	utilization := 0.0
	if maximum > 0 {
		utilization = float64(acquired) / float64(maximum)
	}
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(acquired))
	ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(total))
	ch <- prometheus.MustNewConstMetric(c.maximum, prometheus.GaugeValue, float64(maximum))
	ch <- prometheus.MustNewConstMetric(c.use, prometheus.GaugeValue, utilization)
}

// RevocationFreshnessSource supplies one consistent scrape-time aggregate.
// The callback shape keeps application state independent from Prometheus.
type RevocationFreshnessSource func() (time.Duration, int64, int, uint64, int, bool)

type revocationCollector struct {
	source                                    RevocationFreshnessSource
	age, lag, gaps, gapEvents, tenants, fresh *prometheus.Desc
}

func newRevocationCollector(source RevocationFreshnessSource) *revocationCollector {
	return &revocationCollector{
		source:    source,
		age:       prometheus.NewDesc(namespace+"_revocation_age_seconds", "Maximum monotonic age of tracked tenant revocation state.", nil, nil),
		lag:       prometheus.NewDesc(namespace+"_revocation_lag_entries", "Maximum known authoritative sequence lag across tracked tenants.", nil, nil),
		gaps:      prometheus.NewDesc(namespace+"_revocation_gaps", "Number of tracked tenants with an unresolved distribution gap.", nil, nil),
		gapEvents: prometheus.NewDesc(namespace+"_revocation_gap_events_total", "Total detected revocation distribution gap incidents.", nil, nil),
		tenants:   prometheus.NewDesc(namespace+"_revocation_tracked_tenants", "Number of tenant freshness states tracked by this process.", nil, nil),
		fresh:     prometheus.NewDesc(namespace+"_revocation_fresh", "Whether all tracked revocation state meets the configured readiness contract.", nil, nil),
	}
}

func (c *revocationCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.age, c.lag, c.gaps, c.gapEvents, c.tenants, c.fresh} {
		ch <- desc
	}
}

func (c *revocationCollector) Collect(ch chan<- prometheus.Metric) {
	age, lag, gaps, gapEvents, tenants, healthy := c.source()
	fresh := 0.0
	if healthy {
		fresh = 1
	}
	ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, age.Seconds())
	ch <- prometheus.MustNewConstMetric(c.lag, prometheus.GaugeValue, float64(lag))
	ch <- prometheus.MustNewConstMetric(c.gaps, prometheus.GaugeValue, float64(gaps))
	ch <- prometheus.MustNewConstMetric(c.gapEvents, prometheus.CounterValue, float64(gapEvents))
	ch <- prometheus.MustNewConstMetric(c.tenants, prometheus.GaugeValue, float64(tenants))
	ch <- prometheus.MustNewConstMetric(c.fresh, prometheus.GaugeValue, fresh)
}

// New creates an isolated registry. It never registers collectors globally.
func New(enabled bool, build BuildInfo) (*Metrics, error) {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{enabled: enabled, registry: registry}
	if !enabled {
		return metrics, nil
	}

	metrics.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total completed HTTP requests by bounded route, method, and status class.",
	}, []string{"route", "method", "status_class"})
	metrics.httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration by bounded route and method.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"route", "method"})
	metrics.databaseOperations = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Subsystem: "database", Name: "operations_total", Help: "Total PostgreSQL operations by bounded operation and outcome."}, []string{"operation", "outcome"})
	metrics.databaseDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Subsystem: "database", Name: "operation_duration_seconds", Help: "PostgreSQL operation duration by bounded operation.", Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"operation"})
	metrics.databaseHealth = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Subsystem: "database", Name: "healthy", Help: "Whether the most recent PostgreSQL health check succeeded."})
	metrics.policyDecisions = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Subsystem: "policy", Name: "decisions_total", Help: "Policy decisions by bounded outcome."}, []string{"outcome"})
	metrics.policyDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: namespace, Subsystem: "policy", Name: "decision_duration_seconds", Help: "OPA decision duration at the service boundary.", Buckets: []float64{.001, .005, .01, .025, .05, .075, .1, .25, .5, 1}})
	metrics.outboxPending = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Subsystem: "outbox", Name: "pending", Help: "Pending transactional outbox messages."})
	metrics.outboxOldest = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Subsystem: "outbox", Name: "oldest_seconds", Help: "Age of the oldest pending outbox message."})
	metrics.allocationOperations = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Subsystem: "allocation", Name: "operations_total", Help: "Resource allocation operations by bounded outcome."}, []string{"outcome"})
	metrics.runAdmissions = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Subsystem: "run", Name: "admissions_total", Help: "Run admissions by bounded outcome."}, []string{"outcome"})
	metrics.settlementLag = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Subsystem: "resource", Name: "settlement_lag_seconds", Help: "Oldest terminal resource settlement lag."})
	metrics.cacheOperations = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Subsystem: "cache", Name: "operations_total", Help: "Decision cache operations by bounded outcome."}, []string{"outcome"})
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information for the running ThinkPixelAG process.",
	}, []string{"version", "revision"})
	buildInfo.WithLabelValues(safeBuildLabel(build.Version), safeBuildLabel(build.Revision)).Set(1)

	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, err
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, err
	}
	if err := registry.Register(metrics.httpRequests); err != nil {
		return nil, err
	}
	if err := registry.Register(metrics.httpRequestDuration); err != nil {
		return nil, err
	}
	if err := registry.Register(metrics.databaseOperations); err != nil {
		return nil, err
	}
	if err := registry.Register(metrics.databaseDuration); err != nil {
		return nil, err
	}
	if err := registry.Register(metrics.databaseHealth); err != nil {
		return nil, err
	}
	for _, collector := range []prometheus.Collector{metrics.policyDecisions, metrics.policyDuration, metrics.outboxPending, metrics.outboxOldest, metrics.allocationOperations, metrics.runAdmissions, metrics.settlementLag, metrics.cacheOperations} {
		if err := registry.Register(collector); err != nil {
			return nil, err
		}
	}
	initializeOutcomeSeries(metrics.policyDecisions, "allow", "deny", "unavailable", "invalid")
	initializeOutcomeSeries(metrics.allocationOperations, "ok", "conflict", "denied", "error")
	initializeOutcomeSeries(metrics.runAdmissions, "admitted", "denied", "conflict", "error")
	initializeOutcomeSeries(metrics.cacheOperations, "hit", "miss", "invalid", "unavailable")
	if err := registry.Register(buildInfo); err != nil {
		return nil, err
	}
	return metrics, nil
}

func initializeOutcomeSeries(counter *prometheus.CounterVec, outcomes ...string) {
	for _, outcome := range outcomes {
		counter.WithLabelValues(outcome)
	}
}

// ObservePolicyDecision records only the closed decision outcome vocabulary.
func (m *Metrics) ObservePolicyDecision(outcome string) {
	m.observeOutcome(m.policyDecisions, outcome, "allow", "deny", "unavailable", "invalid")
}

// ObservePolicyDuration records OPA boundary time without policy, tenant, or
// action labels. Outcome is reported separately through ObservePolicyDecision.
func (m *Metrics) ObservePolicyDuration(duration time.Duration) {
	if !m.enabled {
		return
	}
	if duration < 0 {
		duration = 0
	}
	m.policyDuration.Observe(duration.Seconds())
}

// SetOutbox reports bounded backlog state without message or tenant labels.
func (m *Metrics) SetOutbox(pending int, oldest time.Duration) {
	if !m.enabled {
		return
	}
	if pending < 0 {
		pending = 0
	}
	if oldest < 0 {
		oldest = 0
	}
	m.outboxPending.Set(float64(pending))
	m.outboxOldest.Set(oldest.Seconds())
}

func (m *Metrics) ObserveAllocation(outcome string) {
	m.observeOutcome(m.allocationOperations, outcome, "ok", "conflict", "denied", "error")
}
func (m *Metrics) ObserveRunAdmission(outcome string) {
	m.observeOutcome(m.runAdmissions, outcome, "admitted", "denied", "conflict", "error")
}
func (m *Metrics) ObserveCache(outcome string) {
	m.observeOutcome(m.cacheOperations, outcome, "hit", "miss", "invalid", "unavailable")
}
func (m *Metrics) SetSettlementLag(lag time.Duration) {
	if m.enabled {
		if lag < 0 {
			lag = 0
		}
		m.settlementLag.Set(lag.Seconds())
	}
}

func (m *Metrics) observeOutcome(counter *prometheus.CounterVec, outcome string, allowed ...string) {
	if !m.enabled {
		return
	}
	valid := false
	for _, candidate := range allowed {
		if outcome == candidate {
			valid = true
			break
		}
	}
	if !valid {
		outcome = "error"
	}
	counter.WithLabelValues(outcome).Inc()
}

// ObserveDatabase records bounded adapter telemetry; callers must never pass SQL text.
func (m *Metrics) ObserveDatabase(operation, outcome string, duration time.Duration) {
	if !m.enabled {
		return
	}
	switch operation {
	case "query", "exec", "batch", "copy", "connect", "health":
	default:
		operation = "other"
	}
	switch outcome {
	case "ok", "canceled", "timeout", "constraint", "conflict", "unavailable", "error":
	default:
		outcome = "error"
	}
	if duration < 0 {
		duration = 0
	}
	m.databaseOperations.WithLabelValues(operation, outcome).Inc()
	m.databaseDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *Metrics) SetDatabaseHealthy(healthy bool) {
	if !m.enabled {
		return
	}
	if healthy {
		m.databaseHealth.Set(1)
	} else {
		m.databaseHealth.Set(0)
	}
}

// RegisterRevocationFreshness adds bounded-cardinality revocation telemetry to
// this process registry. It must be called at most once during assembly.
func (m *Metrics) RegisterRevocationFreshness(source RevocationFreshnessSource) error {
	if source == nil {
		return errors.New("revocation freshness metrics require a source")
	}
	if !m.enabled {
		return nil
	}
	if len(m.revocationCollectors) != 0 {
		return errors.New("revocation freshness metrics already registered")
	}
	metricCollectors := []prometheus.Collector{newRevocationCollector(source)}
	for _, collector := range metricCollectors {
		if err := m.registry.Register(collector); err != nil {
			return err
		}
	}
	m.revocationCollectors = metricCollectors
	return nil
}

// RegisterDatabasePool exposes bounded pool saturation without connection or
// query identifiers. It must be called at most once during process assembly.
func (m *Metrics) RegisterDatabasePool(source DatabasePoolSource) error {
	if source == nil {
		return errors.New("database pool metrics require a source")
	}
	if !m.enabled {
		return nil
	}
	if m.databasePoolCollector != nil {
		return errors.New("database pool metrics already registered")
	}
	collector := newDatabasePoolCollector(source)
	if err := m.registry.Register(collector); err != nil {
		return err
	}
	m.databasePoolCollector = collector
	return nil
}

// Enabled reports whether application and runtime metrics are collected.
func (m *Metrics) Enabled() bool { return m.enabled }

// Registry exposes the private gatherer for tests and deployment integration.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// Handler returns the Prometheus exposition handler. ENG-007 mounts it on the
// operational endpoint; it is deliberately not registered on a global mux.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}

// ObserveHTTP records one completed request. Route must be a stable route
// template, never a raw path or resource identifier.
func (m *Metrics) ObserveHTTP(route, method string, status int, duration time.Duration) {
	if !m.enabled {
		return
	}
	route = boundedRoute(route)
	method = boundedMethod(method)
	if duration < 0 {
		duration = 0
	}
	m.httpRequests.WithLabelValues(route, method, statusClass(status)).Inc()
	m.httpRequestDuration.WithLabelValues(route, method).Observe(duration.Seconds())
}

func boundedRoute(route string) string {
	if route == "" || len(route) > 128 || strings.ContainsAny(route, "?#\r\n") {
		return "unknown"
	}
	switch route {
	case "/livez", "/readyz", "/metrics",
		"/v1/agents", "/v1/agents/{agent_id}", "/v1/agents/{agent_id}/runs",
		"/v1/runs/{run_id}", "/v1/runs/{run_id}/signals", "/v1/runs/{run_id}/cancel", "/v1/runs/{run_id}/events",
		"/v1/admin/agents", "/v1/admin/agents/{agent_id}/versions", "/v1/admin/agents/{agent_id}/versions/{version_digest}/approvals",
		"/v1/admin/policies", "/v1/admin/policies/{policy_digest}/activations",
		"/v1/trusted/runs/{run_id}/usage", "/v1/trusted/reservations/{reservation_id}/settle",
		"/v1/admin/runs/{run_id}/resource-extensions", "/v1/admin/revocations", "/v1/admin/revocations/{revocation_id}/lift",
		"/v1/trusted/revocations/events", "/v1/trusted/revocations/reconcile":
		return route
	default:
		return "unknown"
	}
}

func boundedMethod(method string) string {
	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "unknown"
	}
	return strconv.Itoa(status/100) + "xx"
}

func safeBuildLabel(value string) string {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "=/?#&;\r\n\t ") {
		return "unknown"
	}
	return value
}
