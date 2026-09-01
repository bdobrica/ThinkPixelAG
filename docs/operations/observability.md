# Metrics and Tracing

ThinkPixelAG initializes Prometheus metrics and OpenTelemetry tracing through
isolated providers under `internal/observability`. The packages do not register
global Prometheus collectors or mutate OpenTelemetry global providers, keeping
tests and multiple process components deterministic. The process entry point
created in ENG-007 owns provider wiring and shutdown.

## Prometheus metrics

Metrics use a private registry. Enabled mode registers Go runtime, process,
immutable build information, HTTP request count, and HTTP duration collectors.
Disabled mode retains the same API but observations are no-ops and the registry
is empty. The process mounts the registry handler at `GET /metrics`; no default
HTTP mux is used.

Initial application metrics are:

- `thinkpixelag_build_info{version,revision}`;
- `thinkpixelag_http_requests_total{route,method,status_class}`;
- `thinkpixelag_http_request_duration_seconds{route,method}`.

Only stable route templates such as `/v1/runs/{run_id}` may be passed as
`route`; raw paths, tenant/resource identifiers, query strings, objectives, and
other user-controlled values are prohibited. Route labels must match the
versioned OpenAPI templates or operational endpoints; everything else becomes
`unknown`. Methods are reduced to the normal HTTP vocabulary or `OTHER`, and
statuses to `1xx`–`5xx` or `unknown`. Feature phases add their SLO metrics with
the same bounded-label rule.

## OpenTelemetry tracing

Tracing has two modes:

- `noop` (default): a real OpenTelemetry no-op provider with no goroutines,
  network calls, or recorded spans;
- `otlp`: a parent-based ratio sampler and batch processor exporting protobuf
  over OTLP/HTTP to the configured collector base URL. `/v1/traces` is appended
  by the initializer.

The default base URL is the local collector at `http://127.0.0.1:4318` and is
inactive while mode is `noop`. Production requires HTTPS except for a loopback
sidecar. Collector authentication should use workload identity, mTLS, or a local
authenticated proxy; credentials are prohibited in the rendered endpoint URL.

The provider exposes W3C Trace Context and Baggage propagation without changing
global state. Baggage is propagation plumbing, not permission to add tenant IDs,
tokens, objectives, inputs, policy data, or other sensitive/high-cardinality
values. Instrumentation must use bounded operation names and attributes.
The service trace facade enforces this mechanically: caller-provided start
attributes and exception text are discarded, event names are bounded, and only
the reviewed database system/operation/outcome attributes currently pass its
allowlist. New attributes require code, documentation, and sentinel leak tests.

The owner calls `ForceFlush` where evidence requires an export boundary and
calls idempotent `Shutdown` during graceful process termination. Export failures
must be logged through safe error categories and must not expose OTLP payloads.
Tracing does not become an authorization or audit source of truth.

## Local settings

```sh
export THINKPIXELAG_METRICS_ENABLED=true
export THINKPIXELAG_TRACING_MODE=otlp
export THINKPIXELAG_OTLP_ENDPOINT=http://127.0.0.1:4318
export THINKPIXELAG_TRACE_SAMPLE_RATIO=1
```

Use `noop` when no collector is running. CI tests both no-op behavior and a local
in-process OTLP receiver, including export, propagation, flushing, and shutdown.
