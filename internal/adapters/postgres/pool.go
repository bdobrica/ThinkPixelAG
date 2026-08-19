package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type PoolConfig struct {
	URL                                                          string
	ConnectTimeout, HealthTimeout, StatementTimeout, LockTimeout time.Duration
	MaxConnectionLifetime, MaxConnectionIdleTime                 time.Duration
	MinConnections, MaxConnections                               int32
}

func Open(ctx context.Context, cfg PoolConfig, metricSet *metrics.Metrics, tracer trace.Tracer) (*pgxpool.Pool, error) {
	if cfg.URL == "" || metricSet == nil || tracer == nil {
		return nil, errors.New("postgres pool requires URL, metrics, and tracer")
	}
	parsed, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool configuration: %w", err)
	}
	if cfg.ConnectTimeout <= 0 || cfg.HealthTimeout <= 0 || cfg.StatementTimeout <= 0 || cfg.LockTimeout <= 0 || cfg.MaxConnectionLifetime <= 0 || cfg.MaxConnectionIdleTime <= 0 || cfg.MinConnections < 0 || cfg.MaxConnections < 1 || cfg.MinConnections > cfg.MaxConnections {
		return nil, errors.New("invalid postgres pool bounds")
	}
	parsed.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	parsed.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	parsed.ConnConfig.RuntimeParams["lock_timeout"] = strconv.FormatInt(cfg.LockTimeout.Milliseconds(), 10)
	parsed.MinConns, parsed.MaxConns = cfg.MinConnections, cfg.MaxConnections
	parsed.MaxConnLifetime, parsed.MaxConnIdleTime = cfg.MaxConnectionLifetime, cfg.MaxConnectionIdleTime
	parsed.ConnConfig.Tracer = queryTracer{metrics: metricSet, tracer: tracer}
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	return pool, nil
}

type Readiness struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	metrics *metrics.Metrics
	serving atomic.Bool
}

func NewReadiness(pool *pgxpool.Pool, timeout time.Duration, metricSet *metrics.Metrics) (*Readiness, error) {
	if pool == nil || timeout <= 0 || metricSet == nil {
		return nil, errors.New("postgres readiness requires pool, timeout, and metrics")
	}
	return &Readiness{pool: pool, timeout: timeout, metrics: metricSet}, nil
}
func (r *Readiness) MarkReady()    { r.serving.Store(true) }
func (r *Readiness) MarkNotReady() { r.serving.Store(false); r.metrics.SetDatabaseHealthy(false) }
func (r *Readiness) Ready(ctx context.Context) error {
	if !r.serving.Load() {
		return errors.New("service is not ready")
	}
	started := time.Now()
	healthCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	err := r.pool.Ping(healthCtx)
	r.metrics.SetDatabaseHealthy(err == nil)
	outcome := telemetryOutcome(err)
	r.metrics.ObserveDatabase("health", outcome, time.Since(started))
	if err != nil {
		return fmt.Errorf("postgres dependency is not ready: %w", err)
	}
	return nil
}

type queryTraceData struct {
	start time.Time
	span  trace.Span
}
type queryTraceKey struct{}
type queryTracer struct {
	metrics *metrics.Metrics
	tracer  trace.Tracer
}

func (t queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ctx, span := t.tracer.Start(ctx, "postgres.query", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.operation.name", "query")))
	return context.WithValue(ctx, queryTraceKey{}, queryTraceData{start: time.Now(), span: span})
}
func (t queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	traceData, ok := ctx.Value(queryTraceKey{}).(queryTraceData)
	if !ok {
		return
	}
	outcome := telemetryOutcome(data.Err)
	t.metrics.ObserveDatabase("query", outcome, time.Since(traceData.start))
	traceData.span.SetAttributes(attribute.String("thinkpixelag.db.outcome", outcome))
	if data.Err != nil {
		traceData.span.SetStatus(codes.Error, outcome)
	}
	traceData.span.End()
}
func telemetryOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	switch ClassifyError(err) {
	case ErrorCanceled:
		return "canceled"
	case ErrorTimeout:
		return "timeout"
	case ErrorUniqueViolation, ErrorForeignKeyViolation, ErrorCheckViolation:
		return "constraint"
	case ErrorSerialization, ErrorDeadlock, ErrorLockUnavailable:
		return "conflict"
	case ErrorUnavailable:
		return "unavailable"
	default:
		return "error"
	}
}
