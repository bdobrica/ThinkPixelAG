# ThinkPixelAG Release-Candidate TODO

This is the chronological implementation checklist. Execute the first unchecked item whose dependencies are complete. An item is checked only after its acceptance evidence passes. Follow the coding-agent and commit protocol in `PLAN.md` after every completed implementation item.

Status notation: `[ ]` pending, `[x]` implemented and verified. Add completion metadata as `— completed YYYY-MM-DD, commit <sha>, evidence: <commands/artifacts>`.

## Phase 0 — Decisions, threats, and contracts

- [x] GOV-001 Create `docs/` and an ADR template with status, context, decision, alternatives, consequences, security, operations, and references sections. — completed 2026-08-10, commit `eddf453`, evidence: `docs/adr/README.md`, `docs/adr/template.md`
- [x] GOV-002 Write the system context, trust-boundary, data-flow, and component diagrams; identify trusted gateways, workers, IdP, OPA, PostgreSQL, Valkey, KMS, and evidence sink. — completed 2026-08-10, commit `eddf453`, evidence: `docs/architecture/system.md`
- [x] GOV-003 Produce a threat model covering spoofing, tenant confusion, confused deputy/delegation, policy tampering, stale revocation, resource races, replay, evidence tampering, and governance-admin compromise. — completed 2026-08-10, commit `eddf453`, evidence: `docs/security/threat-model.md`
- [x] GOV-004 Define the data-classification and redaction rules for tokens, objectives, inputs, policy data, decisions, audit events, logs, traces, and metrics. — completed 2026-08-10, commit `eddf453`, evidence: `docs/security/data-classification.md`
- [x] GOV-005 Record supported Go, PostgreSQL, OPA, Valkey, Kubernetes, OpenAPI, and image-platform versions and the upgrade/support policy. — completed 2026-08-10, commit `eddf453`, evidence: `docs/supported-versions.md`; exact tested pins are a Phase 1 gate
- [x] GOV-006 Formalize agent/version, run, reservation, settlement, policy, and revocation state machines including allowed actors and illegal transitions. — completed 2026-08-10, commit `eddf453`, evidence: `docs/contracts/domain-model.md`
- [x] GOV-007 Formalize resource units, numeric bounds, rounding, allocation equations, extension rules, exhaustion semantics, and settlement invariants. — completed 2026-08-10, commit `eddf453`, evidence: `docs/contracts/resource-accounting.md`
- [x] GOV-008 Define revocation scopes, epoch relationships, ordering, staleness classes, fail-closed rules, reconciliation, and lift semantics. — completed 2026-08-10, commit `eddf453`, evidence: `docs/contracts/revocation.md`
- [x] GOV-009 Define the typed OPA input/output schema, reason-code registry, constraint-narrowing semantics, decision TTL rules, and failure behavior. — completed 2026-08-10, commit `eddf453`, evidence: `docs/contracts/policy-decision.md`
- [x] GOV-010 Draft OpenAPI 3.1 paths and schemas for public lifecycle and trusted/admin APIs, including auth, pagination, limits, idempotency, errors, and event resumption. — completed 2026-08-10, commit `eddf453`, evidence: `api/openapi/thinkpixelag.yaml`
- [x] GOV-011 Define SLOs and capacity targets for API latency/availability, admission, policy evaluation, revocation propagation/freshness, outbox lag, and allocation contention. — completed 2026-08-10, commit `eddf453`, evidence: `docs/operations/slos.md`
- [x] GOV-012 Review Phase 0 artifacts against the source blueprint and close all ambiguous trust sources and invariants. — completed 2026-08-10, commit `eddf453`, evidence: `docs/phase-0-review.md`
- [x] GOV-013 Run documentation/schema validation and commit Phase 0 with updated plan/checklist evidence. — completed 2026-08-10, commit `eddf453`, evidence: Redocly CLI 2.3.0 lint passed; `git diff --check` passed

## Phase 1 — Engineering foundation

