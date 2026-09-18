# OPS-011 retained ARM64 resilience evidence

Qualification date: 2026-09-18 UTC. **OPS-011 remains open.** These results
extend the repository-local matrix with actual homelab faults and real-adapter
probes. Production targets are unchanged. Neither the optional Valkey cache nor
the Run worker is composed into the deployed executable; component probes do
not substitute for those remaining production-composition gates.

## Environment and provenance

The retained OPS-010 namespace has three API replicas with OPA sidecars on a
four-node ARM64 Raspberry Pi 4 K3s cluster, WiFi networking and USB flash storage.
Workers have 8 GiB RAM and the controller 4 GiB. The API image is unchanged:
`sha256:7f52eba9bd8c31d6e6c499032561336f1e53359990626584967f1edecb9aeef9`.
Its source and earlier qualification are recorded in
[homelab evidence](homelab-qualification.md). This change adds the opt-in
[retained runner and probes](resilience-testing.md), without production runtime
or public contract changes.

Dependencies:

- PostgreSQL `postgres:18.4-alpine3.23@sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e`.
- OPA `openpolicyagent/opa:1.19.0-debug@sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4`.
- Valkey `valkey/valkey:9.1.1-alpine3.24@sha256:ee91f7a174ac4d6a6b0685b3a60e321f0a9dbbb691f9b0e285be2ba1d1be8328`.

The new standby used a separate worker and 4 GiB PVC, physical streaming
replication, a dedicated SCRAM replication role and a bounded physical slot.
The original database volume is 12 GiB. Durability settings were not weakened.
Valkey is authenticated and deliberately nonpersistent. A separate real OPA
instance serves component probes. All workloads use synthetic fixture identities.

## Executed scenarios

| Scenario | Observed result | Evidence and scope |
| --- | --- | --- |
| OPA unavailable | Protected reads: 503, 503, transport failure; no ALLOW; restored read passed | [Runtime report](ops011-results/runtime.json), deployed API |
| Malformed OPA decision | Protected reads: transport failure, 503, transport failure; no ALLOW; restored read passed | Same report; actual malformed decision output |
| Database connection latency | Five-second pre-auth delay and closed existing connections produced three HTTP 500 responses; readiness recovered in 4.037 seconds after restoration | [Latency report](ops011-results/latency.json); connection delay, not arbitrary SQL/storage latency |
| Pod eviction | Actual Kubernetes Eviction API, replacement readiness and protected read passed | [Disruption report](ops011-results/disruptions.json) |
| Rolling restart | Initial sample failed 3/20 reads; after fixture drain repair, repeat passed 20/20 | Initial failure retained in runtime report; repeat in disruption report |
| Valkey loss/recovery | Cache-down evaluation fell back to real OPA; restored cache produced a remote hit; authoritative request bypassed cached ALLOW | [Loss](ops011-results/cache-down.json), [recovery](ops011-results/cache-healthy.json); production adapters with synthetic active-policy metadata |
| Worker process crash | SIGKILL after real DB claim; successor reclaimed the same Run after lease expiry with a higher fence; stale heartbeat and mutation rejected; successor heartbeat accepted | [Worker report](ops011-results/worker.json); test process uses actual application/DB adapters, no AR harness |
| Stream partition | Real mTLS stream connected then disconnected for 31 seconds; normal-write freshness boundary denied; monotonic authoritative reconciliation restored authorization | [Partition report](ops011-results/partition.json); ephemeral test consumer, not production gateway or cross-zone networking |
| PostgreSQL promotion | All 41 authoritative tables matched before and after promotion; restored-state invariants passed; same Run response after API reconnection | [Promotion report](ops011-results/promotion.json); 191.257-second planned interruption |

The fixture omitted the production API manifest's five-second preStop drain.
Preparation now applies it, keeps the OPA sidecar alive for ten seconds, uses
a 45-second termination grace period and a two-available-replica PDB. The
production manifest itself was not changed. The successful 20-request repeat
is a short sample, not continuous availability or production latency proof.

One immediate cache recovery probe failed after Deployment readiness; a later
client probe passed. The runner documentation requires client-side reachability
before recovery checks. Kubernetes readiness alone did not establish client
connectivity in that attempt. Transport failures during OPA faults remain
visible rather than being reported as clean HTTP errors.

Promotion was quiesced and operator-driven: stop API writers, capture table
hashes and flushed WAL position, await standby replay, compare rows, stop the
old primary, promote, redirect the existing writer Service, compare rows again,
check invariants, restore API replicas. This demonstrates preservation through
the captured replay barrier. It does not demonstrate automatic HA, unplanned
asynchronous zero-loss failover, or production RTO. The interruption includes
verification work while the API was deliberately stopped.

## Retained state and outstanding gates

`Service/postgres` now selects **Deployment/ops011-standby**, the promoted writer
on `k3spi-02`. Original **Deployment/postgres remains at zero** with its PVC
retained. Do not restart that old writer; it requires explicit rewind or
reinitialization before rejoining. The API has three replicas. Standby storage,
replication credentials, Valkey, probe OPA and disruption budget remain for reuse.
Private promotion markers and worker lease checkpoints stay outside version
control. All Deployments, Services, Secrets and PVCs were retained.

Remaining OPS-011 gates are production-composed Valkey and Run-worker crash
qualification, intended-topology stream partitions and failover qualification.
The separate OPS-010 admission/idempotency atomicity defect remains unresolved;
these probes do not retry uncertain admissions or claim that it is fixed.

## Repository verification

`make test-resilience` passed, including three promotion safety regressions and
the OPA/cache/freshness/worker/evidence boundary tests. Promotion safety tests
also passed after hardening the private marker with exclusive creation and file
and directory synchronization. Full `make verify` passed against the separate
`ops_verify` database through the existing Service after promotion: generation,
format/static checks, unit/race tests, 27 policy cases, PostgreSQL integration
(157.485 seconds), end-to-end PostgreSQL tests (69.350 seconds), adversarial
security tests, Compose/Kubernetes validation, vulnerability checks, build and
container smoke. The tested source is the implementation committed alongside
this report, based on `982ecba`; the cluster API image remained unchanged.
Reports contain aggregate outcomes only, with no tokens, credentials, lease
authority or raw runtime payloads.
