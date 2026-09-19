# Wired ARM64 qualification

The 2026-09-19 retry uses node internal addresses on a wired network. The
controller negotiated 1,000 Mb/s, full duplex. Its Flannel VXLAN uses eth0;
all four nodes advertise wired Flannel peer addresses. Three to four API
replicas use the retained HPA; PostgreSQL remains the promoted `ops011-standby` writer on
a persistent USB-flash PVC. The old writer stays fenced. Production SLOs are
unchanged; these short homelab samples are not a production qualification.

The API image from `406db54` is
`sha256:a8a8a3584350a62c8ad0dd52b096ab40bce45072b9356b11cbbe052d89d2d79e`.
The [atomic admission fix](admission-atomicity.md) now commits the Run and
its exact replay response together.

## RAM use and test isolation

The 9.3 MiB external load executable runs from workstation RAM; measurements
remain in memory until aggregate reports are written to persistent storage.
A separate retained PostgreSQL instance on another worker uses a bounded
1 GiB memory volume and a 2 GiB container memory limit for regression tests.
It has no operational authority and is not the API database.

The initial full repository gate on the shared persistent instance was stopped
during checkpoint stalls: the database node showed 73–74% I/O wait, PostgreSQL
`DataFileFlush`/`WalSync` waits, and delayed admissions. After isolating regression
work, that node returned to 0% I/O wait. `make verify` passed on the RAM-backed
regression instance; `make test-resilience` also passed. Main-database data/WAL
and durable sink receipts remain persistent. RAM results do not prove crash
durability or production throughput.

## Workload results

Client latency includes the wired transport. Server histogram samples are
recorded separately, with one-minute windows and bucket interpolation. Offered
requests dropped by the concurrency bound count as capacity failures even when
every completed response is correct. Loads ran sequentially, with no fault
injection or database regression workload during capacity measurements.

| Workload | Offered rate | Duration | Successful / offered | Client p95 / p99 | Outcome |
| --- | ---: | ---: | ---: | ---: | --- |
| read | 100/s | 60 s | 6,000 / 6,000 | 12.4 / 15.0 ms | Completion and sampled latency pass |
| read | 500/s | 60 s | 29,357 / 30,000 | 52.3 / 382.8 ms | Does not qualify |
| read | 1000/s | 60 s | 36,875 / 60,000 | 629.5 / 757.3 ms | Does not qualify |
| read | 200/s | 180 s | 36,000 / 36,000 | 10.9 / 13.6 ms | Completion and sampled latency pass |
| admission | 1/s | 60 s | 42 / 60 | 14334.6 / 15002.4 ms | Does not qualify |
| admission | 0.1/s | 120 s | 12 / 12 | 3020.7 / 3020.7 ms | Does not qualify |

At 200 reads/s, all 36,000 requests completed without drops or transport errors.
The final one-minute server histogram was approximately 10 ms p95 / 18 ms p99;
OPA remained below its latency target and outbox backlog was zero. Treat 200/s
as a demonstrated read operating point, not a discovered absolute maximum.
The 500/s run dropped 643 offered requests; the 1,000/s run dropped 23,125 and
reached only 611.8 successful reads/s. Neither justifies a 2,000/s burst pass.

Writes remain flash-limited. One admission/s returned 42 successes, eight HTTP
500s and two transport errors, with eight concurrency drops. At 0.1/s all 12
completed, but the tail was about three seconds. That is a low-rate correctness
operating point, not a write-latency SLO pass. The 200/s production admission
target remains unmet; higher-rate mutation/usage and backlog qualification
cannot be inferred from these results.

## Correctness and persistent recovery

- Completion-delay injection rolled back admission; the same-key retry admitted
  once. A 32-request concurrent-key check returned 31 conflicts and one success;
  the final replay matched byte-for-byte and only one Run existed.
- A reconciliation during the write sweep found 48 committed Runs since its
  start and zero without completed replay records. This aggregate included
  subsequent low-rate test admissions.
- PostgreSQL was SIGKILLed from the host after verifying the container process
  identity. Its existing persistent volume recovered in 92.495 seconds; all 41
  authoritative table hashes, the protected Run response and restored invariants
  matched. The interrupted-startup log confirmed automatic WAL recovery; redo
  completed quickly and the end-of-recovery checkpoint delayed readiness. This
  is single-writer process-crash recovery, not HA failover or power-loss proof.
