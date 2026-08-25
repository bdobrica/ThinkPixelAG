# Resource Accounting Contract

## Resource vector

Each resource definition has a stable name, class, unit, numeric scale, minimum, maximum, and aggregation rule.

Definitions are tenant-scoped data rather than a closed list in code, so new
dimensions can be introduced without changing the resource vector schema. Names
and units use canonical lower-case `snake_case` identifiers of at most 63
characters. Values are exact signed 64-bit coefficients at the declared decimal
scale (0–18), and must fall within the definition's inclusive nonnegative bounds.
Consumables use `SUM`; structural ceilings use `MAX` or `MIN` according to their
counter/constraint semantics. Deadline-like dimensions use `ABSOLUTE`, scale 0,
and the explicit `unix_microseconds_utc` unit. New behavioral classes or deadline
representations require a forward migration rather than being accepted as
untyped strings.

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

The admission constraint vocabulary maps `max_budget_usd_microunits`,
`max_llm_tokens`, `max_tool_calls`, `max_tool_calls_per_minute`,
`max_active_children`, `max_total_children`, and `max_delegation_depth` to the
tenant dimension names obtained by removing `max_` (the budget dimension is
`budget_usd_microunits`). Values must be exact nonnegative integers. Issuance
looks up each tenant definition, rejects a missing or out-of-range dimension,
and copies its authoritative unit and scale into the immutable grant. Each
initial balance has `available = granted`, zero direct consumption, zero open
child allocation, and state version one. Deadline remains the separately
persisted absolute run deadline until its enforcement work is introduced.

## Atomic child reservation

1. Authenticate/authorize parent operation and verify tenant, run state, revocation freshness, delegation depth, and child eligibility.
2. Sort dimension rows by stable dimension key to establish deterministic lock order.
3. Lock the parent balance/structural counter rows or perform equivalent conditional updates.
4. Verify every requested consumable fits current availability and all structural limits remain within ceiling.
5. Debit every consumable, increment structural counters, create the child grant/reservation/run/events/audit/outbox.
6. Commit all dimensions or roll back all of them.

Concurrent attempts may retry bounded serialization/deadlock failures with jitter only while the idempotency record preserves one logical outcome. PostgreSQL is authoritative; Valkey cannot approve a reservation.

The reservation persistence primitive accepts a non-empty vector of strictly
positive consumable coefficients. It rejects duplicate dimensions, sorts a
defensive copy by dimension UUID, and locks parent balance rows in that exact
order. After every row is locked and shown sufficient, checked updates move
each coefficient from `available` to `allocated_open`, advancing the balance
version. The same transaction creates the open reservation, copies the
authoritative unit and scale into its immutable item vector and child grant,
and initializes each child balance with full availability. A missing,
non-consumable, insufficient, overflowing, or concurrently changed dimension
rolls back the complete vector. RES-004 composes this primitive with governed
child admission and the structural child/depth checks; callers cannot use the
persistence primitive as an authorization boundary.

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
