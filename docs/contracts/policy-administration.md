# Policy administration

The local-development composition implements the existing OpenAPI policy upload
and activation operations. Production managed-key adapters remain a deployment
prerequisite; local software keys are never accepted by the managed-key guard.
See [ADR-0016](../adr/0016-local-development-policy-promotion.md).

## Signed artifacts and runtime selection

`POST /v1/admin/policies` accepts the signed, content-addressed envelope in
`PolicyUploadRequest`. Artifact and signature fields are base64. The artifact is
at most 1 MiB of UTF-8 Rego, with exactly one `package thinkpixelag.authorization`
declaration. This RC supports self-contained modules using `input`; external
`data`, `http`, `net`, `opa` namespace tokens and the
reserved `__ag_envelope` name are rejected. These conservative restrictions also
apply to comments containing those tokens. Signed bytes are stored unchanged.

OPA compilation uses a derived package unique to the artifact digest. Loading
that module does not activate it. Each allowed decision is evaluated through the
PostgreSQL-selected immutable artifact, verifies the exact stored signature and
loaded source, and carries a digest-bound response wrapper. A concurrent policy
activation invalidates a decision still in flight. Missing modules are reloaded
from verified storage; a conflicting module fails closed instead of being
silently overwritten. No policy namespace switch or peer restart is required.
The adapter uses OPA's [policy and data APIs](https://www.openpolicyagent.org/docs/rest-api).

`POST /v1/admin/policies/{policy_digest}/activations` selects a validated artifact
in the configured channel. First activation of an artifact uses the explicit
`approval_reference: "not-required"`; this does not bypass a rollback approval.
Reactivating a previously active artifact requires the ID of an unexpired,
independently approved `POLICY_ROLLBACK` request. The approval digest binds tenant,
channel, target digest and expected current activation version; consumption and
activation commit atomically. The authenticated requester must match the
approval requester. Approval submission/read APIs are implemented in RC-104.

## Authorization, replay and evidence

Both operations authenticate the caller and require the applicable live
`policies.manage` or `policies.activate` decision. The application authorizes
again when invoked without HTTP. No raw policy source enters policy input or
audit/outbox metadata. Every mutation checks the authorizing activation inside
its transaction and commits evidence and its small idempotency response together.

`Idempotency-Key` is required. A same-principal, same-operation replay returns the
original response; changed content conflicts. Policy epochs increase on
activation. Artifact records and closed activation intervals are immutable in
schema 19. Invalid signatures/contracts/source return a bounded error, without
returning compiler output that could expose protected source.

## Compatibility and scope

The existing wire schemas and policy decision version remain unchanged.
The new optional `local_policy_key` runtime setting selects the explicit local
profile in `local`/`test` environments only; production startup rejects it.
Deployments without that setting retain their existing static policy composition.
The optional console is not required. Initial operator provisioning and editor
workflows are tracked separately in [TODO.md](../../TODO.md).
