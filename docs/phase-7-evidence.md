# Phase 7 Governance Self-Protection Evidence

Phase 7 exited on 2026-09-01 after a threat-model review of the implemented
identity, authorization, privileged approval, signing, evidence, break-glass,
workload-identity, and redaction boundaries. No unresolved critical or high
finding remains in the reviewed release-candidate application boundary.

The SEC-011 phase-completion commit containing this document is the audited
tree. A subsequent ledger-only commit records its short SHA in `TODO.md` and
`PLAN.md`.

## Review method and severity

Each threat in `docs/security/threat-model.md` was traced to an implemented
control and at least one executable proof. A finding is **critical** when it
permits unauthenticated cross-tenant or governance takeover, or undetectable
systemic authority expansion. A finding is **high** when a realistic
authenticated or internal attacker can bypass authorization, dual control,
revocation, resource conservation, signing integrity, or required evidence.
Operational hardening that does not create such a bypass is retained below as
explicit medium residual risk for Phase 8 rather than being treated as an
implemented control.

## Threat-to-evidence traceability

| Threat | Implemented control and executable proof | Review disposition |
|---|---|---|
| T01 | Pinned OIDC issuer/audience/algorithm and bounded key freshness; exact verified-chain mTLS URI-SAN binding. `TestVerifyRejectsAuthenticationFailures`, `TestKeyRotationAndBoundedOutage`, and `TestVerifierRequiresVerifiedExactBoundURISAN`. | Closed |
| T02 | Verified tenant mapping, rejected body/header hints, tenant-bound repositories and composite keys. `TestAuthenticateBearerMapsPrincipalAndRejectsHints` and `TestTenantRepositoriesRollbackAndIsolation`. | Closed |
| T03 | Workload routes reject bearer and forwarding identity and accept only deployment-owned mTLS bindings. `TestAuthenticateWorkloadRejectsBearerAndForwardedIdentity` and `TestVerifierRejectsAmbiguousAndOverprivilegedBindings`. | Closed with the narrower no-forwarding design documented in the threat model |
| T04 | Strict request schemas plus recursive cross-sink redaction. `TestSensitiveFieldsAreRedactedRecursively`, `TestTracerDropsRestrictedNamesAttributesEventsAndErrors`, and HTTP boundary tests. | Closed |
| T05 | Policy-driven approved-version resolution and separately authorized controlled pins. `TestVersionResolverFailsClosedForUnauthorizedPin`, Rego unapproved-version and version-pin cases, and `TestVersionResolutionEligibilitySnapshotsIsolationAndImmutability`. | Closed |
| T06 | Digest-, signature-, kind-, schema-, revision-, key-, and algorithm-bound artifacts plus strict OPA output. `TestVerifyBindsAllArtifactIdentity`, `TestClientRejectsAdversarialOutput`, and policy activation integration. | Closed |
| T07 | Epoch/version-bound cache keys, bounded TTL, synchronous invalidation, and authoritative high-risk evaluation. Cache poison, epoch-change, expiry/recovery, and freshness partition tests. | Closed |
| T08 | Authoritative transactional reservations with deterministic locking and checked quantities. Resource invariant tests and the Phase 5 concurrent PostgreSQL workflow. | Closed |
| T09 | Workload-authenticated producer identity, nonnegative typed usage, unique producer event identity, and append-only ledger. `TestTrustedUsageAuthenticatesAuthorizesAndPersistsProducer` and Phase 5 integration. | Closed |
| T10 | Atomic single settlement and parent credit. Settlement replay/concurrency proofs in the Phase 5 PostgreSQL workflow. | Closed |
| T11 | Expiring leases and monotonic fencing before worker mutation. `TestRunWorkerRejectsExpiredLeaseAndInvalidConfiguration` and worker operation tests. | Closed |
| T12 | Tenant/principal/route/request-hash-bound idempotency with mismatch conflict and concurrent ownership. `TestIdempotencyConcurrencyReplayConflictAndExpiry`. | Closed |
| T13 | Tenant-scoped lookup and enumeration-safe denial/not-found behavior. `TestRunQueryMakesMissingAndDeniedEnumerationSafe`, agent discovery, and repository isolation tests. | Closed |
| T14 | Authentication and exact action policy cover every implemented protected route; trusted routes require workload identity. Rego least-privilege cases, HTTP authorization tests, and `make test-security`. | Application control closed; production egress/NetworkPolicy qualification remains medium Phase 8 work |
| T15 | Provider-verified, digest-bound, expiring, independent, single-use four-eyes approval; narrow break-glass with separate evidence. Approval, break-glass, immutable-evidence, and concurrent-consumption tests. | Closed |
| T16 | Provider-neutral managed signer/verifier ports reject exportable keys and metadata substitution; production configuration rejects file/private-key input. Managed cryptography and configuration tests. | Closed; production rotation drill remains Phase 8 operations work |
| T17 | Business mutation, audit, and outbox commit atomically; stable event replay, fenced delivery, hash links, strict receipts, and monotonic checkpoints. PostgreSQL transactional outbox and evidence-delivery tests. | Closed |
| T18 | Closed allowlists and fail-closed restricted-field checks across logs, traces, metrics, errors, policy input, audit, paths, and panic recovery. SEC-009 sentinel suite. | Closed |
| T19 | Protected operations deny on identity, policy, cache, signing, evidence-sink, and freshness failures; readiness reflects security freshness. OIDC outage, OPA failure, cache poison, freshness partition, and sink failure tests. | Closed |
| T20 | Immutable dependency/image pins, module-source policy, vulnerability/license scans, signed privileged artifacts, and reproducible build checks in `make verify`. | Current application/build inputs closed; release SBOM/provenance/image-signing automation remains medium Phase 8 work |
| T21 | Ordered checksum-locked migrations, forward recovery, database constraints, and invariant integration tests. `TestMigrationQualification` and migration/schema gates. | Migration path closed; backup/restore/PITR rehearsal remains medium Phase 8 work |
| T22 | Header/body/deadline limits, strict decoding, bounded pagination/streams, and bounded worker batches. HTTP limit tests and stream pagination/retention tests. | Static abuse controls closed; production-shaped load qualification remains medium Phase 8 work |

