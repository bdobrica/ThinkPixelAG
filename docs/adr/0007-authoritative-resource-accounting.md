# ADR-0007: Transactional resource conservation

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Parallel child admissions, duplicate metering and crash recovery can oversubscribe
or double-credit a parent's budget. A runtime may enforce an envelope but cannot
be its authority. Historical plan section 3.6 defines conservation as the central
invariant, with consumable, structural and deadline-like dimensions.

## Decision

PostgreSQL atomically owns immutable grants, mutable balances, reservations,
append-only trusted consumption, extensions and settlement. For each consumable
dimension, open-child allocation plus direct consumption plus availability equals
the granted budget. Reserve all dimensions or none with deterministic lock order;
enforce active/total child counts and delegation depth in the admission transaction.

Use checked exact arithmetic and canonical units, never floating point or negative
consumption. Trusted producer/event identity gives metering replay semantics.
Settlement closes a reservation and returns unused capacity exactly once; bounded
reconciliation handles terminal/expired orphans without double credit. Exhaustion
and authorized additive extensions follow the documented lifecycle transitions.

Valkey may accelerate exhausted throughput-window hints but never grant spending
or replace PostgreSQL rate/accounting checks. Loss or malformed hints fall back
to authoritative checks. Runtime composition enables only these throughput hints,
not the optional policy decision cache.

## Alternatives considered

Harness-owned balances, eventually consistent counters and partial vector
reservations violate conservation under races. Floating point makes exact replay
and limits ambiguous. Cache-only enforcement creates authority loss on cache
failure. Per-dimension partial success would require unsafe compensation.

## Consequences

The database bears contention and serialization cost; increasing API replicas
does not eliminate it. Extensible dimensions still require explicit class/unit
semantics. Orphan reconciliation and replay are part of correctness, not cleanup.

## Security impact

Callers cannot self-extend structural or consumable authority. Trusted usage and
settlement require dedicated roles, policy, tenant binding and current state.
Resource mutations and their evidence share a transaction.

## Operational impact

Property/fuzz and real-PostgreSQL concurrency tests establish conservation.
Production high-rate usage/settlement capacity remains DQ-001. Monitor lock/pool
waits, exhaustion, settlement age and outbox lag before scaling concurrency.

## References and evidence

- [Resource contract](../contracts/resource-accounting.md), [Phase 5 evidence](../evidence/implementation/phase-5-evidence.md)
- [Deployed Valkey drill](../evidence/homelab/phase-8-closeout.md)
- [Historical plan](../evidence/rc/history/PLAN.md), section 3.6; RES-001–014 in the [ledger](../evidence/rc/history/TODO.md)
- Commits `8a37a87`, `996d48b`, `de85483`, `e95c5f1`, `44a247b`, `014954a`, `30ff25f`
