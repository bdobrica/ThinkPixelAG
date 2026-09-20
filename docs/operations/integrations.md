# Connect callers, harnesses and ThinkPixel components

AG governs Runs and persists their authority. ThinkPixelAR owns execution and
harness lifecycle; AG does not launch Codex. The current RC has no versioned AR
worker API. A real Codex integration therefore requires the AR adapter work;
there is no AG command that directly attaches or starts a Codex worker.

## Public clients

Configure an OIDC issuer and audience, verified tenant membership and explicit
role mappings as described in [authentication](../security/authentication.md).
Enable the [governed runtime](configuration.md#governed-runtime-composition).
Use the [OpenAPI](../../api/openapi/thinkpixelag.yaml) and
[Run lifecycle examples](../api/run-lifecycle.md) for admission, reads, signals,
cancellation and streams. Preserve idempotency keys for retries of the same
operation. Do not generate a new key merely because a response timed out.

The runtime composes agent discovery/approval, Run admission/query/signals/
cancellation/events, resource extension and revocation. Registration and policy
management exist as internal services but are not mounted runtime APIs. Approved
agent/policy provisioning is a prerequisite, not a public-client setup endpoint.

## Trusted services

Configure the separate mTLS listener with its own certificate, client CA and
[URI-SAN workload bindings](../contracts/workload-identity.md). Route it only to
trusted callers; do not terminate client identity into caller-controlled headers.
Bindings are loaded on startup, so distribute and roll them out consistently.

Trusted usage, resource settlement and revocation distribution use their
published contracts. Consumers persist checkpoints, reconcile gaps against
snapshots and enforce operation-specific freshness. A successful transport
connection does not grant a tenant, role or resource envelope.

## Replaceable integration boundaries

| Integration | Operator responsibility |
|---|---|
| ThinkPixelAR / harness | Implement the versioned execution boundary when available; retain AG admission identity, enforce granted constraints and fence stale owners |
| Model/tool gateways | Authenticate as trusted workloads; enforce authority/freshness and report usage using [resource accounting](../contracts/resource-accounting.md) |
| OPA | Serve the validated policy matching the active approved digest and [decision contract](../contracts/policy-decision.md) |
| Evidence receiver | Implement [delivery/receipt semantics](../contracts/evidence-events.md); keep stable event IDs through retries and preserve durable checkpoints |
| Identity and managed signing providers | Configure replaceable adapters and trust distribution; qualify the selected provider's real ceremonies |

The [platform composition](../architecture/platform-composition.md) and
[ALIGNMENT](../../ALIGNMENT.md) define the remaining peer boundaries. No component
may read AG's database directly or expand Run authority through Skills, memory,
Workspace membership or harness output. The [deferred integration scenarios](../adr/0012-integration-rc-qualification-deferrals.md)
remain prerequisites for the corresponding whole-platform production claim.
