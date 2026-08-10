# Resource Accounting Contract

## Resource vector

Each resource definition has a stable name, class, unit, numeric scale, minimum, maximum, and aggregation rule.

| Class | Semantics | Examples | Expansion rule |
|---|---|---|---|
| Consumable | finite quantity reserved, metered, settled, reclaimed | `usd_microunits`, `llm_tokens`, `tool_calls`, `cpu_milliseconds`, `egress_bytes` | only an authorized additive governance ledger entry |
| Structural | hard topology/concurrency/rate ceiling | calls/minute, active children, total children, delegation depth | child/caller can only narrow; authorized governance action may change root policy limit |
| Deadline | absolute UTC instant after which new work stops | `run_deadline` | only authorized governance extension; harness cannot extend |

Money uses signed 64-bit integer micro-units (`1 USD = 1,000,000`) unless an ADR selects a decimal representation before implementation. Counts/bytes/milliseconds are nonnegative checked 64-bit integers. Input above the declared maximum, arithmetic overflow, fractional count, negative quantity, or unknown unit is rejected before mutation.

## Constraint resolution

For each ceiling, the admitted value is the strictest of agent-version approval, tenant policy, caller-requested constraint when present, parent availability/structure, and deployment safety maximum. Absence of a caller constraint never means unlimited. A caller or child can narrow but cannot expand authority.

## Authoritative equations

For a run `r` and consumable dimension `d`:

```text
allocated_open_children(r,d)
  + direct_consumed(r,d)
  + available(r,d)
  = granted(r,d)
```

For child reservation `c,d`:

```text
0 <= consumed(c,d) <= reserved(c,d)
unused(c,d) = reserved(c,d) - consumed(c,d)
```

All terms are nonnegative and use the dimension's explicit unit. The authoritative ledger plus current balance rows must be sufficient to recompute and verify these equations.

## Root issuance

Run admission resolves the vector through policy and commits the immutable grant, initial balance, run, version snapshot, audit, and outbox records atomically. If any dimension or record fails, no run is admitted.

## Atomic child reservation

1. Authenticate/authorize parent operation and verify tenant, run state, revocation freshness, delegation depth, and child eligibility.
2. Sort dimension rows by stable dimension key to establish deterministic lock order.
3. Lock the parent balance/structural counter rows or perform equivalent conditional updates.
4. Verify every requested consumable fits current availability and all structural limits remain within ceiling.
5. Debit every consumable, increment structural counters, create the child grant/reservation/run/events/audit/outbox.
6. Commit all dimensions or roll back all of them.

Concurrent attempts may retry bounded serialization/deadlock failures with jitter only while the idempotency record preserves one logical outcome. PostgreSQL is authoritative; Valkey cannot approve a reservation.

## Trusted metering

- Only identities granted the meter action for the run/resource source may submit usage.
- Each event is uniquely keyed by `(tenant, producer, source_event_id)` and includes run, resource, quantity, observed time, schema version, and integrity/authentication context.
- Duplicate identical events return the original result. A duplicate ID with different content is a conflict and security signal.
- Events are append-only, nonnegative, unit-valid, and applied transactionally.
- A harness's terminal summary is not authoritative unless it is also a trusted meter source.
- Late usage after settlement is rejected/quarantined and alerted; it never silently makes the parent equation negative.

## Exhaustion

Consumption reaching a ceiling prevents further grants/authorized consumption for that dimension and triggers `BUDGET_EXHAUSTED`. Enforcement must not depend only on asynchronous metrics. Policy chooses `PAUSED_FOR_BUDGET` or `FAILED_BUDGET`; a paused run performs no governed work until an additive extension commits.

## Extension

An extension is a positive, typed ledger entry referencing actor, policy decision, reason, approval when required, prior/new grant, and audit/outbox evidence. It atomically increases granted and available quantity (or adjusts deadline/structural root ceiling as expressly approved). It never rewrites the original grant or consumption. A run/worker identity cannot approve its own extension.

## Settlement and reclaim

Settlement locks the open reservation and relevant parent balances. It validates trusted consumption, inserts a unique settlement, marks the reservation closed, decrements open-child allocation/active-child count, and credits unused quantity to parent availability in one transaction. A repeated/concurrent settlement reads and returns the established result without another credit.

Total-child count and delegation history do not decrease. Active-child count decreases once. The parent credit is immediately authoritative even if cache/notifications lag.

## Reconciliation

A replay-safe worker finds terminal children with open reservations and expired abandoned reservations. It uses the same settlement operation and unique constraints. It does not infer usage from untrusted status. Unknown final usage is handled by deployment policy conservatively (for example, hold allocation pending trusted reconciliation), never fabricated as zero merely to free budget.

## Required properties

- Conservation equation holds after every committed operation.
- Multi-resource reservations are all-or-nothing.
- No interleaving oversubscribes consumables or structural limits.
- Duplicate metering and settlement are idempotent; mismatched duplicates conflict.
- Overflow/underflow and negative balances are impossible at domain and database layers.
- Deadline and structural authority cannot be expanded by caller, child, cache, or worker.
- Reconciliation can run concurrently and after crashes without lost or duplicated credit.
