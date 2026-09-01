# Threat Model

## Scope and assets

This model covers the ThinkPixelAG API, its Kubernetes workload, PostgreSQL, optional Valkey, OPA policy artifacts/evaluation, identity verification, signing integration, execution-worker callbacks, revocation distribution, and evidence export.

Critical assets are agent/version approvals, tenant boundaries, caller and workload identity, policy bundles/activations, run state, resource balances and usage, revocations/epochs, signing authority, administrative approvals, idempotency records, and audit evidence.

The enterprise VPC/VPN is an exposure-reduction control only. A compromised internal workload is in the attacker model.

## Adversaries and assumptions

- An external caller with network reachability but no valid identity.
- A legitimate tenant principal attempting cross-tenant access or excess privilege.
- A compromised agent harness trying to expand resources, forge consumption, reuse a lease, or invoke undeclared tools/subagents.
- A compromised internal workload abusing VPC trust, SSRF, leaked tokens, or service endpoints.
- A malicious or compromised governance administrator.
- A dependency or supply-chain attacker modifying policy, images, modules, or build output.
- An operator making a configuration or rollout error.

We assume enterprise identity, Kubernetes control plane, PostgreSQL, and managed KMS can be configured as trusted dependencies, but explicitly handle their temporary unavailability. Complete compromise of all governance administration plus the independently administered evidence plane is outside the single-service protection boundary.

## Abuse cases and mitigations

| ID | Threat | Impact | Required controls | Verification |
|---|---|---|---|---|
| T01 | Spoofed user/workload or algorithm-confused JWT | unauthorized governance action | strict issuer/audience/algorithm/signature/time validation; workload identity; no IP trust | negative authentication tests |
| T02 | Tenant supplied in body/header conflicts with token | cross-tenant confused deputy | derive tenant from verified mapping; reject/ignore untrusted hints; tenant-scoped repositories | adversarial API/integration tests |
| T03 | Trusted gateway headers accepted from arbitrary peer | identity impersonation | accept forwarded context only from authenticated configured gateway and bind to original assertion | gateway-boundary tests |
| T04 | Raw delegation token logged in payload | credential disclosure/replay | Authorization header only; body schema rejects token fields; centralized redaction | schema and log-capture tests |
| T05 | Caller selects unapproved historical agent version | vulnerable code execution | policy-driven server resolution; explicit pin requires privileged action; immutable versions | resolution and policy tests |
| T06 | Rego bundle or decision tampered/malformed | broad authorization bypass | content digest/signature, controlled activation, typed input/output, deny on error | signature/schema/outage tests |
| T07 | Stale cached ALLOW survives revocation | revoked access continues | epoch/version cache keys, bounded TTL, push plus reconciliation, freshness gate | partition/expiry/revocation tests |
| T08 | Parallel child reservations oversubscribe parent | spend/compute abuse | authoritative PostgreSQL transaction, deterministic locking/conditional update | high-concurrency invariant tests |
| T09 | Duplicate/negative/untrusted meter event alters balance | fraud or denial of service | authenticated trusted producer, unique event ID, checked nonnegative units, append-only ledger | replay/property tests |
| T10 | Concurrent settlement credits unused budget twice | resource expansion | unique settlement and atomic reservation closure/parent credit | concurrent settlement test |
| T11 | Stale worker uses expired lease to mutate run | state corruption | bounded lease plus monotonically increasing fencing token | worker-race tests |
| T12 | Idempotency key reused with different request | unintended action replay | scope key by tenant/principal/route and normalized request hash; conflict on mismatch | concurrent replay tests |
| T13 | Identifier enumeration reveals another tenant | confidentiality loss | authorize before detail disclosure; enumeration-safe not-found | cross-tenant contract tests |
| T14 | SSRF/internal client reaches admin endpoints | privilege escalation | authentication and per-action policy on every route; egress controls; strict URL configuration | authorization/security tests |
| T15 | Admin unilaterally changes trust roots, global revocation/policy, emergency authority, or privileged agent classes | governance compromise | JIT strong identity, digest-bound four-eyes state, single-use approval, monotonic epochs, independent evidence | approval-bypass tests/game day |
| T16 | Shared/exported signing key compromise | forged trusted assertions | provider-neutral digest signing, KMS/HSM non-exportable keys, separated grants, version/algorithm binding, no private-key file configuration | configuration/metadata substitution tests and rotation drill |
| T17 | Audit event lost or altered with transaction | repudiation | audit/outbox in business transaction, immutable IDs/integrity metadata, independent sink | crash/replay and sink verification |
| T18 | Sensitive request/policy content leaks through telemetry | data/credential exposure | classification, allowlisted fields, redaction, no raw bodies/objectives | telemetry capture tests |
| T19 | Dependency outage induces fail-open fallback | unauthorized operation | explicit failure matrix, deny protected operations, readiness/freshness gates | chaos/resilience tests |
| T20 | Malicious dependency/image/build artifact | code execution | pinned versions/digests, checksums, SBOM, scanning, provenance/signing | CI supply-chain gates |
| T21 | Migration or restore breaks epoch/allocation invariants | authorization/spend bypass | explicit migration job, constraints, compatibility policy, restore rehearsal | migration/restore tests |
| T22 | Unbounded input/SSE clients exhaust service | availability loss | body/header/time/concurrency limits, pagination, backpressure, quotas | load/abuse tests |

