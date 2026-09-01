# ThinkPixelAG Implementation Plan

## 1. Purpose

This document is the implementation contract for taking ThinkPixelAG from an empty repository to a release candidate. It translates the agent-governance-plane blueprint into architecture, invariants, delivery phases, validation gates, and working instructions for coding agents.

`TODO.md` is the chronological execution ledger. This plan explains why and how; the checklist records what remains and what was verified.

## 2. Product boundary

ThinkPixelAG owns durable governance state and decisions:

- agent identity, ownership, sponsorship, risk class, and lifecycle;
- immutable implementation versions and their approval status;
- allowed callers, models, tools, skills, and subagents;
- tenant-scoped run admission and lifecycle;
- resource envelopes, reservations, trusted consumption, settlement, and reclaim;
- revocation scopes, monotonic epochs, ordered changes, and freshness state;
- policy metadata, controlled promotion, and decision evidence;
- audit-grade records for administrative and security-sensitive actions.

It does not execute agent logic, proxy arbitrary model/tool traffic in the first release, replace an identity provider, or implement cross-enterprise federation.

## 3. Architecture decisions

These are provisional decisions until captured as ADRs at plan completion.

### 3.1 Deployment shape

Start as a modular monolith compiled from Go. Use explicit domain/application/adapter boundaries and background-worker interfaces. The API process may run HTTP handlers, outbox publishing, and revocation streaming initially, but each worker must support independent enablement and leader/lease behavior where singleton work is required. This reduces initial operational cost while preserving extraction seams.

### 3.2 Persistence responsibilities

PostgreSQL is mandatory and authoritative. It stores normalized durable state and append-only ledgers. All tenant-owned rows include `tenant_id`; queries enforce tenant scoping in repositories, and PostgreSQL row-level security should be evaluated as defense in depth after the basic repository model is proven.

Valkey is optional. It can store expiring authorization decisions keyed by policy/epoch/input digests, token-bucket counters, and stream fanout hints. Every cached value has a bounded TTL and version/epoch key. Cache failure must not corrupt allocation, revocation, or run state. Rate-limit behavior on Valkey loss is policy-defined and conservative for privileged endpoints.

OPA evaluates Rego. The service supplies normalized, bounded input and consumes a typed decision contract. Policy activation metadata is transactional in PostgreSQL. Bundles are content-addressed, signed/verified before activation, and cached locally with explicit freshness. For the release candidate, bundles may be stored in PostgreSQL if kept small; the storage interface must permit a future OCI/object-store backend without changing policy semantics.

### 3.3 Interfaces

- REST/JSON with an OpenAPI 3.1 contract is canonical for the release candidate.
- Server-sent events is the initial transport for ordered run/revocation event consumption; the interface may later gain gRPC without changing domain semantics.
- Authentication uses OIDC/JWT verification against configured issuers/JWKS, with issuer, audience, algorithm, expiry, and clock-skew validation.
- Tenant and principal derive from verified claims and mapping policy, never untrusted JSON fields or forwarding headers from arbitrary clients.
- RFC 7807 problem details provide consistent errors; request IDs and trace context are returned.
- Mutation requests accept an `Idempotency-Key`; the stored result is scoped to tenant, principal, route, and normalized request hash.

The baseline HTTP transport binds before reporting startup, validates or creates
canonical UUIDv7 request IDs, extracts W3C trace context, applies centralized
panic/error handling, limits headers/bodies/time, and drains on termination.
`/livez`, `/readyz`, and `/metrics` are intentionally unauthenticated operational
endpoints; they expose no tenant data and rely on cluster network isolation.
Initial readiness represents local startup completion and is extended with
database, policy, and revocation-freshness checks in their owning phases.

### 3.4 Domain model

Principal entities:

