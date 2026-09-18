# OPS-010 ARM64 diagnostic qualification — 2026-09-18

Follow-up: [homelab operating envelope and recovery](homelab-qualification.md) records hardware-limited measurements separately from production qualification.

Status: **not qualified; OPS-010 remains open**. The governed executable and
external HTTP diagnostic driver are implemented. Initial measurements expose
storage and throughput limits; this report is not production capacity proof.

Implementation and verification source commit: `562cc4df241e906749b824ae0f5464a8c7a1e5bc`.
The cluster images were built before that commit was created, with the
development labels and digests below. Subsequent edits only record evidence.

## Environment and artifacts

Disposable single-site Kubernetes ARM64 cluster: three Raspberry Pi 4 workers
with 8 GiB RAM each, one 4 GiB controller, nominal 32 GB USB disks. API has three
replicas spread across workers, each with a loopback OPA sidecar. PostgreSQL
18.4 uses a 12 GiB local-path PVC on USB/ext4; the evidence fixture has a 4 GiB
PVC. Kubernetes version is v1.36.4+k3s1. The load generator runs outside the
cluster over the LAN. There is no zone diversity or HA database.

API request/limit is 250m CPU/256 MiB and 2 CPU/1 GiB; OPA is 250m/128 MiB and
1 CPU/256 MiB. Pools are 32 connections per replica, PostgreSQL max_connections
160 and shared_buffers 512 MiB. OPA HTTP connections are bounded to 32 per API
process in the final image. Optional decision caching/Valkey is disabled;
every decision checks live revocation authority. Thus this is not a qualification
of the cached-read latency SLO. The database owner is used for this isolated
fixture; least-privilege production deployment is not qualified here.

The fixture contains two tenants, one approved agent/version, 5000 distinct
workload principals/certificates, three human principals, five resource
dimensions, a 5721-byte repository policy bundle, and a 1 KiB synthetic Run input.
Policy signatures use a test-only software authority. Credentials and private
connection details are excluded from this report. Resources and persistent
state are retained for subsequent tests; local operator notes are untracked.

Image digests (development builds from the implementation accompanying this
report, labeled `uncommitted-ops010`, not release attestations):

- Initial API: `sha256:292d7a37b15d5c63d6ccba4bf0598a3d0c404db74bd279d26f0ee64cd7e7c199`.
- Final API: `sha256:791f3ecb6c5e1b99c077104107ba9f2cd5831e117f107cb0bd52c37de25de35a`.
- Fixture: `sha256:954b0b251600a2676d579744986d4cddcbe7da191747c9bd5ebf40ea6ba98577`.
- PostgreSQL: `sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e`.
- OPA 1.19.0: `sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4`.

The fixture image predates a local seed SQL parameter-cast repair; the corrected
seed ran locally against the cluster database. Serving code is unchanged.
The final API adds the stream-deadline, route-label, and OPA connection-bound
fixes discovered during diagnostics. Driver source lives in
`test/operations/load`; invocation details are in [load testing](load-testing.md).
Each measurement begins after admission/replay/read/isolation smoke, not a
formal steady-state warm-up. No other load scenario ran concurrently. The
integration gate began on a
separate database on the same PostgreSQL instance during the tail of the
stream attempt; that attempt is a saturation diagnostic, not an isolated
capacity measurement. PostgreSQL CPU saturation was observed before that overlap.

Aggregate machine-readable reports are retained in
[ops010-results](ops010-results/). No identities, credentials, or payloads are
included.

## Measurements

Client latency includes network; server histogram estimates are separate.
Drops are unsent scheduled requests (late scheduler or saturated worker bound).
The driver counts failures rather than lowering its offered-rate denominator.

| Scenario | Offered | Successful | Drops | Client p95 / p99 | Outcome |
|---|---:|---:|---:|---:|---|
| Initial admission, 20/s for 30s | 600 | 2 | 380 | 15001 / 15015 ms | 148 HTTP 500, 70 transport errors |
| Admission diagnostic, 1/s for 10s | 10 | 4 | 6 | 10350 / 10350 ms | Durable writes already too slow |
| Admission diagnostic, 5/s for 30s | 150 | 34 | 88 | 11487 / 11844 ms | 28 HTTP 500 |
| Initial read, 1000/s for 60s, 128 workers | 60000 | 33232 | 26768 | 389 / 511 ms | 553 successful/s |
| Final read, 1000/s for 60s, 128 workers | 60000 | 49669 | 10331 | 254 / 418 ms | 826 successful/s |

