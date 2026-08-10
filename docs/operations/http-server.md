# HTTP Server and Process Lifecycle

`cmd/thinkpixelag` is the runnable governance-plane process. Startup strictly
loads configuration, creates the redacting JSON logger, private Prometheus
registry, isolated OpenTelemetry provider, and bounded HTTP server, then marks
the instance ready. `SIGINT` or `SIGTERM` clears readiness before draining HTTP
requests within `THINKPIXELAG_HTTP_SHUTDOWN_TIMEOUT` and closing tracing.

## Middleware contract

Inbound middleware is deliberately ordered from outermost to innermost:

1. validate a canonical UUIDv7 `X-Request-ID` or generate a cryptographically
   random replacement, and echo it on every response;
2. extract W3C trace context and start a bounded-name server span;
3. attach request and active trace IDs to the trusted logging context;
4. record bounded route/method/status metrics and a body-free access log;
5. recover panics without returning or logging the recovered value;
6. reject known oversized bodies and cap streaming bodies;
7. apply a cancellation deadline to the handler context;
8. dispatch the endpoint.

Handlers and downstream adapters must honor context cancellation. Socket read,
write, header-read, and idle timeouts provide additional transport bounds.
`MaxHeaderBytes` is enforced by `net/http`; `DecodeJSON` detects streaming bodies
that cross the `MaxBodyBytes` cap, rejects unknown fields, and accepts exactly
one JSON value.

## Errors

Errors cross the HTTP boundary once through RFC 7807
`application/problem+json`. Domain codes map to stable HTTP statuses. Unknown
errors and panics become a generic `internal` problem and never expose wrapped
causes or recovered values. Problems and all other responses carry
`X-Request-ID`; retryable unavailable errors include a retry hint.

## Operational endpoints

- `GET`/`HEAD /livez` reports process liveness and never depends on an upstream
  service.
- `GET`/`HEAD /readyz` initially reports completion of local process startup.
  Phase 2 adds PostgreSQL state; later phases add loaded-policy and revocation
  freshness gates. Readiness is cleared before shutdown.
- `GET`/`HEAD /metrics` exposes the private registry. Disabling metrics leaves a
  valid empty registry response.

These endpoints are intentionally unauthenticated for Kubernetes probes and
Prometheus scraping. They expose no tenant or secret data and must be restricted
to cluster monitoring paths through ingress and NetworkPolicy. Governance API
authentication is implemented separately in Phase 3; a VPC/VPN is not treated
as an identity boundary.
