// Package httpserver owns the bounded inbound HTTP transport and process probes.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/tracing"
)

type IDGenerator func() (string, error)

// Readiness is false until startup finishes and is cleared before draining.
// Dependency and freshness checks will be composed here by later phases.
type Readiness struct{ ready atomic.Bool }

func (r *Readiness) MarkReady()    { r.ready.Store(true) }
func (r *Readiness) MarkNotReady() { r.ready.Store(false) }
func (r *Readiness) Ready(context.Context) error {
	if !r.ready.Load() {
		return errors.New("service is not ready")
	}
	return nil
}

type ReadinessProbe interface {
	Ready(context.Context) error
	MarkReady()
	MarkNotReady()
}

type Dependencies struct {
	Logger          *slog.Logger
	Metrics         *metrics.Metrics
	Tracing         *tracing.Tracing
	Readiness       ReadinessProbe
	NewID           IDGenerator
	AgentApprovals  http.Handler
	AgentDiscovery  http.Handler
	RunAdmission    http.Handler
	RunQuery        http.Handler
	RunSignal       http.Handler
	RunCancellation http.Handler
}

type Server struct {
	http      *http.Server
	readiness ReadinessProbe
}

func New(httpConfig config.HTTPConfig, dependencies Dependencies) (*Server, error) {
	if dependencies.Logger == nil || dependencies.Metrics == nil || dependencies.Tracing == nil || dependencies.NewID == nil {
		return nil, errors.New("HTTP server dependencies must not be nil")
	}
	readiness := dependencies.Readiness
	if readiness == nil {
		readiness = &Readiness{}
	}
	handler := newHandler(httpConfig, dependencies, readiness)
	return &Server{
		http: &http.Server{
			Addr:              httpConfig.Address,
			Handler:           handler,
			ReadHeaderTimeout: httpConfig.ReadHeaderTimeout,
			ReadTimeout:       httpConfig.ReadTimeout,
			WriteTimeout:      httpConfig.WriteTimeout,
			IdleTimeout:       httpConfig.IdleTimeout,
			MaxHeaderBytes:    httpConfig.MaxHeaderBytes,
		},
		readiness: readiness,
	}, nil
}

func (s *Server) ListenAndServe() error {
	listener, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

func (s *Server) Listen() (net.Listener, error) { return net.Listen("tcp", s.http.Addr) }

func (s *Server) Serve(listener net.Listener) error {
	s.readiness.MarkReady()
	defer s.readiness.MarkNotReady()
	err := s.http.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.readiness.MarkNotReady()
	return s.http.Shutdown(ctx)
}

func (s *Server) Handler() http.Handler { return s.http.Handler }

func newHandler(httpConfig config.HTTPConfig, dependencies Dependencies, readiness ReadinessProbe) http.Handler {
	mux := http.NewServeMux()
	mount := func(path string, handler http.Handler) {
		mux.Handle(path, middleware(path, httpConfig, dependencies, handler))
	}
	mount("/livez", http.HandlerFunc(livez))
	mount("/readyz", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { readyz(writer, request, readiness) }))
	mount("/metrics", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !allowGetOrHead(writer, request) {
			return
		}
		dependencies.Metrics.Handler().ServeHTTP(writer, request)
	}))
	if dependencies.AgentApprovals != nil {
		mount("POST /v1/admin/agents/{agent_id}/versions/{version_digest}/approvals", dependencies.AgentApprovals)
	}
	if dependencies.AgentDiscovery != nil {
		mount("GET /v1/agents", dependencies.AgentDiscovery)
		mount("HEAD /v1/agents", dependencies.AgentDiscovery)
		mount("GET /v1/agents/{agent_id}", dependencies.AgentDiscovery)
		mount("HEAD /v1/agents/{agent_id}", dependencies.AgentDiscovery)
	}
	if dependencies.RunAdmission != nil {
		mount("POST /v1/agents/{agent_id}/runs", dependencies.RunAdmission)
	}
	if dependencies.RunQuery != nil {
		mount("GET /v1/runs/{run_id}", dependencies.RunQuery)
	}
	if dependencies.RunSignal != nil {
		mount("POST /v1/runs/{run_id}/signals", dependencies.RunSignal)
	}
	if dependencies.RunCancellation != nil {
		mount("POST /v1/runs/{run_id}/cancel", dependencies.RunCancellation)
	}
	mux.Handle("/", middleware("unknown", httpConfig, dependencies, http.HandlerFunc(notFound)))
	return mux
}

func livez(writer http.ResponseWriter, request *http.Request) {
	if !allowGetOrHead(writer, request) {
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "alive"})
}

func readyz(writer http.ResponseWriter, request *http.Request, readiness ReadinessProbe) {
	if !allowGetOrHead(writer, request) {
		return
	}
	if err := readiness.Ready(request.Context()); err != nil {
		writeProblem(writer, request, problemFor(http.StatusServiceUnavailable, "not_ready", "Service Not Ready", "The service cannot accept governed traffic."))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func notFound(writer http.ResponseWriter, request *http.Request) {
	writeProblem(writer, request, problemFor(http.StatusNotFound, "not_found", "Not Found", "The requested resource was not found."))
}

func allowGetOrHead(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	writer.Header().Set("Allow", "GET, HEAD")
	writeProblem(writer, request, problemFor(http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed", "The request method is not supported for this resource."))
	return false
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	_ = encodeJSON(writer, value)
}

func deadlineContext(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), timeout)
		defer cancel()
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}
