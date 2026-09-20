# ADR-0009: Transactional evidence and replay-safe independent delivery

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

An API success without durable evidence undermines governance; a slow or
unavailable sink must not erase committed state. An uncertain delivery response
must preserve the same event identity across lease takeover. Historical plan
section 3.9 and Phase 7 establish the independent evidence boundary.

## Decision

Commit governance state, audit and outbox together. Export at least once to a
separately configured authenticated HTTPS sink through a replaceable adapter.
Use a closed versioned event/delivery schema, stable event IDs, deterministic
per-sink sequence/hash links, strict receipts and monotonic checkpoints.

Persist receipt, publication and checkpoint advancement atomically under a
fenced claim. Releasing/expiring ownership rotates its token but preserves the
pending delivery identity; an earlier-dated arrival cannot replace an uncertain
delivery at that sequence. A bounded local candidate cache is only a query hint:
PostgreSQL rechecks every candidate and grants authority by compare-and-set.

Keep logs, traces, metrics and client errors separate from audit evidence.
Redact secrets, tokens, raw objectives/inputs, sensitive policy data and panic
values; use closed labels and allowlisted metadata rather than dynamic identities.
Production evidence administration/storage is independent of AG authority.

## Alternatives considered

Synchronous remote delivery inside each business transaction couples governance
availability to the sink. Best-effort logs lose evidence. Claiming exactly-once
network transport ignores lost acknowledgments. Re-selecting a pending event
after lease expiry breaks replay identity and the hash chain.

## Consequences

Sink outages produce a durable backlog, not silent loss. Receipt/checkpoint
serialization and synchronous sink flushes limit throughput. Local hints improve
queries but cannot determine delivery ownership. Retained homelab colocation is
test topology, not production independent failure-domain qualification.

## Security impact

Receipt substitution, stale ownership and chain tampering fail validation.
Evidence credentials remain outside harness state; telemetry is not an alternate
channel for restricted payloads. Loss/corruption remains a release blocker.

## Operational impact

Monitor outbox count/age, retry state and checkpoints. Do not delete pending rows
to clear alerts. Qualify crash/replay and chain continuity after recovery; lower
latency or batching changes need durable correctness testing, not disabled fsync.

## References and evidence

- [Event contract](../contracts/evidence-events.md), [delivery contract](../../api/schemas/evidence-delivery-v1.json)
- [Data classification](../security/data-classification.md), [SSD recovery](../evidence/README.md#capacity)
- [Historical plan](https://github.com/bdobrica/ThinkPixelAG/blob/130fbd21ae27e72912174ce6d2c42fa0318a6989/docs/evidence/rc/history/PLAN.md), section 3.9; DATA-010, SEC-005/006/009 in the [ledger](https://github.com/bdobrica/ThinkPixelAG/blob/130fbd21ae27e72912174ce6d2c42fa0318a6989/docs/evidence/rc/history/TODO.md)
- Commits `3a6bb36`, `647e152`, `3dca78d`, `a7790e6`, `c1d63b0`, `efd47e3`
