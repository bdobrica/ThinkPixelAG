package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/logging"
	"go.opentelemetry.io/otel/trace"
)

type requestIDKey struct{}

func middleware(route string, httpConfig config.HTTPConfig, dependencies Dependencies, handler http.Handler) http.Handler {
	// Streaming responses own their lifecycle and are bounded by disconnect,
	// shutdown, heartbeat, and per-write deadlines rather than a unary timeout.
	if route != "GET /v1/runs/{run_id}/events" {
		handler = deadlineContext(handler, httpConfig.HandlerTimeout)
		handler = responseWriteDeadline(handler, httpConfig.WriteTimeout)
	}
	handler = bodyLimit(handler, httpConfig.MaxBodyBytes)
	handler = recoverPanic(handler, dependencies.Logger)
	handler = observe(handler, route, dependencies)
	handler = correlate(handler)
	handler = traceRequest(handler, route, dependencies)
	handler = requestID(handler, dependencies.NewID)
	return handler
}

func responseWriteDeadline(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(timeout))
		next.ServeHTTP(writer, request)
	})
}

func requestID(next http.Handler, generate IDGenerator) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		id := request.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			var err error
			id, err = generate()
			if err != nil {
				id = "unavailable"
			}
		}
		writer.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(request.Context(), requestIDKey{}, id)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func validRequestID(value string) bool {
	_, err := domain.ParseID(value)
	return err == nil
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func recoverPanic(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(request.Context(), "HTTP handler panic recovered", slog.String("category", "panic"), slog.Int("stack_bytes", len(debug.Stack())))
				if recorder, ok := writer.(*responseRecorder); ok && recorder.status != 0 {
					return
				}
				writeProblem(writer, request, problemFor(http.StatusInternalServerError, "internal", "Internal Server Error", "An internal error occurred."))
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func traceRequest(next http.Handler, route string, dependencies Dependencies) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := dependencies.Tracing.Propagator.Extract(request.Context(), headerCarrier(request.Header))
		ctx, span := dependencies.Tracing.Tracer().Start(ctx, boundedHTTPMethod(request.Method)+" "+route, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

type headerCarrier http.Header

func (carrier headerCarrier) Get(key string) string { return http.Header(carrier).Get(key) }
func (carrier headerCarrier) Set(key, value string) { http.Header(carrier).Set(key, value) }
func (carrier headerCarrier) Keys() []string {
	keys := make([]string, 0, len(carrier))
	for key := range carrier {
		keys = append(keys, key)
	}
	return keys
}

func correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		correlation := logging.Correlation{RequestID: requestIDFromContext(request.Context())}
		if spanContext := trace.SpanContextFromContext(request.Context()); spanContext.IsValid() {
			correlation.TraceID = spanContext.TraceID().String()
		}
		next.ServeHTTP(writer, request.WithContext(logging.WithCorrelation(request.Context(), correlation)))
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status, bytes int
}

func (writer *responseRecorder) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *responseRecorder) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	count, err := writer.ResponseWriter.Write(data)
	writer.bytes += count
	return count, err
}
func (writer *responseRecorder) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func observe(next http.Handler, route string, dependencies Dependencies) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: writer}
		next.ServeHTTP(recorder, request)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		duration := time.Since(started)
		dependencies.Metrics.ObserveHTTP(route, request.Method, recorder.status, duration)
		dependencies.Logger.InfoContext(request.Context(), "HTTP request completed",
			slog.String("method", boundedHTTPMethod(request.Method)), slog.String("route", route), slog.Int("status", recorder.status),
			slog.Int("response_bytes", recorder.bytes), slog.Duration("duration", duration))
	})
}

func boundedHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func bodyLimit(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ContentLength > limit {
			writeProblem(writer, request, problemFor(http.StatusRequestEntityTooLarge, "request_too_large", "Request Too Large", fmt.Sprintf("The request body exceeds the %d-byte limit.", limit)))
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, limit)
		next.ServeHTTP(writer, request)
	})
}
