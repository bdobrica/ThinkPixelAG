# Security guide for operators

AG assumes that harness output and agent-controlled state are untrusted.
Identity, policy and authoritative state determine access; unavailable or stale
security dependencies deny protected work. Start with the
[threat model](threat-model.md) to understand these boundaries.

| Operational task | Reference and expected outcome |
|---|---|
| Configure human/application callers | [Authentication](authentication.md): pinned OIDC issuer/audience, verified tenant membership, explicit role mappings |
| Configure service callers | [Workload identity](../contracts/workload-identity.md): separate mTLS listener, reviewed URI-SAN bindings; no forwarded identity headers |
| Deploy or roll back policy | [Policy lifecycle](policy-lifecycle.md): validate and sign bundles, approve a digest, activate monotonically |
| Deliver secrets and collect telemetry | [Data classification](data-classification.md): keep keys/tokens/payloads out of agent state, logs, metrics and Git |
| Select or update dependencies | [Dependency policy](dependencies.md) and [tested versions](../operations/supported-versions.md): pinned artifacts and verified upgrades |
| Validate isolation and failure behavior | [Adversarial testing](adversarial-testing.md): denial, tampering, replay and stale-state coverage |
| Rotate keys or recover privileged access | [Runbooks](../operations/runbooks.md#key-rotation), [managed signing](../contracts/managed-signing.md), [break glass](../contracts/break-glass.md) |

Before accepting traffic, verify TLS trust, tenant isolation, role mappings,
default-deny networking and independent evidence delivery. Keep migration and
runtime database privileges separate. The RC uses repository-enforced tenant
predicates; it does not claim PostgreSQL RLS enforcement.

Use the [incident guide](../operations/incidents.md) when these controls fail.
A performance deferral never permits fail-open authorization or evidence loss.
The [dated RC risk review](../evidence/rc/risk-review.md) records scan results
and provider qualification limits; refresh deployment-specific checks before
promotion. Test fixtures and software signing keys are not production identity
or key-management integrations.