Final read began at 19:17:17 UTC. A one-minute Prometheus sample at approximately
19:18:48 UTC reported process HTTP p95/p99 243/405 ms, OPA 40.7/65.1 ms,
aggregate API CPU 1.66 cores and resident memory 135.6 MB. All three scrape
targets were up. Outbox pending/oldest age were zero at that sample. These are
snapshot/window estimates, not full-run maxima or publication-lag percentiles.
Initial method-prefixed route labels were incorrectly classified as `unknown`;
this was repaired and regression-tested before the final read measurement.

All smoke checks passed: real admission, byte-identical idempotent replay,
current-tenant read, foreign-tenant denial, forged-token denial. Completed
admission response IDs showed no duplicates. This does not establish the
outcome of every timed-out request or complete security qualification.

## Stream diagnostic

The final image was driven with `-mode streams -clients 5000 -rate 1
-workers 4 -duration 30s`. Setup uses a bounded five-minute allowance in addition
to the measurement duration. Peak active connections were 4093, below 5000;
1250 reconnects were attempted, with 1831 total stream transport/setup errors.
No revocations were admitted (29 HTTP 503 responses, one dropped request), so
zero delivered events and zero lag do **not** establish propagation correctness.
No epoch regression was observed because no events were delivered. This attempt
failed, and does not qualify the required 100 changes/s fanout. At an early
setup sample, PostgreSQL used 2997m CPU against its 3-CPU limit; API+OPA pods
used roughly 1.4 cores each and 112–114 MiB. Pods remained healthy.

## Storage diagnosis and limits

Live PostgreSQL activity during the 5/s diagnostic showed WALSync waits up to
6.2 seconds, WALWrite waits of 4–6 seconds, and consequent transaction/tuple
waits on agent versions and evidence checkpoints. Deadlock count was zero.
The volume was about 1% occupied. `fsync=on`, `synchronous_commit=on`, and
`wal_sync_method=fdatasync` were retained.

`pg_test_fsync -f /var/lib/postgresql/ops010-fsync -s 1` on the same PVC measured
one-block fdatasync at about 7.45 ms and fsync at 20.8 ms, with extreme variance
in synchronous-open patterns: one two-block sample took 18.5 seconds/operation.
This is a short storage diagnostic, not a disk benchmark distribution. It
corroborates the observed durable-write stalls. Durability was not weakened to
improve throughput. Repair/replace the storage path before sustained mutation,
outbox recovery, and fanout qualification; current data does not justify a safe
production admission rate, even at 1/s.

## Outstanding qualification

The full 200/s admission and five-minute 400/s burst, 500/s lifecycle, 2000/s
trusted usage, contested child allocation capacity, 100/s global revocation
fanout, complete gateway reconciliation, terminal settlement lag, and a
15-minute outbox backlog/recovery are not qualified. Integration invariant tests
are correctness evidence only. No targets were lowered to mark this task done.

## Verification

Focused runtime/config/policy/HTTP/driver unit and race checks passed. The policy
suite passed 27/27 tests. The real PostgreSQL evidence-delivery integration test
passed, including atomic receipt/checkpoint/publication and stale-claim fencing.
`scripts/check-restored-invariants.sql` passed against the load database after
the stream attempt (schema row, nonnegative epochs/checkpoints, allocation
conservation, and evidence-to-outbox links). The complete real-PostgreSQL
integration pass took 177 seconds, including concurrent child-allocation
correctness checks. The first end-to-end attempt lost its kubectl port-forward;
verification resumed through a direct SSH tunnel to the retained Service. A
subsequent lifecycle test exposed a pre-existing 25 ms stream-helper deadline
that is unsuitable for remote database testing. The helper now stops on a
flushed event with a five-second safety timeout; latency objectives are
unchanged. The corrected end-to-end suite passed in 123 seconds; the focused
security database suite passed in 1.45 seconds. Generation/lint/OpenAPI, unit,
race, 27/27 Rego, Compose/Kubernetes rendering, dependency policy, and reachable
vulnerability, license, static/image build, and hardened-container smoke checks
passed. All constituent `make verify` gates passed after the forwarding repair
and functional-test correction; this was not an uninterrupted first-pass gate.
Final generation/format checks passed after staging the complete change.