- [x] ENG-001 Initialize the Go module, pin the Go/toolchain version, and create `cmd`, `internal`, `api`, `policies`, `migrations`, `deploy`, and `test` package boundaries. — completed 2026-08-10, commit `5287791`, evidence: `go version`; `go mod edit -json`; `go test ./...`; boundary documentation and package compile checks
- [x] ENG-002 Add dependency policy, license/source checks, vulnerability scanning configuration, and reproducible tool pinning. — completed 2026-08-10, commit `45c422d`, evidence: dependency checker unit/integration tests; pinned `govulncheck` and `go-licenses` gates; `go test ./...`; `git diff --check`
- [x] ENG-003 Implement strict typed configuration with defaults, environment/flag loading, startup validation, secret-safe rendering, and unit tests. — completed 2026-08-10, commit `be384bc`, evidence: configuration unit/redaction tests; `go test -race ./...`; `go vet ./...`; dependency, vulnerability, and license gates
- [x] ENG-004 Implement structured logging with request/trace correlation and centralized sensitive-field redaction tests. — completed 2026-08-10, commit `f6c3a9e`, evidence: logging/config unit and recursive leak tests; `go test -race -cover ./...`; `go vet ./...`; source/license/vulnerability gates
- [x] ENG-005 Add Prometheus metrics and OpenTelemetry tracing initialization with no-op/local configurations. — completed 2026-08-10, commit `215bfbf`, evidence: Prometheus exposition/no-op/cardinality tests; OpenTelemetry no-op/OTLP export/propagation/shutdown tests; race/vet/source/license/vulnerability gates
- [x] ENG-006 Add ID, UTC clock, decimal/resource quantity, pagination cursor, and typed error primitives with tests/fuzz cases. — completed 2026-08-10, commit `8ff8f77`, evidence: deterministic primitive/overflow/tamper tests; three fuzz campaigns; `go test -race -cover ./...`; vet/source/license/vulnerability gates
- [x] ENG-007 Add the HTTP server skeleton, middleware ordering, body/header/time limits, request IDs, RFC 7807 responses, graceful shutdown, `/livez`, and initial `/readyz`. — completed 2026-08-10, commit `72319f1`, evidence: HTTP race/coverage tests; listener/readiness/shutdown and live process smoke tests; OpenAPI lint; build/vet/source/license/vulnerability gates
- [x] ENG-008 Create a Makefile with pinned `tools`, `generate`, `fmt`, `lint`, `test`, `test-race`, `test-policy`, `test-integration`, `test-e2e`, `build`, `image`, and aggregate `verify` targets. — completed 2026-08-10, commit `7e03c72`, evidence: Makefile contract tests; individual target checks; staged clean-checkout `make verify`; intentional pre-ENG-011 image prerequisite failure
- [x] ENG-009 Add local development orchestration for pinned PostgreSQL, OPA, and optional Valkey with isolated test credentials and health checks. — completed 2026-08-10, commit `610088a`, evidence: immutable Compose image pins and contract tests; healthy full-profile runtime smoke; positive/negative PostgreSQL and Valkey authentication; OPA 1.19.0 endpoint/version check; scoped volume cleanup; `make verify`
- [x] ENG-010 Add CI for formatting, generation drift, lint, unit/race tests, policy tests, integration tests, build, vulnerability/license checks, and image build. — completed 2026-08-10, commit `47f8519`, evidence: eight least-privilege GitHub Actions jobs; immutable action pins; CI contract tests; `make verify`
- [x] ENG-011 Add a multi-stage baseline Dockerfile, `.dockerignore`, non-root runtime, build metadata, and container smoke test. — completed 2026-08-10, commit `5869296`, evidence: immutable Go/distroless base pins; allowlisted build context; UID/GID 65532; OCI labels and embedded build metadata; read-only/capability-dropped probe and SIGTERM smoke; `make verify`
- [ ] ENG-012 Verify a clean checkout passes `make verify`, starts the minimal image as non-root, and commit Phase 1 evidence.

## Phase 2 — PostgreSQL, transactions, and delivery primitives

- [ ] DATA-001 Select and wrap the PostgreSQL driver, migration tool, query approach, and transaction abstraction; record the consequential decision.
- [ ] DATA-002 Create initial migrations for tenants/principals, agents/versions/capabilities/approvals, policies/activations, and their integrity constraints/indexes.
- [ ] DATA-003 Add migrations for runs, run version snapshots, state versions, signals, and append-only ordered run events.
- [ ] DATA-004 Add migrations for resource dimensions, envelopes, balances, reservations, trusted usage ledger, settlements, and uniqueness/check constraints.
- [ ] DATA-005 Add migrations for revocations, monotonic epochs, ordered revocation log, and gateway checkpoints.
- [ ] DATA-006 Add migrations for idempotency records, audit events, and transactional outbox claim/retry/dead-letter fields.
- [ ] DATA-007 Implement connection pool configuration, dependency health, transaction helpers, error classification, statement timeouts, and telemetry.
- [ ] DATA-008 Implement tenant-scoped repositories for Phase 2 tables and tests proving rollback and cross-tenant isolation.
- [ ] DATA-009 Implement request-hash-bound idempotency acquisition, replay, conflict, expiry, and concurrent-request behavior.
- [ ] DATA-010 Implement transactional audit/outbox writes and a replay-safe publisher with bounded retry, jitter, leases/claims, and poison-message handling.
- [ ] DATA-011 Test migrations from empty DB, upgrade from prior fixture, constraints/index access paths, forward recovery after failure, and compatibility rules.
- [ ] DATA-012 Evaluate PostgreSQL row-level security as defense in depth; implement it or record why repository enforcement is used for RC.
- [ ] DATA-013 Run real-PostgreSQL integration/concurrency tests and commit Phase 2 evidence.