- `Agent`, `AgentVersion`, `AgentCapability`, `AgentApproval`;
- `Run`, `RunEvent`, `RunSignal`, `RunVersionResolution`;
- `ResourceEnvelope`, `ResourceDimension`, `Reservation`, `UsageEntry`, `Settlement`;
- `PolicyBundle`, `PolicyActivation`, `PolicyDecisionRecord`;
- `Revocation`, `RevocationEpoch`, `RevocationLogEntry`, `GatewayCheckpoint`;
- `IdempotencyRecord`, `AuditEvent`, `OutboxMessage`.

IDs are opaque, canonical RFC 9562 UUIDv7 values generated with cryptographic
randomness. Timestamps are UTC and enter domain state through an injectable
clock. Exact numbers use a signed 64-bit coefficient and an explicit scale of
at most 18, serialize as strings, and never use floating point. Resource
quantities couple that checked representation to an explicit canonical unit.
Pagination cursors are versioned and HMAC-SHA-256 authenticated, and typed
errors expose only a closed machine-code set plus a safe public detail while
retaining diagnostic causes internally. These contracts and their adapter
requirements are maintained in `docs/contracts/primitives.md`.

### 3.5 State machines

The run model begins with:

```text
PENDING -> ADMITTED -> RUNNING
RUNNING -> COMPLETED | FAILED | CANCELLED | TIMED_OUT | BUDGET_EXHAUSTED
BUDGET_EXHAUSTED -> PAUSED_FOR_BUDGET | FAILED_BUDGET
PAUSED_FOR_BUDGET -> RUNNING | CANCELLED | TIMED_OUT
```

Transitions are explicit, authorized, concurrency-safe, append a run event, and emit an outbox event in one transaction. Terminal states cannot transition. Duplicate terminal requests return the established result. The final state table and permitted actors are tested and documented in OpenAPI/ADRs.

Agent versions are immutable after registration. Approval and revocation are separate state records. Run creation resolves exactly one approved, non-revoked version according to policy and persists that resolution.

### 3.6 Resource invariants

The Resource Allocation module is authoritative; the runtime governor only enforces the issued envelope.

For every parent run and resource dimension:

```text
allocated_to_open_children + direct_consumption + available = granted_budget
```

- Child creation locks or conditionally updates the authoritative allocation row.
- All dimensions in a reservation succeed atomically or none do.
- Structural limits are non-expandable by callers or harnesses.
- Trusted metering events are unique by source and event ID and monotonically applied.
- Consumption cannot be negative and cannot release budget.
- Settlement is idempotent and atomically marks the reservation settled, records consumption, and credits unused capacity.
- Expired/terminal abandoned reservations are reconciled by a replay-safe worker.
- Extensions require an explicit authorized governance action and evidence record.

PostgreSQL concurrency tests must demonstrate no oversubscription under parallel child creation and no double credit under repeated/concurrent settlement.

### 3.7 Revocation invariants

Supported scopes are `run_id`, `agent_id`, `agent_version`, `skill_digest`, `principal_id`, `tenant_id`, `tool_id`, `policy_version`, and `global`.

- Each change commits the revocation, applicable monotonic epoch increments, ordered log record, and outbox message atomically.
- Consumers resume from a durable cursor and detect gaps.
- Stream push minimizes latency; authoritative reconciliation repairs gaps and restarts.
- A local/cache `ALLOW` cannot outlive its policy and revocation freshness bound.
- High-risk writes require live authoritative state.
- Normal writes default to at most 30 seconds revocation age and sensitive reads to at most 60 seconds; these are configurable only toward a policy-approved posture.
- Readiness reports degraded/not-ready when this instance cannot satisfy the configured security freshness contract.

### 3.8 Policy decision contract

OPA input contains the verified subject, tenant, action, resource, agent/version metadata, risk class, requested constraints, current epochs, and security-state freshness. It excludes secrets and unbounded raw agent input.

OPA output is typed and validated:

```json
{
  "allow": true,
  "reason_codes": ["agent.invoke.allowed"],
  "resolved_constraints": {},
  "obligations": [],
  "decision_ttl_seconds": 10
}
```

