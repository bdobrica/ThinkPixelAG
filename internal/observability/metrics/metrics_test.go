package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRevocationFreshnessMetrics(t *testing.T) {
	metricSet, err := New(true, BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	err = metricSet.RegisterRevocationFreshness(func() (time.Duration, int64, int, uint64, int, bool) { return 12 * time.Second, 3, 1, 4, 2, false })
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	metricSet.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{"thinkpixelag_revocation_age_seconds 12", "thinkpixelag_revocation_lag_entries 3", "thinkpixelag_revocation_gaps 1", "thinkpixelag_revocation_gap_events_total 4", "thinkpixelag_revocation_tracked_tenants 2", "thinkpixelag_revocation_fresh 0"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
	if err = metricSet.RegisterRevocationFreshness(func() (time.Duration, int64, int, uint64, int, bool) { return 0, 0, 0, 0, 0, true }); err == nil {
		t.Fatal("accepted duplicate revocation collector")
	}
}

func TestEnabledMetrics(t *testing.T) {
	t.Parallel()
	metrics, err := New(true, BuildInfo{Version: "v0.1.0", Revision: "abc123"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	metrics.ObserveHTTP("/v1/runs/{run_id}", "get", 200, 25*time.Millisecond)
	metrics.ObserveDatabase("query", "ok", 10*time.Millisecond)
	metrics.SetDatabaseHealthy(true)

	request := httptest.NewRequest("GET", "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != 200 {
		t.Fatalf("metrics status = %d, body = %s", response.Code, body)
	}
	for _, want := range []string{
		`thinkpixelag_build_info{revision="abc123",version="v0.1.0"} 1`,
		`thinkpixelag_http_requests_total{method="GET",route="/v1/runs/{run_id}",status_class="2xx"} 1`,
		`thinkpixelag_http_request_duration_seconds_count{method="GET",route="/v1/runs/{run_id}"} 1`,
		`thinkpixelag_database_operations_total{operation="query",outcome="ok"} 1`,
		`thinkpixelag_database_operation_duration_seconds_count{operation="query"} 1`,
		`thinkpixelag_database_healthy 1`,
		"go_goroutines",
		"process_cpu_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestDisabledMetricsAreNoop(t *testing.T) {
	t.Parallel()
	metrics, err := New(false, BuildInfo{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	metrics.ObserveHTTP("/v1/agents", "GET", 200, time.Second)
	if metrics.Enabled() {
		t.Fatal("Enabled() = true")
	}
	families, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if len(families) != 0 {
		t.Fatalf("disabled registry has %d metric families", len(families))
	}
}

func TestLabelsFailToBoundedValues(t *testing.T) {
	t.Parallel()
	metrics, err := New(true, BuildInfo{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	metrics.ObserveHTTP("/v1/runs/secret", "invented", 999, -time.Second)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatalf("read metrics response: %v", err)
	}
	output := string(body)
	if strings.Contains(output, "/v1/runs/secret") {
		t.Fatalf("unbounded route leaked into labels: %s", output)
	}
	for _, want := range []string{`method="OTHER"`, `route="unknown"`, `status_class="unknown"`} {
		if !strings.Contains(output, want) {
			t.Errorf("metrics output missing bounded label %q", want)
		}
	}
}

func TestStatusClass(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]string{99: "unknown", 100: "1xx", 204: "2xx", 302: "3xx", 404: "4xx", 503: "5xx", 600: "unknown"} {
		if got := statusClass(status); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", status, got, want)
		}
	}
}
