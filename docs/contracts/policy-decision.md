# OPA Policy Decision Contract

## Versioning and entry point

The RC contract version is `thinkpixelag.authorization/v1alpha1`; it remains experimental until Phase 3 freezes it. The Rego entry point is `data.thinkpixelag.authorization.decision`. Unsupported versions, missing fields, unknown output fields where strict decoding applies, or invalid types produce a deny/error outcome.

## Input

```json
{
  "contract_version": "thinkpixelag.authorization/v1alpha1",
  "decision_id": "opaque-uuid",
  "request_time": "2026-08-10T12:00:00Z",
  "subject": {
    "principal_id": "principal-opaque",
    "tenant_id": "tenant-opaque",
    "principal_type": "human|workload|gateway",
    "roles": ["agent-invoker"],
    "authentication_methods": ["oidc"],
    "issuer": "configured-issuer-id"
  },
  "action": "runs.create",
  "resource": {
    "type": "agent",
    "id": "production-incident-investigator",
    "tenant_id": "tenant-opaque",
    "attributes": {}
  },
  "agent": {
    "id": "production-incident-investigator",
    "version_digest": "sha256:...",
    "risk_class": "medium",
    "owner": "sre-platform",
    "approved": true
  },
  "requested_constraints": {},
  "authority_constraints": {},
  "security_state": {
    "global_epoch": 12,
    "tenant_policy_epoch": 7,
    "agent_revocation_epoch": 3,
    "age_seconds": 2,
    "has_gap": false,
    "authoritative": true
  },
  "context": {
    "request_id": "opaque-uuid",
    "source_workload": "optional-bounded-id"
  }
}
```

The service constructs input exclusively from verified identity, normalized request fields, authoritative records, and bounded deployment context. Roles are advisory claims mapped through trusted configuration, not raw token values blindly accepted. Raw access tokens, objectives, arbitrary run inputs, secrets, and unrestricted headers are prohibited.

Each action defines a JSON Schema-compatible resource and constraint shape. Unknown action/resource combinations deny. Arrays are de-duplicated and deterministically sorted before hashing; strings and maps have explicit size/depth/count limits.

## Output

```json
{
  "contract_version": "thinkpixelag.authorization/v1alpha1",
  "decision_id": "opaque-uuid",
  "allow": true,
  "reason_codes": ["agent.invoke.allowed"],
  "resolved_constraints": {},
  "obligations": [],
  "decision_ttl_seconds": 10
}
```

- `decision_id` must equal input.
- `allow` is mandatory; absence is denial.
- `reason_codes` contains one or more registered, stable, non-sensitive codes and is bounded in count/length.
- `resolved_constraints` is mandatory for admission/allocation and must be equal to or stricter than every `authority_constraints` ceiling. The Go domain validates non-expansion independently of Rego.
- `obligations` is a bounded list of typed obligations understood by the service. Unknown mandatory obligations deny; the service never silently ignores them.
- `decision_ttl_seconds` is nonnegative and capped by deployment, identity, policy, and revocation freshness. Zero disables ALLOW caching and is mandatory for high-risk writes.

## Initial actions

| Action | Resource | Risk baseline |
|---|---|---|
| `agents.list`, `agents.describe` | agent/catalog | low/sensitive read by returned fields |
| `runs.create` | agent/version | normal or high-risk write by agent risk |
| `runs.read`, `runs.events.read` | run | sensitive read |
| `runs.signal`, `runs.cancel` | run | normal write |
| `runs.claim`, `runs.heartbeat`, `runs.complete` | run lease | normal/high-risk workload write |
| `resources.meter`, `resources.settle` | run/reservation | high-risk write |
| `resources.extend` | envelope | high-impact administrative write |
| `agents.manage`, `versions.approve` | registry/version | high-impact administrative write |
| `policies.manage`, `policies.activate` | policy | high-impact administrative write |
| `revocations.manage`, `revocations.reconcile` | revocation/gateway | high-impact write or trusted gateway read |

## Reason-code registry

Codes use `domain.subject.outcome` and reveal no hidden tenant/resource existence. Initial families are:

- allow: `agent.discover.allowed`, `agent.invoke.allowed`, `run.access.allowed`, `workload.operation.allowed`, `governance.operation.allowed`;
- deny: `identity.invalid`, `tenant.mismatch`, `resource.not_visible`, `agent.not_approved`, `agent.revoked`, `principal.revoked`, `policy.revoked`, `security_state.stale`, `security_state.gap`, `constraint.expands_authority`, `approval.required`, `action.not_permitted`, `contract.invalid`;
- service errors exposed externally map to a generic policy-unavailable problem; sensitive internal reason detail remains in protected telemetry/evidence.

Adding/removing/changing semantics of a code requires contract review. Clients must make control decisions from HTTP status/schema, not parse human text.

## Constraint narrowing

For numeric maxima and deadlines, resolved output must be no greater/later than authoritative ceiling. For allowlists it must be a subset. For delegation depth it must be no greater than parent remaining depth and approved version ceiling. Omitted policy output cannot widen a value; the Go resolver takes the intersection/minimum and rejects incompatible units or unknown dimensions.

## Failure matrix

| Condition | Result |
|---|---|
| explicit valid `allow: false` | deny with registered safe reason |
| OPA timeout/unavailable | deny protected operation; emit metric/sanitized evidence |
| no active/fresh verified bundle | deny; readiness false for protected traffic |
| malformed input generated by service | internal contract error and deny |
| malformed/mismatched output | deny and security/operational alert |
| stale/gapped revocation state | deny when action freshness contract is unmet |
| constraint expansion | deny even if policy says allow |
| unknown obligation/action/resource/dimension | deny |

## Decision record

Persist or export decision ID, time, principal/tenant opaque IDs, action, resource type/opaque ID, policy bundle digest/version, applicable epochs/freshness, allow/deny, reason codes, resolved-constraint digest, input digest, duration, cache status, and error category. Do not persist raw input, tokens, objectives, arbitrary request context, or secrets.

## Required policy tests

Golden tests cover every action's positive and negative path, tenant isolation, invisible resource, principal/agent/version/skill/tool/policy/global revocation, every freshness class, risk escalation, approval requirement, caller constraint narrowing, parent non-expansion, missing/unknown/malformed fields, output ID mismatch, unknown obligation, timeout, stale bundle, and deterministic decisions for normalized equivalent input.