Missing, malformed, unavailable, expired, or unverifiable policy produces a denial for protected operations. Decision records contain hashes and identifiers rather than sensitive inputs. Policy tests cover allow, deny, tenant isolation, risk tiers, stale revocation state, constraint narrowing, privileged actions, and malformed input/output.

### 3.9 Evidence and observability

State changes and their audit/outbox records commit together. An asynchronous publisher exports CloudEvents-like envelopes to a configurable sink. Delivery is at least once; event IDs make consumers idempotent. The evidence sink is operationally independent in production.

Use structured logs, Prometheus metrics, and OpenTelemetry traces. Redact authorization headers, tokens, objectives, agent inputs, policy input fields marked sensitive, and database credentials. Required service-level indicators include request rate/error/latency, admission and denial counts, policy latency/failures, database pool saturation, outbox lag, revocation stream lag/age, reservation conflicts, settlement lag, and cache availability.

## 4. Go implementation approach

Target a supported Go release pinned in `go.mod` and CI. Prefer the standard library where practical. Select dependencies through small interfaces and record consequential choices in ADRs. Expected components include:

- `cmd/thinkpixelag` and `cmd/migrate`;
- `internal/domain` for pure types, invariants, and state transitions;
- `internal/app` for use cases and transaction boundaries;
- `internal/ports` for persistence, policy, identity, cache, clock, ID, and evidence interfaces;
- `internal/adapters/http`, `postgres`, `opa`, `oidc`, `valkey`, and `evidence`;
- generated OpenAPI server/client types where this does not leak transport concerns into the domain;
- SQL migrations with forward compatibility and explicit rollback/roll-forward guidance.

The repository-root Makefile is the sole stable developer/CI command surface.
Its aggregate `verify` target covers generation drift, formatting/static/module
checks, OpenAPI, unit/race/policy/integration/e2e suites, dependency source,
vulnerability, license, and binary build gates. Container build remains an
explicit separate target until ENG-011 supplies the pinned Dockerfile and is
then required by CI and release verification.

Errors are typed and mapped once at the transport boundary. Context cancellation and deadlines propagate to all I/O. Graceful shutdown stops admission, drains requests/workers within a bound, and closes pools. Dependency calls use timeouts and narrowly scoped retry with jitter; non-idempotent operations are not blindly retried.

## 5. API and administrative surface

### 5.1 Public lifecycle API

- `GET /v1/agents` lists agents visible to the caller with cursor pagination.
- `GET /v1/agents/{agent_id}` describes an invocable agent without leaking hidden versions/capabilities.
- `POST /v1/agents/{agent_id}/runs` authorizes, resolves a version, narrows constraints, reserves resources, and creates a run atomically.
- `GET /v1/runs/{run_id}` returns tenant-scoped status and envelope summary.
- `POST /v1/runs/{run_id}/signals` validates signal type/payload and appends it idempotently.
- `POST /v1/runs/{run_id}/cancel` performs an idempotent authorized transition.
- `GET /v1/runs/{run_id}/events` provides ordered, resumable events with bounded retention rules.

### 5.2 Trusted/admin API

Separate route groups and policy actions cover agent/version registration and approval, policy upload/validation/activation/rollback, resource extension, trusted usage ingestion, terminal settlement, revocation create/lift/list, and gateway reconciliation. Lift/rollback semantics never decrement epochs; they append a new change.

OpenAPI must define schemas, limits, pagination, idempotency, authentication, error codes, and concurrency semantics before handlers are considered complete.

## 6. Database design and migrations

Schema design must include:

- primary/foreign/unique/check constraints that enforce domain invariants where possible;
- tenant-prefixed indexes for access paths;
- immutable agent-version payloads and content digests;
- run version-resolution snapshot and state version for optimistic concurrency;
- append-only run events and trusted usage ledger with unique producer event IDs;
- resource balances/reservations/settlements updated transactionally;
- monotonic revocation epochs and ordered log sequence;
- policy bundle digest/signature/validation/activation metadata;
- idempotency response/status/request-hash storage with expiry;
- audit and transactional outbox tables with claim/retry/dead-letter metadata.

