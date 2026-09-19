# Durable evidence on SSD

Current status: [Phase 8 integration-RC closeout](phase-8-closeout.md) supersedes
the earlier completion blockers below under the owner-approved
[qualification deferrals](deferred-qualification.md). Historical results and
production targets remain unchanged.

The 2026-09-19 follow-up moves the test evidence sink onto persistent SSD
storage and fixes evidence replay identity across a released or expired claim.
It extends [SSD PostgreSQL qualification](ssd-qualification.md). Production
[SLOs](slos.md) remain unchanged. The migration and recovery checks pass;
200 admissions/s with timely publication still fails on this deployment.

## Migration and retained topology

All API publishers and the old sink were quiesced before copying the receipt
file. Its **30,407 records** matched by whole-file SHA-256 across the source,
private copy and destination. Sequences, predecessor links and the database
checkpoint matched. The existing Kubernetes Service now routes HTTPS to the
SSD workstation, using the same certificate, issuer and sink credential.
See [migration aggregates](ssd-results/sink-migration.json).

The original fixture Deployment remains at zero with its PVC retained. Its
history is now stale; restarting it behind the Service would be unsafe.
The SSD fixture has a 2 CPU/1 GiB limit, a persistent native Docker volume,
read-only root filesystem, non-root user and dropped capabilities. Its
receipt append still calls `fsync` before acknowledging. PostgreSQL retains
`fsync=on`, `synchronous_commit=on` and its existing persistent volume.
Neither durable store uses RAM or the Windows workspace bind mount. The
scoped Windows firewall allows the two service ports from the four wired
cluster addresses. Private configuration and receipt payloads are not committed.

The API runs on four ARM64 Pis with 2 CPU/1 GiB limits, OPA sidecars with
2 CPU/256 MiB limits, and database pools capped at 32 each. The HPA minimum
was temporarily raised from three to four to hold replica count during
measurement; its original minimum is restored after qualification.

## Export correctness and query fix

Source `c1d63b0` keeps at most 64 candidate IDs in an exporter-local cache.
PostgreSQL rechecks every candidate and grants ownership through an atomic
checkpoint compare-and-set. Hints are non-authoritative. This avoids searching
all already-receipted history for each delivery: the earlier single-candidate
search examined 30,407 receipt entries and took 62.4 ms in a read-only plan
sample. A later pending-claim query sample took 0.34 ms. These are diagnostic
samples, not full-run SQL percentiles.

Release rotates the ownership token and expires the lease while retaining
the pending event. After an uncertain sink response, lease expiry, or exporter
restart, a newly arrived event—even one with an earlier occurrence time—cannot
replace the pending delivery at the same sequence and hash. Previous owners
remain fenced. Receipt append, publication marking and checkpoint advancement
remain atomic. No migration, dependency or wire schema change was needed.

The new regression fails against the preceding source and passes the fix.
It covers released/expired ownership, process-local cache loss, an earlier-dated
arrival, stale-owner completion, multiple store instances, receipt continuity
and eventual delivery of late arrivals. The focused PostgreSQL test and its
race variant pass; the complete `make verify` gate also passes. The isolated
RAM-backed database used by that gate establishes software behavior, not
persistent-storage durability.

The deployed ARM64 API image is
`quay.io/bdobrica/thinkpixelag@sha256:87d57e4785740c4e05fddd6b4626ade2343c5a780a8e8de77f5aa9e663054aa3`.
The test fixture executable remains at `bd148a9`. A placement comparison uses
the unmodified `c1d63b0` production executable on the SSD host, with runtime
routes unconfigured, a loopback-only HTTP listener, no published port, a
four-connection database pool and a 2 CPU/512 MiB limit. This exporter shares
a Docker network with PostgreSQL and the HTTPS fixture; only the public test
CA is mounted. API sink settings are empty in that placement, and all prior
Pi publisher pods drained before measurement. The local AMD64 image is
`sha256:f00585a1963739047e01c02a5fdb818cc8c2dc9c704c5423ed3eee2c83a11042`.

## Throughput and publication

The external driver uses the same retained synthetic policy and identities
as the preceding qualification. There are no concurrent independent load or
build jobs during these samples. The 200/s tests use 128 driver workers;
publication is observed approximately once per second until the backlog drains.
Every admitted request succeeded, but scheduler/concurrency drops still fail
the offered-load target.

| Placement / source | Duration | Successful / offered | Client p95 / p99 | Publication p99 | Drain after load |
|---|---:|---:|---:|---:|---:|
| SSD sink, Pi exporter `bd148a9` | 30 s | 5,718 / 6,000 | 490 / 1,931 ms | Not measured | Not measured |
| SSD sink, Pi exporter `c1d63b0` | 60 s | 11,294 / 12,000 | 661 / 1,904 ms | 141.0 s | 142.5 s |
| SSD sink and exporter, `c1d63b0` | 60 s | 9,587 / 12,000 | 1,342 / 4,907 ms | 107.7 s | 109.2 s |

