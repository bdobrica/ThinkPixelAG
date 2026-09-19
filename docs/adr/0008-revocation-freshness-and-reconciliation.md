# ADR-0008: Monotonic revocation and bounded freshness

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Push streams can disconnect, duplicate or skip events. Cached authorization must
not turn a partition, clock adjustment or process restart into continuing revoked
access. Historical plan section 3.7 and Phase 6 define the authority/freshness split.

## Decision

Commit each supported revocation/lift with applicable monotonic epochs, ordered
log and evidence atomically. Supported scopes cover Run, agent, version, Skill
digest, principal, tenant, tool, policy version and global authority. Lifting a
revocation appends a new change; it does not decrement epochs or erase history.

Use authenticated, tenant-bound SSE cursors and bounded queues/write times.
Durably apply consumer state with its cursor. Detect gaps and reconcile against
authoritative delta/snapshot state; a service-side checkpoint is not proof that
a consumer durably applied an event. Production gateway composition remains
separate from the test consumer.

Track freshness per instance/tenant with monotonic elapsed time. Restart begins
unknown. Sparse epochs merge without regression; duplicates cannot refresh
authority, and gaps deny until reconciliation. High-risk operations require
live authority and zero cached decision TTL. Normal-write and sensitive-read
bounds are at most 30/60 seconds; stricter finite policy bounds are allowed.

Policy/epoch/input versions and local invalidation generations make stale cache
entries unreachable. Committed invalidation never depends on deleting remote
Valkey keys. Readiness and bounded-label metrics expose lag, age and gaps.

## Alternatives considered

Push alone cannot repair a lost stream. Wall-clock age permits backward-clock
extension. Trusting duplicate arrivals or checkpoints as proof of current state
conceals gaps. Enumerating cache keys makes invalidation depend on cache health.

## Consequences

Partitions can reduce availability by design. Consumers need atomic durable
state/cursor storage and a reconciliation path. High-risk authorization retains
database cost even when optional caches are healthy.

## Security impact

Unknown actions, stale/missing authority, epoch regression and unhealthy clocks
fail closed. Cached ALLOW never substitutes for required live authority.

## Operational impact

Monitor reconciliation age and sequence lag; recover through authoritative
snapshots, not cursor jumps. The small connected-gateway propagation sample does
not qualify 5,000-client production fanout or network failure domains.

## References and evidence

- [Revocation contract](../contracts/revocation.md), [Phase 6 evidence](../phase-6-evidence.md)
- [ADR-0004](0004-policy-evaluation-and-activation.md), [deferrals](../operations/deferred-qualification.md)
- [Historical plan](../releases/history/PLAN.md), section 3.7; REV-001–011 in the [ledger](../releases/history/TODO.md)
- Commits `72ff9de`, `8409b99`, `2398385`, `c7856ff`, `90f913a`, `bf8eae1`, `57888c2`