Migrations run as an explicit job, not automatically from every API replica. Each release supports expand/migrate/contract rollout. Backups and point-in-time recovery are deployment responsibilities, but restore and migration-forward behavior must be rehearsed before RC.

## 7. Kubernetes and image design

The Docker build uses a pinned builder, reproducible flags, build metadata, and a minimal non-root runtime image. It exposes no shell requirement, contains CA certificates where needed, and handles `SIGTERM`. CI produces an SBOM, vulnerability report, provenance/signature hooks, and immutable digest.

Kubernetes artifacts include Deployment, Service, ServiceAccount, ConfigMap, secret references, migration Job, PodDisruptionBudget, NetworkPolicy, and optional HPA/ServiceMonitor. Security context uses non-root UID, dropped capabilities, seccomp, read-only root filesystem, and writable temporary volume only where required. Readiness verifies database connectivity, loaded valid policy, and acceptable revocation freshness; liveness detects a stuck process without making transient dependency failure restart-loop the pod.

## 8. Testing strategy

- Unit tests: domain transitions, quantity arithmetic, constraint narrowing, claim mapping, typed policy results, and error mapping.
- Policy tests: Rego unit tests plus golden decision-contract cases.
- Repository integration tests: real pinned PostgreSQL, migration from empty database, constraints, isolation, and transaction behavior.
- Concurrency/property tests: allocation invariant, idempotent settlement, monotonic epochs, state-machine legality, duplicate usage, and idempotency races.
- Contract tests: implementation matches OpenAPI and RFC 7807 behavior.
- Security tests: JWT validation failures, tenant boundary attacks, identifier enumeration, stale revocation, policy outage/malformed output, cache poisoning resistance, log redaction, and privilege escalation.
- End-to-end tests: register/approve/invoke/run/signal/cancel/settle/reclaim/revoke/reconcile flows with OPA, PostgreSQL, and optional Valkey.
- Resilience tests: dependency latency/outage, stream disconnect/gap, worker crash/replay, rolling restart, migration compatibility, and Valkey loss.
- Kubernetes smoke tests: image starts under restricted security context; probes, shutdown, migration job, and basic workflow succeed in a disposable cluster.

Tests must be deterministic, parallel-safe, and runnable through Make targets. Race detection and fuzz tests run in CI on an appropriate cadence.

## 9. Delivery phases and exit gates

### Phase 0 — Decisions and contracts

Define threat model, glossary, supported versions, architecture boundaries, API contract, domain state machines, policy schema, and migration strategy. Exit when reviews leave no ambiguous trust source or resource/revocation invariant.

Status: **Completed 2026-08-10.** Evidence is indexed in `docs/README.md`, the blueprint reconciliation is in `docs/phase-0-review.md`, and the OpenAPI contract validates with Redocly CLI 2.3.0. Exact dependency versions intentionally remain a Phase 1 evidence-based decision as specified in `docs/supported-versions.md`.

### Phase 1 — Engineering foundation

Initialize Go, directories, configuration, logging/metrics/tracing, Makefile, CI, development dependencies, baseline Dockerfile, and health endpoints. Exit when `make verify` passes from a clean checkout and a minimal image starts as non-root.

Status: **Completed 2026-08-10.** ENG-001 through ENG-012 are implemented. A
fresh clone of commit `147cbf4` passed the complete `make verify` gate without
changing the checkout, and its 6.7 MB distroless image ran as numeric non-root
UID/GID 65532 with a read-only root filesystem, successful probes, and graceful
SIGTERM shutdown. CI uses separate least-privilege jobs with immutable action
pins. Local development uses healthy immutable PostgreSQL 18.4, OPA 1.19.0,
and opt-in disposable Valkey 9.1.1 dependencies. Full audit evidence is in
`docs/phase-1-evidence.md`.

### Phase 2 — Authoritative persistence

