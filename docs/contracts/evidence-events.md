# Privileged Evidence Event Contract

## Boundary and versioning

Privileged security evidence uses `thinkpixelag.evidence/v1`. The normative
machine-readable schema is
[`api/schemas/evidence-event-v1.json`](../../api/schemas/evidence-event-v1.json),
and the closed Go validation boundary is `internal/evidence`. SEC-005 defines
event content; SEC-006 owns authenticated delivery, replay, sink receipts,
checkpoints, and inter-event integrity links.

Every event has a stable UUID, closed event type, optional tenant scope,
attributed principal/workload/system actor, action, outcome, at least one
controlled reason code, UTC occurrence time, and type-specific data. Request,
trace, and policy-decision identifiers are optional correlation fields. Unknown
versions, event types, fields, enum values, invalid digests, and unbounded
references fail closed.

## Closed event catalog

| Type | Required security binding |
|---|---|
| `POLICY` | artifact class/version/revision and digest, managed key/version/algorithm, prior digest and approval when applicable |
| `REVOCATION` | immutable revocation ID, create/lift/expiry, scope/target, resulting epoch vector, approval when applicable |
| `RESOURCE_OVERRIDE` | signed override class/version/revision and digest, managed key metadata, prior digest and approval |
| `APPROVAL` | approval/action/target, request digest, requester, independent approver when decided, state and expiry |
| `KEY_LIFECYCLE` | opaque managed key/version, algorithm/protection, lifecycle change, prior version and approval when applicable |
| `VERSION_APPROVAL` | approval, agent/version, immutable content digest, lifecycle decision and bounded approval reference |
| `BREAK_GLASS` | session, bounded scope, grant digest, four-eyes approval, explicit expiry and lifecycle change |

The schemas carry digests and opaque references, never artifact payloads,
provider assertions, comments, credentials, raw policy input, private keys, or
break-glass tokens. Producer transactions MUST append authoritative mutation,
local audit evidence, and the exportable event atomically. Global events omit
`tenant_id`; omission does not weaken actor attribution.

## Compatibility

Consumers MUST reject unsupported major contract versions and unknown event
types. Additive optional fields require a new schema revision because v1 uses
closed objects. Changing meaning, required fields, or enums requires a new
contract version and dual-read/dual-write migration plan. Exporters preserve
the original event ID as the sink deduplication key and MUST NOT reinterpret or
silently discard invalid events.
