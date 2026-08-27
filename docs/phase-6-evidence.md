# Phase 6 Revocation Distribution and Freshness Evidence

Phase 6 exited on 2026-08-27 after qualification of atomic scoped revocation,
ordered authenticated distribution, gateway reconciliation, process-local
freshness enforcement, cache invalidation, and fail-closed recovery. The
REV-011 phase-completion commit containing this document is the audited tree; a
subsequent ledger-only commit records its short SHA.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.6 linux/amd64` |
| PostgreSQL | `18.4` on the pinned `postgres:18.4-alpine3.23` image |
| OPA | `1.19.0` |
| Gateway transport | authenticated loopback HTTP/1.1 SSE |
| Propagation sample | 100 tenant-scoped revocations to one continuously connected gateway |

The final gate used loopback-only development dependencies and disposable test
data. No production service, network, identity provider, gateway, or data
participated.

## Connected-gateway propagation result

`TestPhase6ConnectedGatewayPropagationSLO` measures from immediately before the
authoritative PostgreSQL transaction begins until the gateway client decodes
the corresponding event from the authenticated SSE response. The measurement
therefore conservatively includes transaction execution and commit, repository
polling, event serialization, HTTP delivery, and client decoding.

| Samples | p50 | p99 | Maximum | RC target |
|---:|---:|---:|---:|---:|
| 100 | 10.239 ms | 22.463 ms | 33.979 ms | p99 <= 5 s |

All 100 committed changes arrived with their distinct revocation identities and
the test passed the declared connected-gateway target. This Phase 6 result
qualifies functional single-gateway propagation, not production capacity. The
initial capacity envelope of 100 changes/s fanout to 5,000 clients, reconnect
storms, cross-zone network behavior, and sustained alert windows remain Phase 8
production-shaped load and resilience work.

## Gateway integration behavior

A conforming gateway uses only a verified token mapped to its tenant and the
`gateway` or `trusted-workload` role. It must implement this sequence:

1. Start without trusted local freshness after every process restart. Load its
   own last fully applied cursor and epoch vector, but do not serve governed
   traffic merely because that local checkpoint exists.
2. Connect to `GET /v1/trusted/revocations/events` with `Last-Event-ID` (or
   `after`) using the HMAC cursor last returned by the service. The
   `after_sequence` form is only an initial compatibility bootstrap.
3. Validate ordered continuity before applying an event. Apply an exact
   duplicate idempotently without refreshing freshness age. On a missing or
   out-of-order sequence, preserve the last fully applied state, mark a gap,
   deny bounded authorization, and reconcile.
4. Apply the event and its epoch vector atomically to the gateway's revocation
   view, persist the fully applied cursor, then advance the process freshness
   tracker. Accepted observations synchronously invalidate affected local cache
   reachability; epoch/version keys make older remote cache entries unreachable.
5. Treat `410 Gone`, a terminal SSE `gap` event, stream decoding failure, or a
   locally detected gap as a requirement to call
   `POST /v1/trusted/revocations/reconcile`. Apply every delta in order or
   replace local state with the content-addressed active snapshot, verify the
   returned authoritative sequence/epochs, persist them, and only then record a
   successful reconciliation and clear the gap.
6. Reconnect from the newly applied cursor. During disconnect, continue only
   while the operation's monotonic freshness bound remains valid: normal writes
   at most 30 seconds, sensitive reads at most 60 seconds, and low-risk reads an
   explicitly configured finite bound. High-risk writes always require live
   authoritative evaluation and never use a cached ALLOW.

The service-side gateway checkpoint records delivery/reconciliation progress
for operations and regression protection. It is not proof that a gateway
durably applied an event and cannot replace the gateway's own atomic local apply
and cursor persistence. A write failure, process crash, or ambiguous delivery
must replay from the last locally committed cursor; duplicates are safe.

## Qualified failure behavior

The Phase 6 suites cover every supported scope, concurrent monotonic epoch
increments, resumable streaming, retention loss, delta/snapshot reconciliation,
checkpoint regression, duplicate and out-of-order delivery, dropped sequences,
process restart, clock regression, partition beyond each freshness bound,
global and tenant cache invalidation, readiness/metrics, expired ALLOW entries,
and recovery without transient stale authorization. PostgreSQL remains the
authority, while local and Valkey state remain disposable acceleration.

The normative protocol and freshness rules are in
[Revocation, epoch, and freshness contract](contracts/revocation.md). The SLO
and later capacity qualification boundary is in
[Service-level objectives and capacity targets](operations/slos.md).

## Qualification commands

The focused measurement and aggregate acceptance commands are:

```sh
go test -v -count=1 -tags=e2e -run '^TestPhase6ConnectedGatewayPropagationSLO$' ./internal/adapters/postgres
go test -race ./internal/application ./internal/policy
make test-integration
make test-e2e
make verify
```

The final clean-tree gate passes generation drift, formatting,
vet/module/OpenAPI checks, unit and race suites, all 24 OPA policy tests, real
PostgreSQL integration and end-to-end workflows, dependency source, license and
vulnerability checks, binary/image builds, and hardened container smoke. No
required test is skipped.

## Exit conclusion

REV-001 through REV-011 satisfy the Phase 6 exit condition: connected gateways
receive ordered changes within the declared propagation objective, gaps and
partitions cannot silently extend trust, authoritative reconciliation restores
complete state, and bounded serving resumes without exposing a stale ALLOW.

