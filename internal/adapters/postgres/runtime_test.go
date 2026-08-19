package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	observabilitymetrics "github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestClassifyError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err   error
		class ErrorClass
		retry bool
	}{
		{context.Canceled, ErrorCanceled, false}, {context.DeadlineExceeded, ErrorTimeout, false},
		{&pgconn.PgError{Code: "23505"}, ErrorUniqueViolation, false},
		{&pgconn.PgError{Code: "23503"}, ErrorForeignKeyViolation, false},
		{&pgconn.PgError{Code: "23514"}, ErrorCheckViolation, false},
		{&pgconn.PgError{Code: "40001"}, ErrorSerialization, true},
		{&pgconn.PgError{Code: "40P01"}, ErrorDeadlock, true},
		{&pgconn.PgError{Code: "55P03"}, ErrorLockUnavailable, true},
		{&pgconn.PgError{Code: "08006"}, ErrorUnavailable, true},
		{errors.New("opaque"), ErrorUnknown, false},
	}
	for _, tt := range tests {
		if got := ClassifyError(tt.err); got != tt.class {
			t.Errorf("ClassifyError(%v) = %q, want %q", tt.err, got, tt.class)
		}
		if got := IsRetryable(tt.err); got != tt.retry {
			t.Errorf("IsRetryable(%v) = %t, want %t", tt.err, got, tt.retry)
		}
	}
}

func TestPoolSessionPolicyAndReadiness(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	metricSet, err := observabilitymetrics.New(true, observabilitymetrics.BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := Open(context.Background(), PoolConfig{
		URL: databaseURL, ConnectTimeout: time.Second, HealthTimeout: time.Second,
		StatementTimeout: 3 * time.Second, LockTimeout: 750 * time.Millisecond,
		MaxConnectionLifetime: time.Hour, MaxConnectionIdleTime: time.Minute,
		MinConnections: 0, MaxConnections: 3,
	}, metricSet, noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var statementTimeout, lockTimeout string
	if err := pool.QueryRow(context.Background(), "SELECT current_setting('statement_timeout'), current_setting('lock_timeout')").Scan(&statementTimeout, &lockTimeout); err != nil {
		t.Fatal(err)
	}
	if statementTimeout != "3s" || lockTimeout != "750ms" {
		t.Fatalf("session timeouts = %q, %q", statementTimeout, lockTimeout)
	}
	readiness, err := NewReadiness(pool, time.Second, metricSet)
	if err != nil {
		t.Fatal(err)
	}
	if err := readiness.Ready(context.Background()); err == nil {
		t.Fatal("readiness succeeded before MarkReady")
	}
	readiness.MarkReady()
	if err := readiness.Ready(context.Background()); err != nil {
		t.Fatalf("readiness failed: %v", err)
	}
	readiness.MarkNotReady()
	if err := readiness.Ready(context.Background()); err == nil {
		t.Fatal("readiness succeeded after MarkNotReady")
	}
}

func TestOpenRejectsInvalidConfigurationWithoutConnecting(t *testing.T) {
	t.Parallel()
	metricSet, err := observabilitymetrics.New(false, observabilitymetrics.BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(context.Background(), PoolConfig{URL: "postgres://db.example/test", ConnectTimeout: time.Second}, metricSet, noop.NewTracerProvider().Tracer("test"))
	if err == nil || err.Error() != "invalid postgres pool bounds" {
		t.Fatalf("Open() error = %v", err)
	}
	_, err = Open(context.Background(), PoolConfig{}, metricSet, noop.NewTracerProvider().Tracer("test"))
	if err == nil {
		t.Fatal("Open() accepted an empty URL")
	}
}