## Phase 3 — Identity, OPA policy, and agent registry

- [ ] AUTH-001 Implement OIDC issuer discovery/JWKS retrieval with pinned issuers/audiences/algorithms, cache bounds, rotation, time validation, and outage behavior.
- [ ] AUTH-002 Implement bearer-token middleware and verified principal/tenant/role mapping; reject conflicting body/header tenant hints.
- [ ] AUTH-003 Add authentication tests for bad signature, issuer, audience, algorithm, expiry/not-before, key rotation, missing tenant, and forged forwarding headers.
- [ ] POL-001 Implement the typed OPA client/embedded boundary with deadlines, response schema validation, reason codes, decision metadata, and fail-closed errors.
- [ ] POL-002 Implement policy bundle digest/signature verification interfaces, validation/compile testing, persistence, activation, rollback-as-new-activation, and local freshness.
- [ ] POL-003 Write baseline Rego for agent discovery/invocation, tenant isolation, admin actions, risk/staleness classes, constraint narrowing, and privileged operations.
- [ ] POL-004 Add Rego unit/golden tests for allow/deny, malformed input/output, policy outage, stale state, cross-tenant access, and non-expansion of constraints.
- [ ] POL-005 Add optional Valkey-backed decision caching keyed by policy version, relevant epochs, subject/action/resource/input digest, with bounded TTL and safe bypass on failure.
- [ ] REG-001 Implement agent creation/update semantics, owner/sponsor/risk validation, and tenant-scoped list/describe repositories/use cases.
- [ ] REG-002 Implement immutable content-addressed agent versions with image digest, model/tool/skill/subagent declarations, limits, and schema validation.
- [ ] REG-003 Implement approval/deprecation/revocation eligibility state and authorized admin endpoints with evidence records.
- [ ] REG-004 Implement policy-driven version resolution, optional controlled rollback/test pinning, and persisted resolution snapshots.
- [ ] REG-005 Implement public agent list/describe endpoints with cursor pagination, authorization filtering, and enumeration-safe errors.
- [ ] REG-006 Verify deny-by-default identity/policy behavior, immutable versions, tenant isolation, cache invalidation, and version resolution end to end.
- [ ] REG-007 Commit Phase 3 with API/policy documentation and verification evidence.

## Phase 4 — Run lifecycle

- [ ] RUN-001 Implement the pure run state machine with actor permissions, terminal-state rules, optimistic versioning, table-driven tests, and fuzz/property tests.
- [ ] RUN-002 Implement run admission transaction: authenticate, authorize, resolve version, narrow constraints, create initial envelope/run/event/audit/outbox records.
- [ ] RUN-003 Implement `POST /v1/agents/{agent_id}/runs` with request limits, idempotency, server-derived tenant, explicit-version restrictions, and stable errors.
- [ ] RUN-004 Implement tenant-scoped `GET /v1/runs/{run_id}` with authorization and enumeration-safe not-found behavior.
- [ ] RUN-005 Implement validated, typed, idempotent `POST /v1/runs/{run_id}/signals` with ordered events and delivery semantics.
- [ ] RUN-006 Implement idempotent authorized `POST /v1/runs/{run_id}/cancel` including races with terminal transitions.
- [ ] RUN-007 Implement ordered/resumable `GET /v1/runs/{run_id}/events` using SSE, cursor validation, heartbeats, backpressure, retention, and authorization.
- [ ] RUN-008 Implement worker-facing claim/heartbeat/start/complete/fail/timeout operations with leases and fencing so stale workers cannot mutate runs.
- [ ] RUN-009 Add outbox publication for every run mutation and verify at-least-once/replay behavior after worker/process crashes.
- [ ] RUN-010 Run OpenAPI conformance, lifecycle concurrency/idempotency, tenant isolation, SSE reconnect, and complete end-to-end tests.
- [ ] RUN-011 Commit Phase 4 with API examples, state-machine documentation, and evidence.

