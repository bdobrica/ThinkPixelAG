# Service-Level Objectives and Capacity Targets

These are initial RC targets for a single region. Phase 8 load and resilience testing must validate or revise them with evidence. Security invariants are not traded against availability SLOs.

## Measurement rules

- Availability counts a request successful only when it returns the contractually correct response; fail-closed security denials caused by stale/unavailable governance state are service failures for availability accounting but remain the required security behavior.
- Latency is measured at the API process from accepted request to completed response, excluding client/network time; streaming setup and event lag are measured separately.
- Planned maintenance is included unless the deployment's organizational SLO policy explicitly excludes it.
- Metrics avoid tenant, principal, run, or agent IDs as labels.

## SLOs

| Indicator | RC objective over rolling 30 days | Alert windows |
|---|---:|---|
| Public read API availability | 99.9% | fast burn 1h; slow burn 6h/3d |
| Run create/mutation API availability | 99.9% | fast burn 1h; slow burn 6h/3d |
| Admin/trusted API availability | 99.5% | 1h and 24h |
| Public cached read latency | p95 <= 100 ms, p99 <= 250 ms | 10m/1h |
| Run admission latency excluding external approval | p95 <= 300 ms, p99 <= 750 ms | 10m/1h |
| Other mutation latency | p95 <= 200 ms, p99 <= 500 ms | 10m/1h |
| OPA decision latency at service boundary | p95 <= 25 ms, p99 <= 75 ms | 10m/1h |
| Revocation propagation to connected gateway | p99 <= 5 s | immediate if >10 s sustained 5m |
| Authoritative reconciliation success | >= 99.9%; normal-write age <=30 s | before freshness gate and on any gap |
| Transactional outbox publication lag | p99 <= 30 s; no loss | warn 30 s, critical 5 min |
| Terminal resource settlement lag | p99 <= 10 s | warn 30 s, critical 5 min |

High-risk writes have a non-negotiable security objective: cached ALLOW is prohibited and current authoritative revocation state is required. Normal-write and sensitive-read freshness bounds remain 30 and 60 seconds respectively even if enforcing them reduces availability.

## Initial capacity envelope

Validate the following minimums with production-shaped payloads and policy complexity:

| Workload | Minimum sustained target | Burst/test condition |
|---|---:|---|
| Public/API reads | 1,000 requests/s per deployment | 2x for 5 minutes without SLO breach |
| Run admissions | 200 requests/s | 400/s for 5 minutes; no duplicate run/oversubscription |
| Lifecycle mutations | 500 requests/s | 2x for 5 minutes |
| Trusted usage events | 2,000 events/s | duplicates and mixed dimensions included |
| Concurrent SSE clients | 5,000 per deployment | reconnect 25% within 30 seconds |
| Revocation fanout | 100 changes/s to 5,000 clients | global change meets propagation SLO |
| Outbox drain | >= 2x peak committed event rate | recover a 15-minute backlog within 30 minutes |

API request bodies default to 1 MiB maximum, with stricter per-route schemas. List page size defaults to 50 and caps at 200. Signal payloads default to 64 KiB. SSE consumers have bounded queues; slow consumers reconnect from cursors rather than consume unbounded memory. Final limits are configuration with documented safe maxima.

## Resource guardrails

- Database pools are bounded per replica and sized against the PostgreSQL connection budget; readiness does not amplify connection storms.
- HTTP and dependency concurrency, timeouts, idle connections, request bodies, header size, pagination, and worker batch sizes are bounded.
- OPA evaluation input and output have byte, nesting, collection, and duration bounds.
- Cardinality budgets prohibit dynamic IDs in metric dimensions.
- Valkey outage testing must meet correctness/security behavior even if latency/capacity degrades.

## Error budget and release policy

Critical security invariant failure, evidence loss, cross-tenant exposure, resource expansion, or epoch regression has zero error budget and blocks release. Otherwise, exhausting a service error budget pauses non-remediation releases until the cause is understood and an approved recovery plan exists.

## Required dashboards and evidence

Record request count/status/latency by bounded route/action, authn/authz outcomes, OPA latency/error/bundle age, PostgreSQL pool/transaction conflict/deadlock, allocation conflicts/invariant checks, run state and settlement lag, revocation sequence/age/gap/reconciliation, outbox backlog/age/retry/dead letter, SSE clients/lag/drops, cache hit/error, runtime saturation, and rollout/build version.

Phase 8 reports the environment, dataset, policy size, topology, test commands, percentiles, saturation point, bottleneck, failure behavior, and artifact commit/digest.
