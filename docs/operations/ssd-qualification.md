# SSD-assisted operational qualification

The 2026-09-19 follow-up moves PostgreSQL from the Pi USB flash volume to a
persistent native Docker volume on the operator's SSD workstation. The API
and OPA remain on the wired ARM64 Pi cluster. This is a different topology
from the [wired flash qualification](wired-qualification.md); production
objectives in [SLOs](slos.md) are unchanged.

## Storage migration and deployment

PostgreSQL 18.4 runs as an AMD64 container with a 4 CPU/4 GiB limit,
512 MiB shared buffers and 200 maximum connections. Its image is pinned to
`postgres:18.4-alpine3.23@sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e`.
Data and WAL use the persistent Docker volume, not a RAM drive or the Windows
workspace bind mount. `fsync` and `synchronous_commit` remain on. The Windows
WSL firewall permits the database port from only the four wired cluster nodes.

API replicas and publishers were quiesced while a logical dump was restored
across architectures. All **42 public tables** matched by row count and
content hash, and `scripts/check-restored-invariants.sql` passed. The comparison
sorts table results: parallel UNION execution may return tables in different
orders without changing their contents. Both old Pi database Deployments were
then fenced at zero replicas, retaining their PVCs. The existing database
Service now uses an external EndpointSlice. API readiness and authenticated
reads passed after restoring the original HPA behavior. See
[migration evidence](ssd-results/migration.json).

This establishes a checked storage migration, not host failover or power-loss
recovery. The private dump, credentials and operational configuration are
excluded from version control. After new writes, rollback requires data
reconciliation; restarting an old writer would lose authoritative progress.

## Admission fix

The unchanged `406db54` API on SSD passed 10 admissions/s, but the 200/s test
reached only 96.4/s. A live database sample found 93 sessions waiting on the
same immutable agent-version row, while API CPU remained low.

Source `bd148a9` changes admission to a shared version lock held through commit,
followed by a separate statement that reads current approval eligibility.
Competing version decisions still require an exclusive lock. The separate
statement also fixes a reproduced race: the old admission statement could
wait for a revocation to commit and then admit using its earlier approval
snapshot. Root resource grants are inserted in one batch, preserving dimension
bounds and aggregate rollback. Child admissions lock their parent before
creating the parent foreign-key reference, avoiding a lock-upgrade deadlock
exposed by concurrent version sharing.

New PostgreSQL regressions fail against the old source for both serialized
independent admissions and admission after a revocation committed during its
lock wait. The fixed suite passes, including the existing 32-way structural
allocation/no-oversubscription check, invalid-grant rollback and atomic
idempotency completion tests. The focused admission integration suite also
passed with the race detector. The complete `make verify` gate also passed
using the isolated RAM-backed regression database; that is software test
coverage, not storage durability evidence.

The deployed ARM64 image is
`quay.io/bdobrica/thinkpixelag@sha256:29915e9334caf66d1c2c219b572d61bbe24508715cde5ebd817240b71317b18d`.
All old API pods drained before the new image was measured. The retained HPA
allows 3–4 replicas; API containers retain 2 CPU/1 GiB limits and OPA sidecars
1 CPU/256 MiB limits in the initial samples. Later burst tuning allows one
replica on each of the four nodes and increases OPA to a 2 CPU limit with a
500m request; its memory settings and the API container limits are unchanged.
These are retained homelab settings, not changes to production manifests. Each
API database pool remains bounded at 32 connections.

## Measured samples

The external driver uses the retained synthetic identities, approved agent,
policy and payloads from [load testing](load-testing.md). Samples below last
60 seconds and use 128 workers except the 10/s warm-up, which uses 32.
Client percentiles include network time. The accompanying Prometheus reports
sample the final one-minute window at the API process and OPA boundary.

| Workload | Source | Successful / offered | Client p95 / p99 | Result |
|---|---|---:|---:|---|
| Admissions 10/s | `406db54` | 600 / 600 | 51.5 / 59.3 ms | Passed sample |
| Admissions 200/s | `406db54` | 5,898 / 12,000 | 1,765 / 2,525 ms | 6,102 generator drops; lock bottleneck |
| Admissions 200/s, initial rollout | `bd148a9` | 11,697 / 12,000 | 148.8 / 1,459 ms | 303 generator drops; tail objective failed |
| Admissions 200/s, four warm replicas | `bd148a9` | 12,000 / 12,000 | 28.9 / 37.7 ms | Passed admission sample |
| Reads 1,000/s | `bd148a9` | 60,000 / 60,000 | 12.2 / 16.4 ms | Passed read sample |