See the [initial sink sample](ssd-results/sink-ssd-admission-200.json),
[Pi exporter requests](ssd-results/sink-fixed-200.json),
[Pi publication series](ssd-results/sink-fixed-200-publication.json),
[SSD exporter requests](ssd-results/sink-colocated-200.json), and
[SSD publication series](ssd-results/sink-colocated-200-publication.json).
Matching `-metrics.json` files record the final one-minute Prometheus window;
it is not a full-run process latency distribution.

Publication percentiles cover exact receipt-sequence boundaries between the
empty-backlog checkpoints, including the driver's one smoke admission. There
are respectively 11,295 and 9,588 receipts, with no missing outbox records.
An initial occurrence-time filter omitted boundary events because application
and database clocks differ; the reports retain that original count and replace
the cohort with checkpoint boundaries. Lag is sink acceptance time minus
application occurrence time, so small residual clock offsets remain in it.
Drain duration also includes final metrics collection and observation delay.
Neither target-rate sample meets the 30-second publication objective.

A separate `pg_test_fsync -s 1` diagnostic used a disposable file on the native
ext4 SSD volume after the measured backlog drained. One 8 KiB write plus
`fdatasync` averaged 2.706 ms; `fsync` averaged 6.014 ms. See
[raw storage results](ssd-results/ssd-fsync.txt). This short file test is not
an application latency distribution. It identifies a material cost in a
serial delivery path containing a claim commit, a synchronous sink append and
a receipt/checkpoint commit. Colocation alone is insufficient. Durable batching
or lower-latency durable storage needs qualification before asserting the
production rate; disabling flushes or using RAM would invalidate that evidence.

## Conservative operating point

A five-minute confirmation at **25 admissions/s** completed **7,500/7,500**
requests, with zero drops, transport errors or reported invariant failures.
Client p95/p99 were **36.4/52.8 ms**. All **7,501** events including smoke had
receipts; publication p95/p99 were **1.074/1.191 seconds**, and the final backlog
was zero. See [requests](ssd-results/sink-colocated-25-confirmation.json),
[publication](ssd-results/sink-colocated-25-confirmation-publication.json), and
[final-window metrics](ssd-results/sink-colocated-25-confirmation-metrics.json).
This is a confirmed conservative point for further correctness drills, not a
measured maximum, production capacity qualification, or replacement target.

## Sink outage and exporter restart

The fixture process was paused while the API accepted 1,500/1,500 scheduled
admissions at 50/s for 30 seconds (plus the driver's smoke admission). The
checkpoint did not advance, and 1,501 events remained durable in the outbox.
The exporter was then killed with SIGKILL and explicitly restarted while the
sink remained paused. After 12 seconds, the same pending delivery identity
was still retained. The existing fixture was then unpaused.

All 1,501 records drained in **21.087 seconds**, an observed average of
71.181/s. All retained outbox rows had receipts, the checkpoint advanced by
exactly 1,501, and the complete receipt chain had zero sequence or predecessor
breaks. `scripts/check-restored-invariants.sql` passed and the authenticated
API read returned 200. See [recovery results](ssd-results/ssd-sink-recovery.json).
This tests the composed production evidence exporter and real durable sink.
It does not qualify the Run worker, host/zone failover, or the production
15-minute backlog and twice-peak drain requirement.

## Sink persistence and retained state

With no pending outbox records, the fixture received SIGKILL and was explicitly
restarted on the same SSD volume. All **66,011** records had unique event IDs,
contiguous sequences and valid predecessor links, and the sink head matched
PostgreSQL. Replaying the last delivery returned the exact original receipt
within **3.622 seconds**. The whole receipt file hash was unchanged across the
crash and replay. Startup also validates each persisted delivery/receipt pair.
See [persistence results](ssd-results/ssd-sink-persistence.json). This is process
recovery with an explicit restart, not a power-loss or HA claim.

The SSD database, fixture and colocated exporter remain running for reuse.
Old Pi database and fixture writers remain fenced at zero; all PVCs, Docker
volumes and helper resources are retained. The original HPA minimum of three
is restored. Final checks show an authenticated read succeeds, the outbox is
empty, restored database invariants pass, and durability settings remain on.
See [final state](ssd-results/sink-final-state.json).

**OPS-010 and OPS-011 remain open.** This follow-up closes the sink migration
and these evidence-recovery checks. It does not meet sustained target-rate
publication or twice-peak drain, and it does not substitute for the remaining
burst, usage/allocation/settlement, large-fanout, Run-worker/cache composition,
or intended-topology failover/partition qualification. The fixture and exporter
share the workstation failure domain with PostgreSQL.

## Accepted homelab exception

On 2026-09-19, the project owner accepted the observed publication-lag p99
miss as an exception for Phase 8 homelab closeout. This removes that percentile
miss as a closeout blocker; the measured failures remain in the evidence and
production SLOs are unchanged. The exception does not qualify dropped requests,
sustained throughput, backlog-drain capacity, unexecuted workload scenarios,
or failover/recovery behavior. Evidence preservation, replay correctness and
security freshness requirements remain required.