- The first low-rate lifecycle repeat passed admission, replay, conflicting-key
  denial and tenant/authentication boundaries, then its signal returned 500.
  The failed attempt and private checkpoint were retained.

The first OPA-outage attempt observed `[200, 503, 503]` immediately after
Deployment rollout completion. The runner now waits for every prior API Pod
to terminate before asserting a fleet-wide fault; readiness of replacement
replicas alone overlaps old-process drain. A regression fixture reproduces
that overlap and requires the barrier. The subsequent live outage and malformed
output samples each returned three explicit 503s, with zero transport failures.
The original failed attempt is retained rather than overwritten.

Deployed database-latency injection returned three HTTP 500s, then recovered
in 6.027 seconds. Pod eviction recovered, and the rolling-restart sample
completed 20/20 protected reads. Real-adapter Valkey loss fell back to OPA;
the later healthy probe hit the remote cache and enforced authoritative bypass.
The first healthy probe after Deployment readiness returned nonzero without an
aggregate result; that failed attempt remains recorded. These cache probes
still use test composition and do not qualify a production-enabled cache.

The worker process probe reclaimed the same Run with a higher fence and rejected
stale heartbeat/mutation attempts. A 31-second mTLS stream partition enforced
normal-write freshness denial, then monotonic reconciliation restored authority.
Both remain test consumers of real adapters, not production worker/gateway
composition.

A fresh lifecycle scenario with 40-second spacing between mutations passed
admission/replay, conflicting-key rejection, authenticated and cross-tenant
reads, signal/replay, cancellation/replay, ordered events, cursor resume, no
duplicate events and the database invariants. The earlier ten-second-spaced
failed signal attempt remains visible; this is correctness at low load.

The 32-client mTLS fanout sample opened all streams in 0.559 seconds, delivered
both synthetic revocations to every client, reconnected eight clients and had
zero gaps/errors. Observed p99 lag was 870 ms. Only two revocations were offered
over 80 seconds (0.025/s), so this does not qualify the production 100 changes/s
to 5,000 clients target or a statistically robust long-term tail percentile.

At 256 streams, setup took 0.881 seconds; both revocations reached all 256
clients, 64 clients reconnected, and no gaps or errors occurred. Observed p99
lag was 2.86 seconds at the same 0.025 changes/s. This demonstrates a bounded
homelab fanout operating point; 5,000 clients and production change rates
remain unqualified.

## Evidence and remaining gates

Aggregate reports, including unsuccessful attempts, are retained under
[wired results](wired-results/). Key records are the
[read operating point](wired-results/read-200-stable.json),
[write limits](wired-results/admission-1.json),
[atomic retry](wired-results/idempotency-window-uncontended.json),
[concurrent replay](wired-results/concurrent-replay.json),
[persistent crash recovery](wired-results/database-crash-host.json),
[deployed fault checks](wired-results/runtime-drained.json),
[low-rate lifecycle](wired-results/workflow-spaced.json), and
[256-stream sample](wired-results/streams-256.json). Reports contain synthetic
aggregate outcomes, without tokens, credentials, lease authority or raw
runtime payloads.

**OPS-010 and OPS-011 remain open.** The admission atomicity defect is fixed.
The 1,000 reads/s and 200 admissions/s targets are not demonstrated on this
fixture; the remaining mutation, trusted-usage/allocation, 5,000-client fanout
and long-backlog target matrix still requires intended-topology evidence.
Production-composed Valkey/Run-worker failures and gateway/HA partition/failover
qualification also remain outstanding. Local adapter probes and single-writer
crash recovery do not replace those gates. No production targets were relaxed.

All reusable Deployments, Services, Secrets and PVCs remain. The old PostgreSQL
writer stays fenced; the promoted persistent writer remains authoritative.
The separate RAM-backed regression database is retained for future tests.
The local `homelab.md` configuration is excluded from version control.

Final checks confirmed `fsync=on`, `synchronous_commit=on`, restored
`pre_auth_delay=0`, the completion-delay trigger disabled, a writable promoted
database, healthy retained deployments and zero outbox backlog.
