# Phase 5 Resource Governance Evidence

Phase 5 exited on 2026-08-26 after qualification of typed resource issuance,
atomic parent/child allocation, trusted metering, governed exhaustion and
extension, terminal settlement, and orphan reconciliation. Commit `edf1dec` is
the composed resource-lifecycle qualification baseline. The RES-014
phase-completion commit containing this document is the audited tree; a
subsequent ledger-only commit records its short SHA.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.6 linux/amd64` |
| PostgreSQL | `18.4` on the pinned `postgres:18.4-alpine3.23` image |
| OPA | `1.19.0` |
| Race detector | enabled for the complete Go race suite and focused resource workflow |

The final gate used loopback-only development dependencies and disposable test
data. No production service or data participated. PostgreSQL remained the
authority throughout; optional Valkey rate markers were qualified separately
as conservative, integrity-protected denial acceleration.

## Qualified resource model

Tenant definitions classify exact checked quantities as consumable, structural,
or deadline-like. Root admission maps policy constraints to definitions and
atomically persists immutable grants plus mutable balances. Child admission
locks the parent envelope and dimension rows deterministically, enforces active
and lifetime child ceilings and delegation depth, and commits the child run,
reservation, resource vector, and evidence as one unit.

Authenticated trusted producers append idempotent usage events. PostgreSQL
updates usage, fixed-minute structural counters, and balances atomically; an
identical source event replays and changed reuse conflicts. Exact exhaustion
fences the worker and follows policy into a paused extension workflow or
terminal budget failure. Approved extensions are additive ledger actions and
cannot rewrite original grants or be self-approved by the requester.

Terminal settlement derives consumption from durable child balances, closes
each reservation once, rolls child consumption into the parent, and immediately
returns unused capacity. Reconciliation uses `SKIP LOCKED` and the same unique
closure boundary for terminal or expired children. It fences expired live work
before reclaim and retains allocations with open descendants for leaf-first
processing, so retry and concurrent replicas cannot create credit.

The normative equations, units, authority boundaries, and operation-specific
rules are in [Resource accounting](contracts/resource-accounting.md). The
corresponding relational constraints and locking strategy are in
[Authoritative PostgreSQL schema](architecture/database-schema.md).

## Conservation and concurrency evidence

Randomized model tests exercised reservation, usage, leaf settlement, and
checked signed-64-bit arithmetic against the conservation equation:

```text
available + direct_consumed + allocated_open = effective_grant
```

The real-PostgreSQL stress scenario synchronized 32 contenders in each of three
cases: inverse-order multi-dimension reservation, repeated terminal settlement,
and governed child admission at an active-child ceiling of one. Repeated runs
proved deterministic completion without deadlock, capacity-bounded admission,
one exact settlement/credit and evidence set, and rollback of every rejected
child aggregate. A post-settlement database assertion independently checked
the conservation equation after descendant consumption roll-up.

The composed lifecycle reserved 60 of a parent's 100 tokens, exhausted and
paused the child, granted and consumed an approved 20-token extension, settled
the original reservation into parent consumption, reclaimed an expired
40-token sibling, and admitted a later sibling using exactly that restored
capacity.

## Qualification commands and results

The focused and aggregate Phase 5 acceptance commands completed successfully:

```sh
go test -race -count=1 -tags=e2e -run '^TestPhase5ResourceLifecycleWorkflow$' ./internal/adapters/postgres
make test-integration
make test-e2e
make verify
```

Earlier item gates additionally repeated the 32-way PostgreSQL contention
scenario five times and ran the randomized conservation model and checked
arithmetic fuzz campaigns. The final clean-tree gate passed generation drift,
formatting, vet/module/OpenAPI checks, unit and race suites, all 21 OPA policy
tests, real-PostgreSQL integration and end-to-end workflows, dependency source,
license and vulnerability checks, binary/image builds, and hardened container
smoke. No required test was skipped.

## Exit conclusion

RES-001 through RES-014 satisfy the Phase 5 exit condition: stress and property
tests preserve exact resource conservation without oversubscription, deadlock,
lost credit, double settlement, or structural-limit bypass. Phase 6 can add
revocation distribution and freshness enforcement to these qualified resource
and run-governance boundaries.