Add migrations, transaction manager, repositories, idempotency, outbox, and integration-test infrastructure. Exit when schema and repositories pass isolation, rollback, migration, and replay tests on real PostgreSQL.

Status: **Completed 2026-08-19.** The pinned PostgreSQL 18.4 integration gate
qualified empty installation and upgrade recovery, schema constraints and
access paths, tenant isolation and rollback, concurrent idempotency ownership,
and replay-safe outbox claiming/retry/dead-letter behavior. Full environment,
command, and result evidence is recorded in `docs/phase-2-evidence.md`.

### Phase 3 — Identity, policy, and registry

Implement OIDC authentication, claim-to-tenant mapping, OPA client/contract, policy lifecycle, agent/version registration, approvals, and version resolution. Exit when deny-by-default, tenant isolation, policy freshness, and immutable-version tests pass.

Status: **Completed 2026-08-22.** OIDC verification and claim mapping establish
tenant authority; the typed OPA boundary, signed activation lifecycle, baseline
Rego, and optional integrity-protected Valkey cache fail closed; and the
tenant-scoped registry enforces immutable versions, governed eligibility,
policy-driven resolution, and authorized discovery. The composed workflow and
full clean-tree gate passed against pinned PostgreSQL 18.4, OPA 1.19.0, and
Valkey 9.1.1. Full evidence is recorded in `docs/phase-3-evidence.md`.

### Phase 4 — Run lifecycle API

Implement run admission, status, events, signals, cancellation, state transitions, and event publication. Exit when OpenAPI contract and end-to-end lifecycle/idempotency tests pass.

Status: **Completed 2026-08-22.** The authenticated public lifecycle surface,
tenant-scoped PostgreSQL repositories, ordered resumable SSE, concurrency-safe
cancellation, fenced worker leases, and transactional outbox publication are
implemented. The OpenAPI contract, focused concurrency/replay suites, composed
real-PostgreSQL workflow, and full clean-tree gate passed. API examples and
full qualification evidence are recorded in `docs/api/run-lifecycle.md` and
`docs/phase-4-evidence.md`.

### Phase 5 — Resource governance

Implement resource dimensions, envelope issuance, atomic child reservation, trusted metering, exhaustion transitions, extension, settlement, reclaim, and reconciliation. Exit when stress/property tests cannot oversubscribe or double-credit resources.

Status: **Completed 2026-08-26.** Tenant-scoped typed dimensions, immutable
grants, lockable balances, atomic child allocation, trusted metering and rate
enforcement, governed exhaustion and extension, terminal settlement, and
replay-safe reconciliation are implemented. Randomized conservation checks,
32-way real-PostgreSQL contention scenarios, the composed allocation lifecycle,
and the full clean-tree gate passed. The authoritative model and qualification
results are recorded in `docs/contracts/resource-accounting.md`,
`docs/architecture/database-schema.md`, and `docs/phase-5-evidence.md`.

### Phase 6 — Revocation and freshness

Implement scoped revocation, epochs, ordered durable log, resumable push stream, gateway reconciliation/checkpoints, cache invalidation, and fail-closed freshness gates. Exit when disconnect/gap/staleness scenarios meet declared security behavior.

Status: **Completed 2026-08-27.** Every supported scope advances atomic ordered
epochs and distribution evidence; authenticated gateways resume or reconcile
from durable state; instance-local monotonic freshness, risk-class gates,
versioned caches, readiness, and metrics fail closed across restart, lag, gaps,
partitions, and recovery. The real-PostgreSQL authenticated SSE path delivered
100 committed changes to a connected gateway at p99 22.463 ms against the
5-second RC objective, and the full clean-tree gate passed. The normative model,
gateway consumer behavior, measurement boundary, and qualification results are
recorded in `docs/contracts/revocation.md` and `docs/phase-6-evidence.md`.

### Phase 7 — Evidence and self-protection

