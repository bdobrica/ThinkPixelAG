# Capacity and scaling

The integration RC accepts the measured homelab operating point below for
development of dependent components. Production [SLOs and minimum capacity
targets](slos.md) remain unchanged. Their remaining qualification
is explicitly [deferred](../adr/0012-integration-rc-qualification-deferrals.md), not passed.
No 30-day availability SLO has been established by these short tests.

## What was demonstrated

| Workload | Measured result | Interpretation |
|---|---|---|
| Admissions with durable evidence, 25/s for 5 minutes | 7,500/7,500 successful; zero drops/errors; client p95/p99 36.4/52.8 ms; publication p99 1.191 s; zero final backlog | Conservative point for integration and recovery work |
| Public reads, 1,000/s for 60 seconds | 60,000/60,000 successful; client p95/p99 12.2/16.4 ms | Separate read-only sample; not a combined workload guarantee |
| Admissions with durable evidence, 200/s for 60 seconds | 9,587/12,000 successful; 2,413 dropped; publication p99 107.7 s | Fails production throughput/publication qualification |
| Read burst, 2,000/s for 5 minutes, best recorded run | 599,230/600,000 successful; 770 dropped | Fails offered-load completion qualification |

Sources: [selected request and publication results](../evidence/README.md#capacity).
Client latency includes network time; it is not the
API-process latency definition used by the production SLO. The conservative
sample used four API replicas; the retained HPA subsequently returned to its
three-replica minimum. Reconfirm the operating point after placement, replica,
policy, payload or workload-mix changes. Twenty-five admissions/s is a measured
point, not a discovered maximum or an enforced application limit.

The topology is wired ARM64 Raspberry Pi API/OPA replicas plus SSD-hosted
PostgreSQL and the durable sink/exporter. The latter share one host failure
domain. PostgreSQL and sink flushes remain enabled. RAM-backed verification
storage supplies no durability or production-capacity evidence. Full usage,
settlement, fanout, SSE and backlog targets remain in DQ-001; production AR and
gateway behavior remain in DQ-002/003.

## Scaling and operating guidance

Start integration traffic below the conservative admission point, with bounded
concurrency. Measure offered, completed, dropped and failed requests together;
successful-request percentiles alone conceal overload. Observe outbox age and
backlog through the entire run and drain, alongside OPA latency, CPU throttling,
database locks/pool waits and storage latency. A growing backlog means the
end-to-end workload is not sustainable even if admission responses are fast.

Scale API/OPA replicas only after identifying CPU or per-replica concurrency as
the constraint. The retained HPA's observed 3→4→3 behavior proves controller
operation, not linear throughput scaling or the production replica envelope;
see [lifecycle evidence](../evidence/README.md#recovery). Preserve readiness,
disruption budgets and available node headroom during rollout.

Budget database connections across every API replica, exporters, migration jobs
and operational clients, reserving recovery headroom. With a 32-connection API
pool, four replicas alone can request 128 connections; raising either replica
count or pool size requires checking the server's actual connection budget.
More replicas do not remove lock contention or durable storage latency.

Evidence delivery serializes a checkpoint's claim, synchronous sink receipt and
atomic completion. More exporters do not promise parallel throughput for that
same chain. Lower-latency durable storage or a deliberately designed durable
batching change needs separate correctness and capacity qualification. Do not
disable flushes, weaken receipt semantics, move authoritative data to RAM, or
use Valkey as authority to meet a benchmark.

If overload occurs, reduce offered traffic and let durable work drain; preserve
idempotency and inspect uncertain mutations before retrying. Apply the existing
[runbooks](runbooks.md) for dependency failures and invariant
checks. Security failure, evidence loss or accounting corruption still blocks
the candidate; accepted performance deferrals do not cover those failures.