A five-minute 2,000 reads/s burst with 256 workers and four replicas on three
workers completed 503,968 of 600,000 offered requests (1,679/s), with 96,032
generator drops and client p95/p99 of 281/375 ms. It fails the burst target.
The two replicas sharing one worker compete for CPU; the API's node affinity
excluded the controller. See [burst evidence](ssd-results/read-2000-burst.json).

Allowing the fourth replica onto the controller while retaining the topology
spread constraint improved the repeated five-minute burst to 590,066/600,000
successful requests (1,967/s), with 9,934 drops and client p95/p99 of 108/179 ms.
It still fails the target. A sidecar cgroup sample showed 622 throttled CPU
periods out of 914 at its one-core limit. Each node retained CPU headroom;
controller memory was about 45%. See the
[four-node sample](ssd-results/read-2000-four-nodes.json).

Raising OPA's limit to two cores removed throttling in the sampled sidecar
and improved the five-minute burst to 595,651/600,000 successful requests
(1,985/s), with client p95/p99 of 79/129 ms. The 256-slot generator still
dropped 4,349 offered requests, so this sample also fails the burst target.
At 2,000/s the allowed 250 ms p99 budget can require 500 requests in flight;
a subsequent comparison uses 512 generator slots without changing the offered
rate or latency objectives. See [OPA tuning](ssd-results/read-2000-opa2.json).

The 512-slot comparison completed 599,230/600,000 requests (1,997/s), but still
had 770 generator drops and client p95/p99 of 198/283 ms. It fails the burst
objective and shows that increasing generator concurrency alone does not
resolve the tail. Sampled API cgroups showed no material quota throttling;
Pi temperatures were approximately 39–44 C with reported frequencies of
1.5 GHz on the controller and 1.8 GHz on workers. Further profiling is needed
before attributing this residual limit. See
[512-slot sample](ssd-results/read-2000-opa2-c512.json).



The warm admission sample's process-side p95/p99 are approximately 39/49 ms;
OPA p95/p99 are 4.8/5.4 ms. The read sample's process-side percentiles are
11/22 ms; OPA is 4.9/8.3 ms. The unsuccessful initial rollout sample remains
part of the evidence and is not replaced by the warm result.

The deployed [32-way retry check](ssd-results/concurrent-replay.json) produced
exactly one new Run and identical successful/replayed bodies. The
[HTTP lifecycle](ssd-results/workflow.json) passed admission/replay, conflicting
keys, tenant isolation, signal/replay, cancel/replay, ordered events, cursor
resume and database invariants without artificial delays between mutations.

## SSD process-crash recovery

After the flash evidence sink drained the retained backlog completely, the
SSD PostgreSQL container received SIGKILL and was explicitly restarted using
the same persistent volume. The same authorized Run response became available
in **2.716 seconds**, all 42 public table counts/content hashes matched, and
restored invariants passed. `fsync` and `synchronous_commit` were still on.
See [recovery evidence](ssd-results/crash-ssd.json). The previous flash-backed
process recovery took 92.495 seconds. Both are individual observations, not
long-term recovery percentiles; this SSD drill includes an explicit Docker
restart and does not qualify automatic HA, host loss or power failure.

## Remaining qualification

**OPS-010 and OPS-011 remain open.** Target-rate read/admission samples now
pass, but these are not full workload, burst, long-term availability or
intended-topology qualification. The trusted usage/allocation, high-rate
fanout, settlement and backlog-drain matrix still needs evidence, along with
production-composed worker/cache and HA/partition scenarios.

The warm 200/s admission sample with the Pi flash evidence sink left
approximately 17,578 pending outbox records and 211 seconds of oldest-event
age. That historical backlog drained before the PostgreSQL crash test. The
[SSD evidence follow-up](ssd-evidence-qualification.md) now preserves and
migrates the complete sink history, fixes pending-delivery replay identity,
and verifies sink-outage/exporter-restart recovery. Even with an SSD-hosted
exporter, 200 admissions/s with timely publication still fails. Admission
throughput alone does not qualify the full system. No evidence has been
discarded, and all reusable cluster resources and volumes remain.