Complete privileged policy separation, approval hooks for high-impact operations, signed artifact verification, KMS abstraction, evidence export, redaction, and break-glass workflow documentation. Exit when privileged changes are attributable and independently exportable, and bypass attempts fail.

Progress through SEC-010 (2026-09-01): privileged actions have closed
least-privilege roles and digest-bound four-eyes approvals; managed KMS/HSM
ports keep private keys non-exportable; and a closed, versioned privileged
artifact envelope now binds payload digest, class, schema/revision, and managed
key metadata before policy persistence or activation. A closed v1 privileged
evidence contract now binds attribution, outcome, correlation, and minimized
category-specific policy, revocation, resource-override, approval, key,
version-approval, and break-glass data. A separately configured authenticated
HTTPS sink now receives stable-ID replayable deliveries with a deterministic
per-sink SHA-256 chain; strict receipts commit append-only beside a fenced,
monotonic checkpoint. Break-glass is now a
closed policy/revocation-recovery capability rather than a standing role:
fresh provider-verified phishing-resistant MFA and an exact four-eyes digest
produce a credential-digest-only grant for at most fifteen minutes. Every
activation, use, expiry, and revocation commits append-only lifecycle evidence
and an independent export record atomically. Trusted service routes now have a
closed v1 deployment binding from an exact verified client-certificate URI SAN
to one workload principal, tenant, and least-privilege service-role set. Their
middleware rejects bearer and forwarding identity, while policy independently
requires workload principal type, exact action role, tenant/resource binding,
and authoritative freshness. Explicit local/test transport exceptions cannot
derive authority from caller input. Central leak controls now fully redact
structured error values, restrict trace names and attributes, bound metric
labels and client problem details, reject restricted policy/audit fields, and
keep panic recovery free of panic values and stacks. Sentinel tests cover each
sink. A stable `make test-security` acceptance gate now groups privilege
escalation, approval bypass, replay, cache poisoning, policy tampering, stale
authorization, and evidence-suppression attacks. Its transaction- and
lease-sensitive cases run against real PostgreSQL, and the complete repository
gate passed. Final threat-model review remains.

### Phase 8 — Production packaging and operations

Finalize image, Kubernetes resources, dashboards, alerts, SLOs, runbooks, backup/restore, upgrade/rollback, load testing, SBOM/scanning, and release automation. Exit when a disposable cluster passes installation, upgrade, disruption, recovery, and smoke tests.

### Phase 9 — Release-candidate closure

Run all verification gates, resolve critical/high findings, freeze contracts, prepare release notes, convert completed planning decisions/history into ADRs, update README, remove `PLAN.md` and `TODO.md`, and commit the documentation transition. Exit when the repository can produce a traceable, signed/taggable RC artifact from a clean checkout.

## 10. Coding-agent operating instructions

These instructions apply to every implementation session.

1. Read `README.md`, this file, and `TODO.md`; inspect repository status before editing. Preserve unrelated user changes.
2. Select the first unchecked TODO whose dependencies are complete. Work on one atomic item or a tightly coupled contiguous group; do not skip prerequisite gates silently.
3. Before implementation, restate the acceptance criteria internally and identify tests that prove them. If the design changes, update the relevant plan text in the same change.
4. Implement the smallest complete vertical change. Include tests, migrations, API/schema changes, security handling, observability, and documentation required by that item.
5. Run the narrow tests while developing, then the item-specific acceptance commands. Run `make verify` before declaring a milestone complete; if a required check cannot run, leave the item unchecked and record the blocker in `TODO.md`.
6. Update `TODO.md` in the same change: mark only proven items `[x]`, add the completion date and commit placeholder/reference, and record material evidence or deviations in the progress log. Add newly discovered work in its correct chronological location with a stable ID.
7. Update this plan whenever implementation invalidates an assumption, changes a decision, adds/removes scope, or changes sequencing. Never rewrite completed history to hide a deviation; document the reason and superseding decision.
8. Update `README.md` when user-facing behavior, setup, commands, configuration, API status, or operational expectations change.
9. Review the diff and ensure generated artifacts are current. Do not include secrets, local state, test credentials, binaries, or unrelated modifications.
10. Commit the completed unit of work directly to `main` only after its acceptance criteria pass and repository policy permits direct commits. Use an imperative, descriptive message such as `feat(resources): add atomic child reservations`; include the TODO IDs, behavior, tests, migrations, and security/operational notes in the body. Never amend or rewrite shared history, and never bypass failing hooks. If direct commit is disallowed or unrelated changes cannot be isolated safely, stop before committing and record/report the blocker.
11. After committing, replace any commit placeholder in the TODO progress log with the resulting short SHA in a follow-up documentation commit when necessary, or record the completed SHA in the next progress-log update. Do not claim a commit that does not exist.
12. At each phase exit, run the full gate, update phase status in both planning files, and create a phase-completion commit with evidence.