## Phase 5 — Resource allocation and run governance

- [ ] RES-001 Implement extensible typed resource dimensions classified as consumable, structural, or deadline-like with explicit units and validation.
- [ ] RES-002 Implement policy-constrained root envelope issuance and persist its immutable grant plus mutable balances.
- [ ] RES-003 Implement multi-dimension atomic child reservation using PostgreSQL locks/conditional updates and deterministic lock ordering.
- [ ] RES-004 Enforce maximum active/total children and delegation depth transactionally during child admission.
- [ ] RES-005 Implement trusted metering ingestion with authenticated producers, unique source event IDs, monotonic application, and invalid/negative usage rejection.
- [ ] RES-006 Implement rate enforcement for structural throughput limits; use Valkey only as a conservative accelerator with defined outage behavior.
- [ ] RES-007 Implement consumable exhaustion detection and the `BUDGET_EXHAUSTED`, `PAUSED_FOR_BUDGET`, and `FAILED_BUDGET` transitions.
- [ ] RES-008 Implement authorized deadline/budget extension as an additive ledger action with policy approval and privileged evidence; prevent self-extension.
- [ ] RES-009 Implement idempotent terminal settlement that atomically closes a reservation and immediately returns unused capacity to the parent.
- [ ] RES-010 Implement reconciliation for terminal/expired orphan reservations and replay after crashes without double credit.
- [ ] RES-011 Add invariant/property tests over random reservation/usage/settlement sequences and checked arithmetic bounds.
- [ ] RES-012 Add high-concurrency tests proving no oversubscription, deadlock, lost credit, double settlement, or structural-limit bypass.
- [ ] RES-013 Add end-to-end parent/child allocation, exhaustion, extension, settlement, reclaim, and sibling reuse tests.
- [ ] RES-014 Commit Phase 5 with resource model documentation, database evidence, and load/concurrency results.

## Phase 6 — Revocation distribution and freshness

- [ ] REV-001 Implement typed revocation creation/lift for every supported scope with authorization, reason, effective/expiry time, and evidence.
- [ ] REV-002 Implement atomic monotonic epoch increment plus ordered log/outbox append; prove epochs never decrement or duplicate under concurrency.
- [ ] REV-003 Enforce revocations and applicable epoch checks in discovery, admission, lifecycle mutations, worker operations, and policy evaluation.
- [ ] REV-004 Implement resumable authenticated revocation SSE stream with sequence/epoch cursor, heartbeat, backpressure, retention, and gap signaling.
- [ ] REV-005 Implement authoritative reconciliation/snapshot endpoint and persisted gateway checkpoints.
- [ ] REV-006 Implement instance-local freshness tracker with last stream receipt, last reconciliation, last applied epoch, and clock-safe age calculation.
- [ ] REV-007 Enforce risk-based freshness: authoritative high-risk writes, default 30-second normal writes, 60-second sensitive reads, and bounded low-risk cache.
- [ ] REV-008 Wire revocation/policy activation changes to local and Valkey cache invalidation/versioned cache keys.
- [ ] REV-009 Expose revocation age/lag/gap metrics and make readiness reflect inability to meet configured security freshness.
- [ ] REV-010 Test stream gaps, duplicate/out-of-order events, restarts, partitions, expired cache, reconciliation, global revocation, and fail-closed recovery.
- [ ] REV-011 Measure propagation against SLO targets and commit Phase 6 with documented gateway integration behavior.

## Phase 7 — Governance self-protection and independent evidence

- [ ] SEC-001 Classify endpoints/actions by risk and enforce separate least-privilege service/admin roles through policy.
- [ ] SEC-002 Implement approval-provider interfaces and four-eyes state for trust-root rotation, global revocation changes, policy bypass/rollback, emergency expansion, and privileged agent classes.
- [ ] SEC-003 Implement signer/verifier abstractions compatible with managed KMS/HSM and ensure production private keys are non-exportable/not file-configured.
- [ ] SEC-004 Require signed/versioned policy and privileged configuration artifacts and reject digest/signature/version mismatch.
- [ ] SEC-005 Finalize evidence event schemas for policy, revocation, resource overrides, approvals, key lifecycle, version approvals, and break-glass actions.
- [ ] SEC-006 Implement authenticated export to a separately configurable evidence sink with replay, receipt/checkpoint, and tamper-evident hash/link metadata.
- [ ] SEC-007 Define and implement a narrow, time-bound, strongly authenticated break-glass workflow with explicit expiry and separate evidence.
- [ ] SEC-008 Add workload-identity/mTLS integration hooks and explicit service-to-service authorization; document local-development exceptions.
- [ ] SEC-009 Complete secret/token/payload redaction tests across logs, traces, metrics, errors, policy records, audit, and panic paths.
- [ ] SEC-010 Run security tests for privilege escalation, approval bypass, replay, cache poisoning, policy tampering, stale authorization, and evidence suppression.
- [ ] SEC-011 Review the threat model against implementation, resolve all critical/high findings, and commit Phase 7 evidence.