## Phase 7 control outcomes

Privileged actions use non-hierarchical roles and authoritative freshness.
Defined high-impact changes consume exact, independently approved action
digests. Managed signing binds non-exportable key metadata and versioned
artifacts. Evidence is minimized, transactionally enqueued, independently
exportable, replay-safe, receipt-bound, and tamper-evident. Break-glass grants
are recovery-only, strongly authenticated, one-time-credential protected, and
expire within fifteen minutes. Service-to-service authority comes from exact
mTLS workload bindings, never caller-controlled headers. Cross-sink redaction
tests prevent restricted data from entering telemetry, errors, policy records,
audit metadata, or panic output.

`make test-security` makes the Phase 7 bypass set a stable repository gate. It
covers privilege escalation, approval bypass, replay, cache poisoning, policy
tampering, stale authorization, and evidence suppression through Go, Rego, and
real-PostgreSQL proofs.

## Residual-risk register

| Threats | Residual work | Severity | Owner / acceptance item |
|---|---|---|---|
| T14, T22 | Qualify production egress controls, NetworkPolicies, quotas, backpressure, and load/abuse limits. | Medium | Phase 8 OPS-004 and OPS-010 |
| T20 | Generate and verify release SBOM, provenance, checksums, and image signatures. | Medium | Phase 8 OPS-002 and OPS-013 |
| T16, T21 | Exercise production key rotation and PostgreSQL backup/restore/PITR recovery while preserving epochs, outbox, and allocation invariants. | Medium | Phase 8 OPS-008 and OPS-009 |

These risks are release-tracked and must not be represented as completed
production controls. None supplies an unresolved critical/high application
bypass in the audited Phase 7 tree.

## Qualification commands

The focused and aggregate acceptance commands are:

```sh
make test-security
make verify
```

The focused gate passed all 26 Rego cases, all selected Go packages, and the
three real-PostgreSQL evidence/approval integration cases against the pinned
local PostgreSQL 18.4 and OPA 1.19.0 dependencies. The aggregate gate also
passed. No production identity, KMS, evidence sink, policy, or database
participated in qualification.
