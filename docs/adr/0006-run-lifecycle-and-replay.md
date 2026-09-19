# ADR-0006: Atomic Run authority, replay and fenced ownership

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Concurrent admissions, duplicate requests and crashed workers must not create
duplicate Runs, mutate terminal state or let stale owners act. Agent execution
is outside AG, but its authority and lifecycle evidence require one durable
ordering boundary. Historical plan sections 3.5 and 5 establish that boundary.

## Decision

Resolve one policy-allowed, approved, non-revoked immutable agent version and
persist its governed snapshot during admission. Explicit pin/rollback use has
separate authorization. A policy activation change during resolution denies
rather than mixing decisions from different versions.

Commit Run mutation, ordered event and outbox evidence atomically. Admission
also commits its established idempotency response with creation, so a response
storage failure cannot leave an admitted Run available for duplicate creation.
Idempotency binds tenant, principal, route and normalized request hash; changed
content conflicts, exact replay returns the established response.

Use explicit legal state transitions, optimistic versions and monotonically
fenced worker leases. Terminal states cannot transition. Signals and resumable
SSE obey the versioned lifecycle contract and bounded retention. Internal lease
services do not constitute a deployed or versioned AR worker integration.

## Alternatives considered

Separate commits for admission and replay response were disproved by operational
failure and replaced. Unfenced leases permit old workers to mutate after expiry.
Mutable version payloads or caller-selected unapproved versions erase the
authority decision. Blind mutation retries cannot distinguish lost responses
from failed commits.

## Consequences

Transactions and lock ordering are more demanding than CRUD. Concurrent
approval changes require current eligibility checks without stale snapshots;
the operational fix preserves concurrency rather than serializing every Run
against one agent. Clients must reuse replay identity only for identical input.

## Security impact

Tenant-derived repositories, live revocation/policy checks, immutable snapshots
and stale-owner fencing prevent callers or workers from manufacturing authority.
State/evidence atomicity preserves attribution through crashes.

## Operational impact

Worker crash/reclaim is component-qualified; AR/harness qualification remains
DQ-002. Observe contention and uncertain responses before retrying. Historical
bugs and their regression evidence remain part of the release record.

## References and evidence

- [Lifecycle contract](../contracts/domain-model.md), [API examples](../api/run-lifecycle.md)
- [Phase 4 evidence](../phase-4-evidence.md), [SSD qualification](../operations/ssd-qualification.md)
- [Historical plan](../releases/history/PLAN.md), sections 3.5/5; RUN entries in the [ledger](../releases/history/TODO.md)
- Commits `1b31cbe`, `406db54`, `bd148a9`; `internal/application/run_worker.go`, `internal/adapters/postgres/run_admission.go`
