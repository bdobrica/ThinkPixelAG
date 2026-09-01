package tracing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNoopTracing(t *testing.T) {
	t.Parallel()
	tracing, err := New(context.Background(), Config{Mode: "noop"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, span := tracing.Tracer().Start(context.Background(), "noop")
	if span.IsRecording() {
		t.Error("no-op span is recording")
	}
	span.End()
	if err := tracing.ForceFlush(context.Background()); err != nil {
		t.Errorf("ForceFlush() error = %v", err)
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown() error = %v", err)
	}
}

func TestTracerDropsRestrictedNamesAttributesEventsAndErrors(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracing := &Tracing{Provider: provider}
	_, span := tracing.Tracer().Start(context.Background(), "POST /runs?token=trace-name-secret", trace.WithAttributes(attribute.String("authorization", "start-secret")))
	span.SetAttributes(attribute.String("objective", "attribute-secret"), attribute.String("db.system.name", "postgresql"))
	span.AddEvent("payload-event-secret", trace.WithAttributes(attribute.String("access_token", "event-secret")))
	span.RecordError(errors.New("recorded-error-secret"))
	span.End()
	finished := recorder.Ended()
	if len(finished) != 1 || finished[0].Name() != "operation" {
		t.Fatalf("finished spans=%v", finished)
	}
	serialized := finished[0].Name()
	for _, value := range finished[0].Attributes() {
		serialized += fmt.Sprint(value)
	}
	for _, event := range finished[0].Events() {
		serialized += event.Name + fmt.Sprint(event.Attributes)
	}
	for _, sentinel := range []string{"trace-name-secret", "start-secret", "attribute-secret", "payload-event-secret", "event-secret", "recorded-error-secret"} {
		if strings.Contains(serialized, sentinel) {
			t.Fatalf("trace leaked %q: %s", sentinel, serialized)
		}
	}
	if !strings.Contains(serialized, "postgresql") {
		t.Fatalf("allowlisted trace attribute missing: %s", serialized)
	}
}

func TestOTLPTracingExportsAndShutsDown(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/traces" {
			t.Errorf("export path = %q, want /v1/traces", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("content type = %q", request.Header.Get("Content-Type"))
		}
		requests.Add(1)
		response.Header().Set("Content-Type", "application/x-protobuf")
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tracing, err := New(context.Background(), Config{
		Mode:          "otlp",
		ServiceName:   "thinkpixelag-test",
		Environment:   "test",
		OTLPEndpoint:  server.URL,
		SampleRatio:   1,
		ExportTimeout: 2 * time.Second,
		BatchTimeout:  time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, span := tracing.Tracer().Start(context.Background(), "exported")
	if !span.IsRecording() {
		t.Fatal("OTLP span is not recording")
	}
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tracing.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("export requests = %d, want 1", requests.Load())
	}
	if err := tracing.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestPropagation(t *testing.T) {
	t.Parallel()
	tracing, err := New(context.Background(), Config{Mode: "noop"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanContext)
	carrier := propagation.MapCarrier{}
	tracing.Propagator.Inject(ctx, carrier)
	extracted := trace.SpanContextFromContext(tracing.Propagator.Extract(context.Background(), carrier))
	if extracted.TraceID() != spanContext.TraceID() || extracted.SpanID() != spanContext.SpanID() || !extracted.IsRemote() {
		t.Fatalf("extracted span context = %#v, want %#v", extracted, spanContext)
	}
}

func TestInvalidTracingConfig(t *testing.T) {
	t.Parallel()
	tests := []Config{
		{Mode: "stdout"},
		{Mode: "otlp", OTLPEndpoint: "://bad", ServiceName: "service", SampleRatio: 1, ExportTimeout: time.Second, BatchTimeout: time.Second},
		{Mode: "otlp", OTLPEndpoint: "https://collector.example?token=secret", ServiceName: "service", SampleRatio: 1, ExportTimeout: time.Second, BatchTimeout: time.Second},
		{Mode: "otlp", OTLPEndpoint: "https://collector.example", SampleRatio: 1, ExportTimeout: time.Second, BatchTimeout: time.Second},
		{Mode: "otlp", OTLPEndpoint: "https://collector.example", ServiceName: "service", SampleRatio: 2, ExportTimeout: time.Second, BatchTimeout: time.Second},
		{Mode: "otlp", OTLPEndpoint: "https://collector.example", ServiceName: "service", SampleRatio: 1},
	}
	for _, config := range tests {
		if tracing, err := New(context.Background(), config); err == nil {
			_ = tracing.Shutdown(context.Background())
			t.Errorf("New(%#v) error = nil", config)
		}
	}
}
