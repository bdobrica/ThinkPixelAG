// Package metrics owns ThinkPixelAG's private Prometheus registry and bounded
// metric vocabulary.
package metrics

import (
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
	enabled             bool
	registry            *prometheus.Registry
	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
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
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information for the running ThinkPixelAG process.",
	}, []string{"version", "revision"})
	buildInfo.WithLabelValues(nonEmpty(build.Version), nonEmpty(build.Revision)).Set(1)

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
	if err := registry.Register(buildInfo); err != nil {
		return nil, err
	}
	return metrics, nil
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

func nonEmpty(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