## Phase 7 implementation review

The Phase 7 exit review on 2026-09-01 assessed every abuse case against the
implemented control and its executable evidence. The detailed traceability and
residual-risk register are recorded in
[Phase 7 governance self-protection evidence](../phase-7-evidence.md).

- **Critical findings:** none open.
- **High findings:** none open.
- **Medium residual risks:** production network-policy enforcement and
  production-shaped abuse/load qualification (T14/T22), release SBOM,
  provenance, and image-signing automation (T20), and backup/restore/PITR
  rehearsal (T21). These are explicit Phase 8 deployment and operational
  acceptance work and do not provide an application-layer authorization
  bypass in the reviewed RC implementation.

T03 is implemented as a narrower boundary than its original candidate
mitigation: trusted service routes reject forwarding identity entirely and
derive workload authority from an exact deployment-owned binding to a verified
client-certificate URI SAN. A future gateway assertion-forwarding design would
require a new versioned contract and a fresh threat review.

## STRIDE summary

- **Spoofing:** T01–T04; strong human/workload identity and token binding.
- **Tampering:** T06–T11, T16–T17, T20–T21; signatures, transactions, ledgers, fencing, provenance.
- **Repudiation:** T12, T15, T17; idempotency and independently exported evidence.
- **Information disclosure:** T02, T04, T13, T18; tenant scoping and redaction.
- **Denial of service:** T19, T21–T22; bounded work, backpressure, recovery and conservative degradation.
- **Elevation of privilege:** T03, T05–T07, T14–T16; policy, freshness, least privilege, and dual control.

## Security acceptance criteria

- No protected endpoint is reachable without a verified principal/workload and an explicit policy decision.
- Tenant context is never selected by request payload.
- No cached decision creates a new ALLOW beyond its policy/revocation validity.
- No concurrency schedule can expand resource balances or decrement epochs.
- Privileged actions are attributable; defined high-impact actions require a second independent approval.
- Four-eyes approvals are provider-verified, expire, bind the exact proposed-action digest, and cannot be self-approved or reused; see [the governance approval contract](../contracts/governance-approvals.md).
- Required evidence survives API/publisher crashes and can be reconciled by event ID.
- Critical/high exploitable findings and undocumented fail-open behavior block release-candidate status.

Review this model at every phase exit and whenever a trust boundary, privileged action, dependency, or sensitive data flow changes.
