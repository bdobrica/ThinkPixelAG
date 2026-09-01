# Privileged Evidence Event Contract

## Boundary and versioning

Privileged security evidence uses `thinkpixelag.evidence/v1`. The normative
machine-readable schema is
[`api/schemas/evidence-event-v1.json`](../../api/schemas/evidence-event-v1.json),
and the closed Go validation boundary is `internal/evidence`. SEC-005 defines
event content; SEC-006 owns authenticated delivery, replay, sink receipts,
checkpoints, and inter-event integrity links.

## Authenticated export and integrity

SEC-006 uses the closed `thinkpixelag.evidence-delivery/v1` envelope defined by
[`api/schemas/evidence-delivery-v1.json`](../../api/schemas/evidence-delivery-v1.json).
Each separately configured sink has a stable identifier and its own ordered
checkpoint. A delivery binds the stable outbox/event ID, sink ID, sequence,
prior event hash, exact minimized JSON event, and a deterministic SHA-256 hash.
Sequence one omits the prior hash; every later delivery MUST name the preceding
committed event hash.

The HTTPS sink authenticates the exporter independently of API callers. The
event ID is also the idempotency key. A successful response is not durable
completion until AG validates that the receipt repeats the sink, sequence,
event ID, and event hash, and atomically appends it while advancing the local
checkpoint. Receipts are append-only. A crash after remote acceptance but
before local commit reclaims the same event after lease expiry and recreates
the same delivery hash, allowing sink-side deduplication. Mismatched, unknown,
oversized, non-2xx, or malformed receipts fail closed and do not advance the
checkpoint.

Sink endpoint and credentials are deployment configuration, not governance
authority. Production deployments MUST use HTTPS and an independently
administered credential; bearer credentials are excluded from event bodies,
receipts, logs, and safe configuration rendering. Workload identity/mTLS hooks
remain SEC-008. Sink loss retains the transactional outbox and stops checkpoint
progress; it does not roll back the already committed governance mutation.

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
