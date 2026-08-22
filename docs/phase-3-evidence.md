# Phase 3 Identity, Policy, and Registry Evidence

Phase 3 exited on 2026-08-22 after qualification of authenticated tenant
authority, fail-closed policy evaluation, governed agent-version state, and
policy-driven discovery and resolution. Commit `c91ed61` is the composed
security-workflow baseline; the REG-007 phase-completion commit containing this
document is the audited tree and also contains the concurrency correction found
by its exit gate. A subsequent ledger-only commit records that short SHA without
changing the qualified behavior.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.6 linux/amd64` |
| PostgreSQL | `18.4` on the pinned `postgres:18.4-alpine3.23` image |
| OPA | `1.19.0` |
| Valkey | `9.1.1`, optional cache qualification |
| Race detector | enabled for the complete Go race suite |

PostgreSQL and Valkey used disposable test data and loopback password
authentication. The PostgreSQL named development volume was preserved after
verification; no production service or data participated in the gate.

## Identity and tenant authority

The OIDC boundary verifies issuer discovery, a same-origin JWKS endpoint,
configured audience and asymmetric algorithms, token times, key rotation, and
bounded known-key use during provider outages. Only verified claims enter the
application boundary. Tenant, principal, role, and forwarding-header identity
hints from clients are rejected, and external roles acquire authority only
through the configured mapping.

Tests cover missing and malformed credentials, invalid signatures and claims,
unknown and rotated keys, discovery/JWKS failure, stale keys, forwarded-identity
forgery, tenant confusion, and concurrent verifier use. Protected registry
operations deny when authentication cannot establish trusted authority.

## Policy lifecycle and evaluation

The `thinkpixelag.authorization/v1alpha1` Go and Rego boundary validates bounded
typed input and output, registered reason codes, obligations, evidence binding,
constraint non-expansion, evaluation deadlines, and decision TTLs. Missing,
malformed, stale, mismatched, or unavailable policy fails closed.

Policy artifacts are content-addressed and signature/validation gated.
Activation and rollback append monotonic PostgreSQL activation history; rollback
never rewrites an earlier activation. Process-local active-policy metadata has
a bounded freshness age. The baseline policy's 19 tests cover allow and deny,
tenant isolation, stale and gapped security state, privileged version actions,
and constraint narrowing.

The optional Valkey cache is disposable acceleration. Keys bind the policy
contract and activation versions, relevant epochs, verified subject, action,
resource, and normalized input. Values are HMAC authenticated, ALLOW TTL is
bounded by all freshness limits, and cache outage, corruption, forgery, or
poisoning falls back to live policy evaluation rather than producing authority.

## Registry and public API behavior

Agent records validate active same-tenant owner and sponsor principals, risk
class, lifecycle transitions, and optimistic updates. Implementations are
canonical schema-v1 manifests whose content and image digests, declarations,
and limits are checked before atomic registration. PostgreSQL triggers prevent
version or capability updates and deletes.

Approval, rejection, deprecation, and revocation require an authoritative
`versions.approve` decision and append the lifecycle decision, audit record,
and outbox message atomically. Automatic resolution considers approved versions
newest first. Explicit historical pins require `versions.pin`; deprecated
rollback requires `versions.rollback`; both must agree with the invocation
decision's active policy version. The immutable per-run resolution snapshot
binds the version, approval, policy, decisions, mode, and narrowed constraints.

The implemented HTTP transport exposes authenticated `GET`/`HEAD
`/v1/agents`, authenticated `GET`/`HEAD /v1/agents/{agent_id}`, and governed
`POST /v1/admin/agents/{agent_id}/versions/{version_digest}/approvals` handlers
when their dependencies are assembled. Discovery first authorizes catalog
access, then filters each active agent through current eligibility and policy;
opaque cursors are authenticated and detail denials are enumeration-safe. The
OpenAPI 3.1 document remains the canonical full API contract. Run admission and
the remaining lifecycle transport assembly belong to Phase 4.

## Qualification commands and results

The following acceptance commands completed successfully against the pinned
dependencies:

```sh
go test -count=10 -tags=e2e ./internal/adapters/postgres -run TestPhase3RegistrySecurityWorkflow
make test-e2e test-integration
make verify
```

The clean-tree `make verify` gate passed generation drift, formatting, vet and
module checks, OpenAPI validation, unit and full race suites, all 19 OPA tests,
real-PostgreSQL integration and composed Phase 3 end-to-end tests, Compose
validation, dependency source/license/vulnerability gates, static build, image
build, and hardened container smoke. No required test was skipped.

The composed end-to-end workflow additionally proved deny-by-default identity
and policy behavior, governed approval, invocation-role enforcement, immutable
version storage, tenant isolation, correct constraint narrowing, authorization
cache reuse and epoch/activation invalidation, and durable tenant-private
resolution evidence. Test-owned outbox rows are cleaned within their tenant so
the end-to-end and integration suites remain order independent.

The phase-exit gate also exposed and corrected a PostgreSQL `READ COMMITTED`
snapshot race in concurrent version decisions. Version locking and current-state
evaluation now occur as separate statements in one transaction, so a waiter
observes the committed winning transition before attempting its own. The
focused real-database concurrency test passed 20 consecutive runs after the
correction, and the complete gate then requalified the atomic evidence path.

## Exit conclusion

AUTH-001 through AUTH-003, POL-001 through POL-005, and REG-001 through REG-007
satisfy the Phase 3 exit conditions for deny-by-default behavior, tenant
isolation, policy freshness, immutable versions, governed eligibility,
authorized discovery, and policy-driven resolution. Phase 4 can build run
admission and lifecycle APIs on these qualified boundaries.
