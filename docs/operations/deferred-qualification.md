# Qualification deferred beyond the first integration RC

On 2026-09-19, the project owner clarified that the immediate goal is an AG
release candidate for integration with ThinkPixelAR and a real Codex harness.
Phase 8 closes on implemented AG behavior, repository gates, and the available
homelab evidence. It no longer waits for production-scale performance or
components that depend on AG and do not yet exist.

The owner previously accepted the publication-lag p99 miss and then deferred
remaining throughput/dropped-request qualification. The table below also
records the cross-component and production-topology qualification deferred by
the clarified integration-RC scope. These are **deferred, not passed**.
Production [SLOs](slos.md), security contracts, and recorded failures are unchanged.

| ID | Deferred work | Revisit when | Completion evidence |
|---|---|---|---|
| DQ-001 | Sustained/burst production throughput, dropped requests and tail latency; high-rate usage/allocation/settlement; 5,000-client SSE, 100 changes/s fanout; 15-minute backlog/twice-peak drain | The intended deployment and representative integrated workload are available, before production capacity is advertised | Full offered/completed/dropped counts, latency/resource measurements, correctness invariants and backlog recovery against unchanged targets |
| DQ-002 | Crashes and recovery of the production-composed AR Run worker and real harness | AR implements the versioned AG integration and a deployable worker/harness | Kill/restart through that integration; fence stale ownership, preserve admission/replay identity, lifecycle evidence, revocation and accounting |
| DQ-003 | Production gateway durable stream application and network-partition recovery | A deployable gateway/consumer with durable checkpoints exists | Real network fault, stale-operation denial, monotonic reconciliation and durable replay without skipped events |
| DQ-004 | Intended-production host/zone partition and database HA failover | The production failure domains, replication and failover controller are selected | Failover under writes, explicit old-writer fencing, acknowledged-data/RPO checks, authoritative invariants and recovery timing |

AG's internal worker lease tests and test-consumer probes remain useful component
evidence. They are not relabeled as tests of AR, a real harness or a production
gateway. The current AG executable does not compose a Run worker or expose a
versioned worker API; adding that integration is deliberate cross-component
work, not a prerequisite to qualify unrelated AG operations.

The retained SSD database, sink and exporter share one workstation failure
domain. Their process-crash tests do not establish host-loss HA. Quiesced Pi
replica promotion establishes preservation through a replay barrier, not
automatic failover or an unplanned asynchronous zero-loss guarantee.

These deferrals do not permit a known correctness defect, unauthorized access,
fail-open behavior, duplicate admission, stale-owner mutation, allocation
expansion, evidence loss, or an unresolved critical/high security finding.
Existing tests for those properties continue to gate the integration candidate.
