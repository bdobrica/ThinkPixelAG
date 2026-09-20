# Harness guidance v1

[ADR-0013](../adr/0013-dynamic-harness-instructions.md) owns this additive contract.
[OpenAPI](../../api/openapi/thinkpixelag.yaml) defines the complete schemas:

- `GET /v1/harness/capabilities` returns `thinkpixelag.harness-guidance/v1`
  structured capabilities and the corresponding Markdown.
- `GET /v1/harness/instructions` returns that trusted Markdown as `text/markdown`.
- Optional `run_id` selects a caller-owned Run. Optional `contract_version`
  defaults to v1; unsupported versions and unknown/duplicate query fields fail.

Both routes use the normal verified OIDC identity and live policy/revocation
checks. Discovery requires the existing `agents.list` permission. Run scope
additionally requires the existing `runs.read` permission and the Run requester
to be the current principal. Cross-tenant/other-caller Run contexts return 404.
There is no new role, implicit administrator permission, or workload identity.

## Meaning and sources

The scope contains verified tenant/principal IDs and an optional Run ID. The
fixed operation catalog describes mounted AG governance operations, with
prerequisites and `authorization: checked_per_request`. Presence is **not** a
permission grant. `governance: ready` means this discovery request passed current
AG authorization/readiness checks; it does not assert peer health or authority
to execute a later call. `execution: unsupported` explicitly preserves the
remaining AR/gateway handoff gap. No configured peer URL invents a capability.

Runtime composition controls Run-list presence. Current terminal/deadline Run
state removes cancel/signal guidance. Mapping, active policy, effective OPA
configuration and Run/envelope revisions invalidate the capability revision.
Role removals are enforced by ordinary authentication/authorization even when a
harness retains older text. No administrative/source/secret endpoints are
advertised to the harness.

Only trusted templates, closed operation names and validated identifiers/states
are rendered. Agent descriptions, objectives, inputs, Rego text, Skills,
marketplace metadata, token roles and peer bodies are never instruction sources.
No credential, token reference, internal endpoint or secret path appears in the
response. The opaque HMAC revision binds effective state without exposing it.
No peer database is read and no Session/Workspace authority is inferred.

## Expiry and conditional requests

Responses occupy fixed UTC 30-second windows (`issued_at`, `expires_at`). The
remaining lifetime can be shorter than 30 seconds. Every response is at most
64 KiB; Markdown is below 32 KiB and the closed catalog has at most eight items.
A representation-specific strong ETag covers the entire body, including its
window. JSON and Markdown therefore have different ETags.

`If-None-Match` can return 304 **only after** repeating current identity, mapping,
policy, revocation and optional Run checks. Policy/configuration changes during
discovery fail closed and require another request. Expired representations never
receive 304: the next window changes the body and ETag. A 304 does not extend the
cached body's expiry. `X-AG-Capability-Revision` and `X-AG-Guidance-Expires` accompany
200/304, with `Cache-Control: private, no-cache` and `Vary: Authorization, Accept`.
Clients must not share cached guidance across origins, credentials or Run scopes.

Refresh at session setup, after admission/context changes, on expiry and after
stale-revision/conflict responses. Guidance is a projection, not durable authority;
callers must still invoke the governed API for every operation. A failed refresh
must not become permission to execute with expired instructions or bypass AG.
