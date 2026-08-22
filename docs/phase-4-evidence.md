# Phase 4 Run Lifecycle API Evidence

Phase 4 exited on 2026-08-22 after qualification of authenticated run
admission and lookup, durable signals and cancellation, ordered resumable event
streaming, fenced worker mutations, and at-least-once event publication. Commit
`81b7508` is the composed lifecycle qualification baseline. The RUN-011
phase-completion commit containing this document is the audited tree; a
subsequent ledger-only commit records its short SHA.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.6 linux/amd64` |
| PostgreSQL | `18.4` on the pinned `postgres:18.4-alpine3.23` image |
| OpenAPI validator | Redocly CLI `2.3.0` |
| Race detector | enabled for the complete Go race suite and focused lifecycle workflow |

The final qualification used an isolated loopback PostgreSQL instance on host
port 55432 because the default development port was occupied. Its disposable
test volume was preserved and the isolated service was stopped after the gate.
No production service or data participated.

## Qualified lifecycle behavior

Admission derives tenant and principal from verified identity, evaluates policy
fail closed, resolves one eligible immutable version, narrows constraints, and
atomically stores the admitted run, resolution snapshot, envelope header,
initial event, audit record, and outbox message. Strict bounded JSON and scoped
request hashes provide exact replay and detect changed-content key reuse without
persisting the restricted objective or input in governance evidence.

Run lookup is tenant bound and policy authorized, with an enumeration-safe
not-found result for absent, cross-tenant, or hidden identifiers. Typed signals
and cancellation each have independent authorization and idempotency scopes.
Their state checks, ordered events, audit records, and outbox messages commit in
one PostgreSQL transaction. Cancellation and worker terminal mutations lock the
same run, producing one stable terminal result and exactly one terminal event.

The SSE boundary emits increasing per-run sequences with authenticated,
run-bound cursor IDs. Reconnect resumes strictly after `Last-Event-ID` or
`after`; authorization is re-evaluated on each connection. Logical retention,
heartbeats, bounded batches, and write deadlines make stale cursors and slow
consumers explicit rather than silently dropping continuity.

Workers claim tenant queues with `SKIP LOCKED`, receive expiring leases and
monotonic fences, and must present the current unexpired lease for every
heartbeat or mutation. Reclaim invalidates stale workers. Every claim,
heartbeat, start, completion, failure, and timeout emits a transactional event
and outbox row sharing one stable UUID, allowing a sink to deduplicate replay
after an ambiguous post-send crash.

The durable state machine and actor/transition table are maintained in
[Domain contracts and state machines](contracts/domain-model.md). Executable
public request, response, SSE, reconnect, and concurrency guidance is in
[Run lifecycle API examples](api/run-lifecycle.md). OpenAPI remains canonical
for schemas and transport status codes.

## Qualification commands and results

The Phase 4 acceptance gate completed successfully with:

```sh
go test -count=10 -tags=e2e ./internal/adapters/postgres -run TestPhase4RunLifecycleWorkflow
go test -race -count=1 -tags=e2e ./internal/adapters/postgres -run TestPhase4RunLifecycleWorkflow
make test-e2e
make test-integration
make lint
make security
make compose-check build container-smoke
make verify
```

The composed real-PostgreSQL workflow mounts the actual public HTTP,
application, policy, idempotency, repository, and worker boundaries. It proves
the OpenAPI-shaped admission/status responses, exact admission/signal/cancel
replay, request-hash conflict, tenant isolation, SSE reconnect, cancellation
versus completion serialization, and a single terminal event. Deeper adapter
suites cover retention loss, slow-stream handling, simultaneous claims, lease
expiry/reclaim, stale-worker fencing, high-contention terminal transitions, and
outbox crash/replay identity.

The full gate passed generation drift, formatting, vet/module/OpenAPI checks,
unit and race suites, pinned OPA policy tests, real-PostgreSQL integration and
end-to-end suites, dependency source/license/vulnerability checks, binary and
image builds, and hardened container smoke. No required test was skipped.

## Exit conclusion

RUN-001 through RUN-011 satisfy the Phase 4 exit condition: the OpenAPI run
lifecycle contract and complete lifecycle/idempotency workflow pass with
tenant isolation, concurrency safety, resumable events, fenced workers, and
replay-safe publication. Phase 5 can add authoritative resource dimensions,
balances, reservations, metering, settlement, and reclaim to the qualified run
and envelope boundaries.
