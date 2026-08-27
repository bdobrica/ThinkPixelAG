package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	logpipeline "github.com/bdobrica/ThinkPixelAG/internal/observability/logging"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/tracing"
)

type staticAdditionalReadiness struct{ err error }

func (p staticAdditionalReadiness) Ready(context.Context) error { return p.err }

func TestComposeReadinessIncludesSecurityProbe(t *testing.T) {
	primary := &Readiness{}
	securityErr := errors.New("revocation state is stale")
	probe := ComposeReadiness(primary, staticAdditionalReadiness{err: securityErr})
	probe.MarkReady()
	if err := probe.Ready(context.Background()); !errors.Is(err, securityErr) {
		t.Fatalf("composed readiness error = %v", err)
	}
	probe.MarkNotReady()
	if err := primary.Ready(context.Background()); err == nil {
		t.Fatal("primary readiness lifecycle was not cleared")
	}
}

const fixedRequestID = "019feba6-b9bb-7fff-bfff-ffffffffffff"

func testDependencies(t *testing.T, logs *bytes.Buffer, enabledMetrics bool) Dependencies {
	t.Helper()
	logger, err := logpipeline.New(logs, "debug")
	if err != nil {
		t.Fatal(err)
	}
	metricSet, err := metrics.New(enabledMetrics, metrics.BuildInfo{Version: "test", Revision: "test"})
	if err != nil {
		t.Fatal(err)
	}
	traceSet, err := tracing.New(context.Background(), tracing.Config{Mode: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	return Dependencies{Logger: logger, Metrics: metricSet, Tracing: traceSet, NewID: func() (string, error) { return fixedRequestID, nil }}
}

func testConfig() config.HTTPConfig {
	value := config.Defaults().HTTP
	value.Address = "127.0.0.1:8080"
	return value
}

func TestHealthMetricsAndUnknownRoutes(t *testing.T) {
	var logs bytes.Buffer
	dependencies := testDependencies(t, &logs, true)
	readiness := &Readiness{}
	readiness.MarkReady()
	handler := newHandler(testConfig(), dependencies, readiness)

	for _, test := range []struct {
		path        string
		status      int
		contentType string
	}{
		{"/livez", http.StatusOK, "application/json"},
		{"/readyz", http.StatusOK, "application/json"},
		{"/metrics", http.StatusOK, "text/plain"},
		{"/missing?secret=value", http.StatusNotFound, "application/problem+json"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Errorf("GET %s status = %d", test.path, recorder.Code)
		}
		if !strings.HasPrefix(recorder.Header().Get("Content-Type"), test.contentType) {
			t.Errorf("GET %s content type = %q", test.path, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("X-Request-ID") != fixedRequestID {
			t.Errorf("GET %s request ID missing", test.path)
		}
	}
	if strings.Contains(logs.String(), "secret=value") {
		t.Fatal("raw path or query leaked into logs")
	}
}

func TestReadinessMethodAndRequestIDHandling(t *testing.T) {
	var logs bytes.Buffer
	dependencies := testDependencies(t, &logs, false)
	readiness := &Readiness{}
	handler := newHandler(testConfig(), dependencies, readiness)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	request.Header.Set("X-Request-ID", "attacker controlled\nvalue")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("X-Request-ID") != fixedRequestID {
		t.Fatalf("not ready response = %d, %q", recorder.Code, recorder.Header().Get("X-Request-ID"))
	}
	var problem Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "not_ready" || problem.RequestID != fixedRequestID {
		t.Fatalf("problem = %#v", problem)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/livez", nil)
	request.Header.Set("X-Request-ID", fixedRequestID)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("method response = %d, %q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestBodyLimitAndDecodeJSON(t *testing.T) {
	var logs bytes.Buffer
	dependencies := testDependencies(t, &logs, false)
	settings := testConfig()
	settings.MaxBodyBytes = 16
	decode := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var value map[string]string
		if err := DecodeJSON(request, &value); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		writeJSON(writer, http.StatusOK, value)
	})
	handler := middleware("unknown", settings, dependencies, decode)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/decode", strings.NewReader(`{"value":"this is too long"}`))
	request.ContentLength = -1
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "configured limit") {
		t.Fatalf("chunked limit response = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/decode", strings.NewReader(strings.Repeat("x", 17)))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("content-length limit response = %d", recorder.Code)
	}
}

func TestPanicRecoveryCorrelationMetricsAndDeadline(t *testing.T) {
	var logs bytes.Buffer
	dependencies := testDependencies(t, &logs, true)
	settings := testConfig()
	settings.HandlerTimeout = 50 * time.Millisecond
	panicHandler := middleware("/livez", settings, dependencies, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("sentinel secret panic") }))
	recorder := httptest.NewRecorder()
	panicHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String()+logs.String(), "sentinel secret panic") {
		t.Fatalf("unsafe panic response/log: %d %s %s", recorder.Code, recorder.Body.String(), logs.String())
	}
	if !strings.Contains(logs.String(), `"request_id":"`+fixedRequestID+`"`) {
		t.Fatalf("panic/request logs lack correlation: %s", logs.String())
	}

	deadlineSeen := make(chan struct{}, 1)
	deadlineHandler := middleware("/livez", settings, dependencies, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := request.Context().Deadline(); ok {
			deadlineSeen <- struct{}{}
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	}))
	deadlineHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/livez", nil))
	select {
	case <-deadlineSeen:
	default:
		t.Fatal("handler deadline missing")
	}

	families, err := dependencies.Metrics.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, family := range families {
		if family.GetName() == "thinkpixelag_http_requests_total" {
			found = true
		}
	}
	if !found {
		t.Fatal("HTTP request metric missing")
	}

	traceRequest := httptest.NewRequest(http.MethodGet, "/livez", nil)
	traceRequest.Method = "SENTINEL_SECRET_METHOD"
	traceRequest.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	deadlineHandler.ServeHTTP(httptest.NewRecorder(), traceRequest)
	if !strings.Contains(logs.String(), `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`) {
		t.Fatalf("trace correlation missing: %s", logs.String())
	}
	if strings.Contains(logs.String(), "SENTINEL_SECRET_METHOD") || !strings.Contains(logs.String(), `"method":"OTHER"`) {
		t.Fatalf("unbounded method leaked: %s", logs.String())
	}
}

