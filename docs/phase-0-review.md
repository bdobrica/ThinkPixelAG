# Phase 0 Blueprint Review

Reviewed on 2026-08-10 against `06-the_agent_governance_plane.md` from the Enterprise Execution Platform for AI Agents blueprint.

## Question coverage

| Blueprint question/capability | Contract location | Phase 0 result |
|---|---|---|
| Which agents exist; owner/sponsor/risk | registry model and OpenAPI agent schemas | covered |
| Approved implementation versions | immutable version lifecycle and server resolution | covered |
| Who may invoke | verified identity plus typed OPA action | covered |
| Allowed tools/skills/models/subagents | version manifest and policy/resource input | covered |
| Execution/spending limits | typed vector, narrowing, accounting equations | covered |
| Tenant ownership | verified token/workload mapping; tenant-scoped persistence | covered |
| Active runs and lifecycle | run state machine and lifecycle API | covered |
| Scoped revocation | nine scopes, monotonic epochs, stream/reconcile | covered |
| Stable run API | OpenAPI public lifecycle paths | covered |
| No body delegation token | schemas exclude it; security/redaction contract | covered |
| Policy-resolved version | admission contract; pins are privileged | covered |
| Protocol adapters | REST canonical; adapters remain post-RC extension | explicitly out of RC scope |
| Authoritative allocation vs enforcement | PostgreSQL allocation contract; harness only enforces envelope | covered |
| Atomic child reservation/reclaim | transaction algorithm and invariants | covered |
| Exhaustion states | complete state transitions and actors | covered |
| Revocation push plus reconciliation | resumable SSE and authoritative reconciliation | covered |
| Fail-closed freshness | operation risk table and health semantics | covered |
| Governance self-protection | threat model, approval/KMS/evidence requirements | covered as Phase 7 implementation scope |

## Trust-source decisions

- Identity, principal, and tenant: verified OIDC/workload assertion mapped by trusted configuration.
- Agent/version/capability/approval: PostgreSQL authoritative records plus policy decision.
- Policy: digest/signature-verified active artifact and typed OPA response.
- Revocation/epochs: PostgreSQL authoritative transaction/log; streams/caches are derived.
- Resources/usage: PostgreSQL ledger; only authenticated trusted meters submit consumption.
- Time: service/database UTC plus monotonic elapsed clocks for freshness.
- Signing: managed KMS/HSM provider; no production file key.
- Evidence: transactional outbox plus independently administered sink.
- VPC/VPN, source IP, arbitrary forwarding header, client tenant field, harness self-report, and Valkey are not authority.

## Resolved Phase 0 ambiguities

- PostgreSQL is the source of truth; Valkey is optional acceleration.
- OPA evaluates authorization and constraint narrowing; Go validates typed output and domain non-expansion independently.
- The RC is a modular monolith with extraction seams, not premature microservices.
- SSE is the first ordered/resumable stream transport; domain semantics remain transport-neutral.
- Authentication is required inside the corporate network.
- High-risk writes require live authoritative revocation state and zero cached-ALLOW TTL.
- Policy bundles may initially be stored in PostgreSQL behind an artifact interface; activation/digest/signature semantics are storage-independent.

## Deferred decisions with gates

These are not ambiguous trust sources and do not block Phase 0:

- Exact supported dependency versions are selected/pinned/tested in Phase 1 (`ENG-001`, `ENG-002`, `ENG-009`).
- Go libraries, migration/query tooling, and embedded-versus-sidecar OPA mechanism are selected through implementation evidence/ADRs (`DATA-001`, `POL-001`).
- PostgreSQL RLS is evaluated after repository isolation exists (`DATA-012`).
- Exact policy bundle artifact backend can remain PostgreSQL for RC if size/performance tests pass.
- Final capacity figures may be tightened/revised by Phase 8 tests, but security freshness limits cannot be loosened through performance tuning.

## Phase 0 exit conclusion

The architecture, threat/data rules, versions policy, state machines, resource/revocation/policy invariants, API surface, and SLO targets form a coherent implementation baseline. No caller-controlled source is treated as identity, tenant, authorization, resource, or revocation authority. Phase 1 may begin once document/OpenAPI validation passes and the Phase 0 commit is recorded in `TODO.md`.