### Commit scope and safety

- The default branch is `main`; confirm it before each implementation commit.
- Stage explicit files, not the entire worktree, when unrelated changes exist.
- Database migrations already released or shared are immutable; add a corrective migration.
- A TODO checkbox means implemented and verified, not merely coded.
- Any skipped/flaky test, accepted vulnerability, or temporary fail-open behavior blocks RC unless explicitly resolved by an approved recorded decision.
- Security-sensitive changes require a second review when the repository workflow supports it, even if committed locally for traceability.

## 11. Plan maintenance and ADR transition

During implementation, `PLAN.md` and `TODO.md` are living documents. The progress log is append-only apart from correcting factual mistakes. Stable decisions should be drafted in `docs/adr/` as soon as they become durable; final cleanup must not be the first time critical rationale is recorded.

When every RC item is verified:

1. Reconcile the plan, checklist, commits, release evidence, and actual implementation.
2. Create numbered ADRs in `docs/adr/` covering at least service shape, persistence/cache boundaries, policy lifecycle, authentication/tenant trust, run state machine, resource accounting, revocation/freshness, evidence/outbox, and Kubernetes security.
3. Each ADR records context, decision, alternatives, consequences, security/operational impact, and relevant implementation/commit references extracted from these files.
4. Move enduring setup, API, operations, and contribution information into `README.md` and durable documents under `docs/`.
5. Verify no unresolved item, blocker, risk, or useful rationale would be lost.
6. Remove `PLAN.md` and `TODO.md` in the same final documentation change.
7. Run documentation/link checks and `make verify`, then commit to `main` with a message such as `docs: finalize governance plane architecture records`.

## 12. Release-candidate quality gate

An RC requires all of the following:

- all TODO items checked with evidence and no unresolved blockers;
- clean build plus unit, race, policy, integration, contract, end-to-end, security, and Kubernetes smoke tests;
- API and migration compatibility reviewed and documented;
- load/resource targets met and failure behavior demonstrated;
- zero unresolved critical/high exploitable vulnerabilities or policy violations;
- threat model, data classification, SLOs, alerts, and operational/security runbooks reviewed;
- restore, upgrade, rollback, revocation-gap recovery, and credential/key-rotation procedures exercised;
- OCI image, SBOM, checksums/provenance/signature hooks, Kubernetes artifacts, and release notes generated reproducibly;
- ADR extraction and removal of temporary planning files completed as the final release-preparation change.

## 13. Initial risks

- **Authorization drift:** constrain OPA input/output with schemas, version contracts, golden tests, and fail-closed validation.
- **Allocation races:** centralize balance updates in PostgreSQL transactions and stress test concurrent reservations/settlements.
- **Revocation staleness:** couple cache keys to epochs, reconcile streams, expose age metrics, and gate by operation risk.
- **Tenant data leakage:** derive tenant from identity, scope every repository query, test adversarial cross-tenant identifiers, and consider RLS defense in depth.
- **Governance compromise:** minimize standing privilege, separate keys/roles, verify signed artifacts, and export evidence independently.
- **Plan becoming stale:** require checklist/plan updates and verification evidence in every implementation commit.