func TestProblemMappingAndJSONStrictness(t *testing.T) {
	cause := errors.New("database secret")
	problem := ProblemFromError(domain.WrapError(domain.CodeUnavailable, "temporarily unavailable", cause).WithRetryable())
	if problem.Status != http.StatusServiceUnavailable || problem.Code != "unavailable" || problem.RetryAfterSeconds == nil || strings.Contains(problem.Detail, cause.Error()) {
		t.Fatalf("problem = %#v", problem)
	}
	internal := ProblemFromError(cause)
	if internal.Code != "internal" || strings.Contains(internal.Detail, cause.Error()) {
		t.Fatalf("internal problem = %#v", internal)
	}
	typedInternal := ProblemFromError(domain.NewError(domain.CodeInternal, "sentinel secret"))
	if strings.Contains(typedInternal.Detail, "sentinel") {
		t.Fatalf("typed internal leaked: %#v", typedInternal)
	}

	request := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(`{"known":"yes"} {}`)))
	var value struct {
		Known string `json:"known"`
	}
	if err := DecodeJSON(request, &value); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestNewRejectsMissingDependenciesAndAppliesLimits(t *testing.T) {
	if _, err := New(testConfig(), Dependencies{}); err == nil {
		t.Fatal("expected dependency error")
	}
	var logs bytes.Buffer
	server, err := New(testConfig(), testDependencies(t, &logs, false))
	if err != nil {
		t.Fatal(err)
	}
	if server.http.MaxHeaderBytes != testConfig().MaxHeaderBytes || server.http.ReadHeaderTimeout != testConfig().ReadHeaderTimeout {
		t.Fatalf("server limits not applied: %#v", server.http)
	}
}

func TestServeReadinessAndGracefulShutdown(t *testing.T) {
	var logs bytes.Buffer
	server, err := New(testConfig(), testDependencies(t, &logs, false))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	deadline := time.Now().Add(time.Second)
	for server.readiness == nil || server.readiness.Ready(context.Background()) != nil {
		if time.Now().After(deadline) {
			t.Fatal("server never became ready")
		}
		time.Sleep(time.Millisecond)
	}
	response, err := http.Get("http://" + listener.Addr().String() + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if server.readiness.Ready(context.Background()) == nil {
		t.Fatal("server remained ready after shutdown")
	}
}