## Phase 8 — Kubernetes, operations, and release engineering

- [ ] OPS-001 Finalize reproducible multi-architecture OCI build with pinned base digests, non-root user, read-only filesystem compatibility, CA roots, labels, and graceful shutdown.
- [ ] OPS-002 Generate SBOM, vulnerability report, checksum, provenance, and image-signing/verification hooks in CI with release thresholds.
- [ ] OPS-003 Create Kubernetes Deployment, Service, ServiceAccount, ConfigMap, secret references, and pre-deploy migration Job.
- [ ] OPS-004 Add restricted security context, seccomp, dropped capabilities, NetworkPolicies, resource requests/limits, topology spreading, and PodDisruptionBudget.
- [ ] OPS-005 Add readiness/liveness/startup probes that reflect process health, DB/policy readiness, and revocation freshness appropriately.
- [ ] OPS-006 Add optional HPA and ServiceMonitor/PodMonitor plus dashboards for API, policy, database, outbox, allocation, run, revocation, cache, and Go runtime signals.
- [ ] OPS-007 Define actionable alerts tied to SLOs with severity, ownership, runbook links, and anti-noise windows.
- [ ] OPS-008 Write runbooks for install/configure, migration, upgrade/rollback, backup/restore, policy rollback, revocation gaps, DB/OPA/Valkey outage, outbox backlog, key rotation, and break-glass.
- [ ] OPS-009 Test PostgreSQL backup/restore, point-in-time recovery assumptions, schema forward recovery, and restoration of epoch/outbox/allocation invariants.
- [ ] OPS-010 Run load tests for target API/policy latency, run admission throughput, allocation contention, SSE fanout, and outbox/revocation lag; tune documented limits.
- [ ] OPS-011 Run resilience tests for DB failover/latency, OPA outage/malformed response, Valkey loss, stream partitions, worker crash, pod eviction, and rolling restart.
- [ ] OPS-012 Run disposable-cluster install, restricted-container smoke, migration, core workflow, HPA/disruption, upgrade, rollback, and uninstall tests.
- [ ] OPS-013 Add release automation that produces immutable image digests, manifests/chart package, SBOM/provenance, API artifacts, checksums, and draft notes.
- [ ] OPS-014 Commit Phase 8 with operational docs, test reports, and artifact-generation evidence.

## Phase 9 — Release-candidate closure

- [ ] RC-001 Freeze the OpenAPI and policy decision contracts; run backward-compatibility and generated-artifact drift checks.
- [ ] RC-002 Run `make verify` from a clean checkout and archive unit, race, fuzz, policy, integration, contract, end-to-end, security, and Kubernetes smoke evidence.
- [ ] RC-003 Confirm SLO/capacity targets and document accepted performance envelopes and scaling guidance.
- [ ] RC-004 Confirm zero unresolved critical/high vulnerabilities, security findings, migration risks, flaky/skipped required tests, or fail-open paths.
- [ ] RC-005 Exercise and record install, upgrade, rollback/forward, backup/restore, policy rollback, revocation reconciliation, key rotation, and break-glass game days.
- [ ] RC-006 Reconcile every TODO item against implementation, tests, commits, docs, and release artifacts; resolve all blockers and deviations.
- [ ] RC-007 Update README with final architecture, supported versions, quick start, configuration, API docs, development commands, deployment, security, and support status.
- [ ] RC-008 Create numbered ADRs under `docs/adr/` for all durable decisions and relevant history extracted from `PLAN.md` and `TODO.md`.
- [ ] RC-009 Verify ADRs preserve all useful rationale, alternatives, consequences, risks, and implementation/commit references from the living plan.
- [ ] RC-010 Prepare RC release notes, known limitations, operator checklist, artifact inventory, upgrade notes, and proposed semantic version/tag.
- [ ] RC-011 Remove `PLAN.md` and `TODO.md`, validate links/docs and run `make verify` in the resulting tree.
- [ ] RC-012 Commit the final ADR/documentation transition to `main`, build the release artifacts from that exact commit, and verify digest/provenance consistency.

