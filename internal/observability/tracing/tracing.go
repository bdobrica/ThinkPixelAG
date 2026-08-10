// Package tracing initializes isolated OpenTelemetry trace providers for no-op
// and OTLP/HTTP operation.
package tracing

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const InstrumentationName = "github.com/bdobrica/ThinkPixelAG"

// Config contains non-secret trace pipeline settings.
type Config struct {
	Mode          string
	ServiceName   string
	Environment   string
	OTLPEndpoint  string
	SampleRatio   float64
	ExportTimeout time.Duration
	BatchTimeout  time.Duration
}

// Tracing owns a provider, W3C propagation, flushing, and idempotent shutdown.
// It does not mutate OpenTelemetry globals.
type Tracing struct {
	Provider    trace.TracerProvider
	Propagator  propagation.TextMapPropagator
	flush       func(context.Context) error
	shutdown    func(context.Context) error
	once        sync.Once
	shutdownErr error
}

// New initializes a no-op provider or a batched OTLP/HTTP exporter.
func New(ctx context.Context, config Config) (*Tracing, error) {
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	if config.Mode == "noop" {
		return &Tracing{
			Provider:   noop.NewTracerProvider(),
			Propagator: propagator,
			flush:      func(context.Context) error { return nil },
			shutdown:   func(context.Context) error { return nil },
		}, nil
	}
	if config.Mode != "otlp" {
		return nil, fmt.Errorf("tracing mode must be noop or otlp")
	}
	if config.ServiceName == "" {
		return nil, fmt.Errorf("tracing service name is required")
	}
	if math.IsNaN(config.SampleRatio) || math.IsInf(config.SampleRatio, 0) || config.SampleRatio < 0 || config.SampleRatio > 1 {
		return nil, fmt.Errorf("trace sample ratio must be a finite number from 0 through 1")
	}
	if config.ExportTimeout <= 0 || config.BatchTimeout <= 0 {
		return nil, fmt.Errorf("trace export and batch timeouts must be greater than zero")
	}
	traceEndpoint, err := endpointForTraces(config.OTLPEndpoint)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(traceEndpoint),
		otlptracehttp.WithTimeout(config.ExportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", config.ServiceName),
			attribute.String("deployment.environment.name", config.Environment),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.SampleRatio))),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithExportTimeout(config.ExportTimeout),
			sdktrace.WithBatchTimeout(config.BatchTimeout),
		),
	)
	return &Tracing{
		Provider:   provider,
		Propagator: propagator,
		flush:      provider.ForceFlush,
		shutdown:   provider.Shutdown,
	}, nil
}

// Tracer returns an instrumentation-scoped tracer.
func (t *Tracing) Tracer() trace.Tracer { return t.Provider.Tracer(InstrumentationName) }

// ForceFlush exports all completed spans available to the processor.
func (t *Tracing) ForceFlush(ctx context.Context) error { return t.flush(ctx) }

// Shutdown flushes and closes the provider exactly once.
func (t *Tracing) Shutdown(ctx context.Context) error {
	t.once.Do(func() { t.shutdownErr = t.shutdown(ctx) })
	return t.shutdownErr
}

func endpointForTraces(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("OTLP endpoint must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("OTLP endpoint must not contain credentials, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/traces"
	return parsed.String(), nil
}
