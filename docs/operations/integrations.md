# Connect a harness or application to AG

The [platform composition](../architecture/platform-composition.md) places
clients, IDEs and automation at AG's boundary. **AG is the harness's platform
entry point.** AR owns runtime/session execution behind the platform integration;
the harness is not expected to configure a direct AR connection.

That is the target architecture. The current RC implements a useful governance
API, but it does not yet implement the entire harness-facing platform workflow.
The distinction matters when deciding what can be used today.

## What the current executable provides

| Operation | Current state |
|---|---|
| Authenticate a caller, discover approved agents | Implemented: OIDC bearer authentication and agent discovery |
| Admit a Run, return identity/version/resource envelope | Implemented: `POST /v1/agents/{agent_id}/runs` |
| Read, signal, cancel and stream Run events | Implemented under `/v1/runs/{run_id}` |
| Trusted usage, settlement and revocation distribution | Implemented through the separate mTLS listener and versioned contracts |
| Register/provision arbitrary tenants, agents and policies through an operator-facing installer/API | Implemented in the source local profile: [protected bootstrap](bootstrap.md) and authenticated registry/policy APIs |
| Attach an existing harness and drive execution/completion through a complete AG-facing adapter | Not yet implemented as a complete integration; internal lease services are not a published worker API |
| Obtain dynamic platform capabilities/instructions for a harness | Implemented in source: [v1 discovery](../contracts/harness-guidance.md) and [installed helper](../../integrations/harness/README.md) |

An `ADMITTED` Run is not proof that a harness was launched, constrained, metered
or completed its objective. The examples intentionally show the working
admission/read/cancel path rather than claim that the full platform is connected.

## Use the current HTTP boundary

1. Configure the AG URL and obtain a caller token from the deployment's issuer.
   Keep credentials in the integration host's secret storage, not in prompts or
   generated instructions. Apply [issuer/audience/claim/role mappings](../security/authentication.md).
2. Discover an approved agent with `GET /v1/agents`, then request admission using
   a stable idempotency key. Retain the returned Run ID, resolved version and
   resource envelope. Retry an uncertain request with its original key/body.
3. Read the current projection and subscribe to Run events as needed. Preserve
   event cursors and handle gap/reconciliation responses according to the contract.
4. Use the separate cancellation endpoint and current state version to request
   cancellation. Do not invent an execution-completion call that this RC does
   not publish.

The [quick start](../quickstart.md) supplies real commands and credentials for
an installed service. [Run examples](../api/run-lifecycle.md) cover signals and
streams. The [OpenAPI](../../api/openapi/thinkpixelag.yaml) defines the wire API.

## Harness instructions and enforcement

Install the [trusted helper and appendable AGENTS.md snippet](../../integrations/harness/README.md).
It retrieves caller/Run-scoped guidance from AG, keeps bearer credentials in host
storage, revalidates online before each operation and refreshes after admission
or context changes. Existing project instructions remain intact. The source
candidate supports a real discovery/admission/read/cancel workflow; the published
`0.1.0-rc.1` image predates this helper/API.

Capability descriptions do not grant authority. A trusted integration must
handle authentication, refresh, Run context and service/gateway enforcement.
Model compliance with instructions is not a security boundary. Do not put AG
administrator credentials, signing keys or long-lived provider secrets in harness
instructions. Dynamic guidance does not move AR execution, TG side effects,
LLMGW credentials or other component responsibilities into AG.

## Trusted service integrations

Enable the additional runtime mTLS settings and configure [URI-SAN bindings](../contracts/workload-identity.md)
for trusted callers. Usage, settlement and revocation handlers use these verified
bindings, not bearer tokens or forwarded headers. Consumers persist checkpoints,
reconcile gaps and enforce operation-specific freshness. Bindings load at startup.

OPA implements the [decision contract](../contracts/policy-decision.md). Independent
evidence receivers implement [delivery and receipt semantics](../contracts/evidence-events.md).
Peer components use versioned contracts and stable IDs; they never access AG's
database or private Go types. Production and unavailable cross-component
qualification remains in [ADR-0012](../adr/0012-integration-rc-qualification-deferrals.md).
