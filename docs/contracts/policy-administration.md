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

## Editor and local approvals

The optional local profile now also exposes:

| Operation | API |
|---|---|
| List metadata, read exact source | `GET /v1/admin/policies`, `GET /v1/admin/policies/{digest}`, `GET /v1/admin/policies/{digest}/source` |
| Read activation history/current | `GET /v1/admin/policy-activations`, `GET /v1/admin/policy-activations/current` |
| Create/list/read/revise drafts | `POST/GET /v1/admin/policy-drafts`, `GET/PUT /v1/admin/policy-drafts/{id}` |
| Validate/promote reviewed source | `POST /v1/admin/policy-drafts/{id}/validation`, `POST /v1/admin/policy-drafts/{id}/promotions` |
| Request rollback approval | `POST /v1/admin/policies/{digest}/rollback-approvals` |
| Inspect/decide approval | `GET /v1/admin/approvals/{id}`, `POST /v1/admin/approvals/{id}/decisions` |

Read/list/source operations require live `policies.manage` authorization.
Lists are bounded to 100 items and use authenticated cursors bound to the caller,
tenant, channel and endpoint. List projections exclude source; source is returned
only by explicit detail operations. History is paginated by stable activation ID;
`policy_epoch` establishes authoritative activation order.

Draft saves require `source` and `expected_revision` (zero for creation). Every
save appends an immutable revision; stale writes conflict. A `revision` query on
draft detail selects an exact revision, with zero/absence selecting the latest.
Validation compiles without activating. Promotion requires the exact `revision`,
`digest` and positive `artifact_revision`; it signs that immutable revision with
the development key and calls the same verified upload service. A later draft
edit cannot change the promoted bytes. Draft source never enters idempotency
response storage or audit/outbox metadata.

Rollback requests supply `expected_policy_epoch`, `lifetime_seconds` (1–3600)
and `reason_code`. The returned approval identifies the exact action digest.
Another operator uses `{"approved":true}` or `false` to decide it; the caller's
verified identity supplies the approver. AG records an immutable authenticated
receipt and verifies it through the local ApprovalProvider adapter before
recording the decision. A free-form approval reference is insufficient.
Requesters cannot approve themselves. Expired or consumed approvals cannot be
used; the API projects expiry without rewriting the append-only history.

Schema 20 adds draft revisions and authenticated approval receipts. OIDC-backed
local receipts exercise the protocol but do not establish an independent
enterprise approval service or production MFA qualification.