## Progress log

Append one entry per completed atomic item or tightly coupled group. Do not delete historical entries; supersede them with a later note.

| Date | TODO IDs | Commit | Verification evidence | Notes/deviations |
|---|---|---|---|---|
| 2026-08-10 | GOV-001–GOV-013 | `eddf453` | Redocly CLI 2.3.0 lint; `git diff --check`; blueprint coverage review | Exact dependency versions deferred to Phase 1 testing by design |
| 2026-08-10 | ENG-001 | `5287791` | `go version`; `go mod edit -json`; `go test ./...`; `git diff --check` | Go 1.26.5 tested; command behavior remains scoped to later Phase 1 items |
| 2026-08-10 | ENG-002 | `45c422d` | source-policy audit; `govulncheck -test ./...`; `go-licenses check --include_tests`; root/tools tests; `git diff --check` | No dependency exceptions; Make and CI wiring remain ENG-008/ENG-010 |
| 2026-08-10 | ENG-003 | `be384bc` | configuration/redaction tests; `go test -race ./...`; `go vet ./...`; source/license/vulnerability gates | Secret values are environment-only; command startup wiring remains ENG-007 |
| 2026-08-10 | ENG-004 | `f6c3a9e` | structured logging, correlation spoofing, recursive redaction and handler immutability tests; race/vet/source/license/vulnerability gates | Trace population and HTTP request-ID middleware remain ENG-005/ENG-007 |
| 2026-08-10 | ENG-005 | `215bfbf` | metrics and OTLP integration tests; `go test -race -cover ./...`; vet/source/license/vulnerability gates | Eight exact upstream pseudo-version exceptions expire 2027-02-10; metrics endpoint/server wiring remains ENG-007 |
| 2026-08-10 | ENG-006 | `8ff8f77` | UUIDv7, UTC, exact decimal/quantity, authenticated cursor, and typed-error tests; ID/decimal/cursor fuzz campaigns; race/vet/source/license/vulnerability gates | Primitives add no dependencies; HTTP RFC 7807 mapping and cursor-key configuration remain ENG-007 |
| 2026-08-10 | ENG-007 | `72319f1` | request/trace correlation, recovery/redaction, limits, RFC 7807, metrics, probe, readiness lifecycle, listener shutdown, OpenAPI, and process smoke evidence; full race/vet/build/source/license/vulnerability gates | Initial readiness is process-local; PostgreSQL, policy, and revocation freshness gates remain in their owning phases |
| 2026-08-10 | ENG-008 | `7e03c72` | Makefile contract tests; exact-version tool, generation/format/lint, unit/race/policy/integration/e2e, security, build, and aggregate verification evidence | `image` intentionally requires ENG-011's Dockerfile and remains outside `verify` until then; OPA/dependency-backed suites report their scheduled absence explicitly |
| 2026-08-10 | ENG-009 | `610088a` | immutable Compose pin/health/isolation contract tests; full PostgreSQL 18.4, OPA 1.19.0, and Valkey 9.1.1 runtime smoke; positive/negative authentication; `make verify` | Host ports are loopback-only; Valkey is opt-in and non-persistent; PostgreSQL state survives `dev-down` and only scoped `dev-reset` removes it; OPA starts policy-empty until Phase 3 |
| 2026-08-10 | ENG-010 | `47f8519` | CI contract tests; formatting/generation/lint, unit/race/policy/integration, dependency/vulnerability/license, binary and image jobs; `make verify` | Third-party actions use full commit SHAs; ENG-011 subsequently activated the image job as a mandatory build and runtime-smoke gate |
| 2026-08-10 | ENG-011 | `5869296` | Docker contract tests; pinned multi-stage build; OCI metadata; non-root/read-only/probe/SIGTERM runtime smoke; `make verify` | Distroless runtime intentionally has no shell; Go builder and runtime indexes support both required `linux/amd64` and target `linux/arm64`; multi-architecture publishing remains OPS-001 |

## Active blockers and deviations

- ENG-005 inherits eight exact pseudo-version dependencies from the pinned Prometheus/OpenTelemetry graph. Exceptions are version-bound in `dependency-policy.json`, expire 2027-02-10, and must be removed or renewed through review before that date.
