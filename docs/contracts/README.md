# Contract guide

These documents describe the agreements AG enforces at its boundaries and in
its authoritative state. Read the relevant contract before implementing a
client, adapter or peer component. A domain contract is not necessarily an HTTP
endpoint: the [integration guide](../operations/integrations.md) distinguishes
the current runtime from intended platform composition.

| Contract | Applies to / who uses it |
|---|---|
| [OpenAPI](../../api/openapi/thinkpixelag.yaml) and [Run examples](../api/run-lifecycle.md) | Public/trusted HTTP clients: request bodies, responses, errors, authentication and replay |
| [Domain model](domain-model.md) | AG application and persistence adapters; agent/version and Run state transitions |
| [Primitives](primitives.md) | Every adapter handling stable identifiers, digests, epochs, cursors or idempotency |
| [Policy decision](policy-decision.md) | AG-to-OPA input/output; policy authors implement this independently versioned boundary |
| [Resource accounting](resource-accounting.md) | AG, runtime and metering adapters; reservation, usage, settlement and conservation |
| [Revocation](revocation.md) | Gateways/runtime consumers; ordered distribution, freshness and snapshot reconciliation |
| [Workload identity](workload-identity.md) | Trusted service operators; mTLS URI-SAN bindings and allowed service roles |
| [Evidence events](evidence-events.md) | Evidence producers and independent sinks; stable event identity, receipts and checkpoints |
| [Signed artifacts](signed-artifacts.md) | Signers and verifiers; canonical envelopes, purpose binding and trust metadata |
| [Managed signing](managed-signing.md) | KMS/HSM adapters and key custodians; non-exportable signing authority |
| [Governance approvals](governance-approvals.md) | Management integrations and approvers; digest-bound four-eyes authorization |
| [Break glass](break-glass.md) | Recovery operators and privileged adapters; narrow, expiring, single-use recovery |

Machine-readable [JSON Schemas](../../api/schemas) cover evidence, delivery,
workload identity and break glass. Validate the declared schema/version rather
than accepting unknown fields or guessing meanings. OpenAPI is frozen at
`0.1.0-rc.1`; policy retains `thinkpixelag.authorization/v1alpha1`.
[Compatibility policy](compatibility.md) explains the freeze and checks.

AG owns the governance database. Peer components use versioned contracts and
stable IDs, never direct database access or Go `internal` types. Skills,
marketplace metadata and harness output cannot grant Run authority. See
[ALIGNMENT](../../ALIGNMENT.md) and [ADRs](../adr/README.md) for ownership and rationale.

[Policy administration](policy-administration.md) explains signed upload,
activation, replay and exact-artifact OPA selection in the local development profile.
