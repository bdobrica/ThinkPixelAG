package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/evidencehttp"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	postgresadapter "github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/valkey"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
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
	ctx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
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
	if err := metricSet.RegisterDatabasePool(func() (int32, int32, int32) {
		stats := databasePool.Stat()
		return stats.AcquiredConns(), stats.TotalConns(), stats.MaxConns()
	}); err != nil {
		return fmt.Errorf("register database pool metrics: %w", err)
	}
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
	operationalMetrics, err := application.NewOperationalMetricsPublisher(repositories, metricSet, clock)
	if err != nil {
		return fmt.Errorf("initialize operational metrics: %w", err)
	}
	go operationalMetrics.Run(ctx, 15*time.Second)
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

	dependencies := httpserver.Dependencies{
		Logger: logger, Metrics: metricSet, Tracing: traceSet, Readiness: readiness,
		NewID: func() (string, error) { id, idErr := domain.NewID(); return id.String(), idErr },
	}
	var trustedServer *httpserver.Server
	var trustedTLS *tls.Config
	if settings.RuntimeFile != "" {
		runtime, err := readRuntimeSettings(settings.RuntimeFile)
		if err != nil {
			return err
		}
		verifier, err := oidc.New(ctx, settings.OIDC, nil)
		if err != nil {
			return err
		}
		routes := &runtimeRoutes{settings: settings, runtime: runtime, repositories: repositories, policies: policyFreshness, verifier: verifier, clock: clock, metrics: metricSet, client: policyHTTPClient(settings.OPA.Timeout), readiness: securityReadiness}
		if settings.Valkey.URL.IsSet() {
			routes.accelerator, err = valkey.New(settings.Valkey.URL.Value(), settings.Valkey.Timeout, []byte(settings.Valkey.CacheIntegrityKey.Value()))
			if err != nil {
				return fmt.Errorf("initialize throughput cache: %w", err)
			}
		}
		routes.mount(&dependencies, false)
		if runtime.TrustedAddress != "" {
			trustedTLS, routes.workload, err = trustedTransport(runtime)
			if err != nil {
				return err
			}
			trustedDependencies := httpserver.Dependencies{Logger: logger, Metrics: metricSet, Tracing: traceSet, Readiness: readiness, NewID: dependencies.NewID}
			routes.mount(&trustedDependencies, true)
			trustedConfig := settings.HTTP
			trustedConfig.Address = runtime.TrustedAddress
			trustedServer, err = httpserver.New(trustedConfig, trustedDependencies)
			if err != nil {
				return err
			}
		}
	}
	workerCtx, stopWorkers := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { stopWorkers(); workers.Wait() }()
	if settings.Evidence.Endpoint != "" {
		sink, err := evidencehttp.New(evidencehttp.Config{Endpoint: settings.Evidence.Endpoint, BearerToken: settings.Evidence.BearerToken.Value(), Timeout: settings.Evidence.Timeout, MaxResponseBytes: settings.Evidence.MaxResponseBytes}, nil)
		if err != nil {
			return err
		}
		store, err := postgresadapter.NewEvidenceDeliveryStore(databasePool, 2*settings.Evidence.Timeout)
		if err != nil {
			return err
		}
		exporter, err := evidence.NewExporter(settings.Evidence.SinkID, store, sink)
		if err != nil {
			return err
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			for workerCtx.Err() == nil {
				exported, err := exporter.ExportOne(workerCtx, clock.Now())
				if err != nil {
					logger.Warn("evidence export retry", slog.String("category", "evidence_export"))
				}
				if err != nil || !exported {
					timer := time.NewTimer(time.Second)
					select {
					case <-workerCtx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}
		}()
	}
	server, err := httpserver.New(settings.HTTP, dependencies)
	if err != nil {
		return fmt.Errorf("initialize HTTP server: %w", err)
	}
	listener, err := server.Listen()
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}

	listenResult := make(chan error, 2)
	serverCount := 1
	if trustedServer != nil {
		trustedListener, err := trustedServer.Listen()
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("listen trusted HTTP: %w", err)
		}
		serverCount++
		go func() { listenResult <- trustedServer.Serve(tls.NewListener(trustedListener, trustedTLS)) }()
	}
	go func() { listenResult <- server.Serve(listener) }()
	logger.Info("HTTP server started", slog.String("address", listener.Addr().String()), slog.String("version", version), slog.String("revision", revision))
	var serveErr error
	select {
	case serveErr = <-listenResult:
		serverCount--
	case <-ctx.Done():
		logger.Info("HTTP server draining")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.HTTP.ShutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if trustedServer != nil {
		if err := trustedServer.Shutdown(shutdownCtx); shutdownErr == nil {
			shutdownErr = err
		}
	}
	for range serverCount {
		if err := <-listenResult; serveErr == nil {
			serveErr = err
		}
	}
	if shutdownErr != nil {
		return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}
	if serveErr != nil {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}

	logger.Info("HTTP server stopped")
	return nil
}
