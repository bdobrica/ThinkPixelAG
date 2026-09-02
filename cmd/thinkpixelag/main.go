package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	postgresadapter "github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/logging"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/tracing"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "thinkpixelag: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	settings, err := config.Load(args)
	if err != nil {
		return err
	}
	logger, err := logging.New(os.Stdout, settings.Log.Level)
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	metricSet, err := metrics.New(settings.Telemetry.MetricsEnabled, metrics.BuildInfo{Version: version, Revision: revision})
	if err != nil {
		return fmt.Errorf("initialize metrics: %w", err)
	}
	traceSet, err := tracing.New(ctx, tracing.Config{
		Mode: settings.Telemetry.TracingMode, ServiceName: settings.Telemetry.ServiceName,
		Environment: string(settings.Environment), OTLPEndpoint: settings.Telemetry.OTLPEndpoint,
		SampleRatio: settings.Telemetry.TraceSampleRatio, ExportTimeout: settings.Telemetry.TraceExportTimeout,
		BatchTimeout: settings.Telemetry.TraceBatchTimeout,
	})
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	databasePool, err := postgresadapter.Open(ctx, postgresadapter.PoolConfig{
		URL: settings.Database.URL.Value(), ConnectTimeout: settings.Database.ConnectTimeout,
		HealthTimeout: settings.Database.HealthTimeout, StatementTimeout: settings.Database.StatementTimeout,
		LockTimeout: settings.Database.LockTimeout, MaxConnectionLifetime: settings.Database.MaxConnectionLifetime,
		MaxConnectionIdleTime: settings.Database.MaxConnectionIdleTime, MinConnections: settings.Database.MinConnections,
		MaxConnections: settings.Database.MaxConnections,
	}, metricSet, traceSet.Tracer())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer databasePool.Close()
	databaseReadiness, err := postgresadapter.NewReadiness(databasePool, settings.Database.HealthTimeout, metricSet)
	if err != nil {
		return fmt.Errorf("initialize database readiness: %w", err)
	}
	clock := domain.SystemClock{}
	policyFreshness, err := policy.NewFreshness(settings.OPA.BundleMaxAge, clock.Now)
	if err != nil {
		return fmt.Errorf("initialize policy freshness: %w", err)
	}
	revocationFreshness, err := application.NewRevocationFreshnessTracker(clock)
	if err != nil {
		return fmt.Errorf("initialize revocation freshness: %w", err)
	}
	repositories, err := postgresadapter.NewRepositories(databasePool)
	if err != nil {
		return fmt.Errorf("initialize repositories: %w", err)
	}
	securityReadiness, err := application.NewRuntimeSecurityReadiness(repositories, policyFreshness, revocationFreshness, clock, application.DefaultNormalWriteFreshness)
	if err != nil {
		return fmt.Errorf("initialize security readiness: %w", err)
	}
	if err := metricSet.RegisterRevocationFreshness(func() (time.Duration, int64, int, uint64, int, bool) {
		status := revocationFreshness.Metrics(application.DefaultNormalWriteFreshness)
		return status.MaximumAge, status.MaximumLag, status.CurrentGaps, status.GapEvents, status.TrackedTenants, status.Healthy
	}); err != nil {
		return fmt.Errorf("register revocation freshness metrics: %w", err)
	}
	refreshInterval := min(settings.OPA.BundleMaxAge/2, application.DefaultNormalWriteFreshness/2)
	go securityReadiness.Run(ctx, refreshInterval)
	readiness := httpserver.ComposeReadiness(databaseReadiness, securityReadiness)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.HTTP.ShutdownTimeout)
		defer cancel()
		if shutdownErr := traceSet.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("tracing shutdown failed", slog.String("category", "telemetry_shutdown"))
		}
	}()

	server, err := httpserver.New(settings.HTTP, httpserver.Dependencies{
		Logger: logger, Metrics: metricSet, Tracing: traceSet, Readiness: readiness,
		NewID: func() (string, error) { id, idErr := domain.NewID(); return id.String(), idErr },
	})
	if err != nil {
		return fmt.Errorf("initialize HTTP server: %w", err)
	}
	listener, err := server.Listen()
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}

	listenResult := make(chan error, 1)
	go func() { listenResult <- server.Serve(listener) }()
	logger.Info("HTTP server started", slog.String("address", listener.Addr().String()), slog.String("version", version), slog.String("revision", revision))
	select {
	case listenErr := <-listenResult:
		if listenErr != nil {
			return fmt.Errorf("serve HTTP: %w", listenErr)
		}
		return nil
	case <-ctx.Done():
		logger.Info("HTTP server draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.HTTP.ShutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	listenErr := <-listenResult
	if shutdownErr != nil {
		return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}
	if listenErr != nil {
		return fmt.Errorf("serve HTTP: %w", listenErr)
	}
	logger.Info("HTTP server stopped")
	return nil
}
