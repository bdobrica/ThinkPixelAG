# Data Classification and Redaction

## Classes

| Class | Meaning | Examples | Storage/telemetry rule |
|---|---|---|---|
| Public | intentionally publishable | API paths, product docs, public reason-code definitions | may appear in all outputs |
| Internal | non-sensitive operational metadata | opaque run ID, agent ID, state, service version, aggregate latency | permitted in access-controlled logs/metrics with retention limits |
| Confidential | tenant/business or security-relevant data | owner, repository reference, policy metadata, tool/skill declarations, detailed decision reasons | encrypt in transit/at rest; logs only when allowlisted/minimized; never metric labels |
| Restricted | credentials, raw inputs, sensitive policy/context, key material | authorization/delegation tokens, objectives and input payloads, JWT claims beyond allowlist, policy input, database/KMS secrets, private keys | never log/trace/metric/audit payload; store only when explicitly required, encrypted and access controlled; private keys never enter service storage |

Data receives the highest applicable class. Tenant-provided content defaults to Restricted until explicitly classified lower.

## Field rules

| Data | Authoritative storage | Logs | Traces | Metrics | Audit/evidence |
|---|---|---|---|---|---|
| Authorization/JWT/delegation token | none after verification | redact entirely | never attach | never | token hash/key ID only if required |
| Principal/tenant ID | PostgreSQL where referenced | opaque ID allowed | opaque ID allowed if sampling policy permits | never as label | opaque ID required for attribution |
| Objective and arbitrary run input | PostgreSQL only if product contract requires it; otherwise external reference/digest | never | never | never | schema/version, size, and digest only |
| Agent/version/tool/skill identifiers | PostgreSQL | opaque identifier allowed | allowed | bounded aggregate labels only | allowed |
| Rego source/bundle | controlled artifact storage | digest/version only | digest/version only | version only | digest/version/signature result |
| OPA input | ephemeral | never raw | never raw | never | action/resource/policy IDs, allow/deny, reason codes, hashes |
| OPA output | decision metadata where required | allow/deny and bounded reason code | same | aggregate outcome/reason family | typed decision metadata and hashes |
| Resource values | PostgreSQL ledger | bounded numeric summary with tenant omitted | optional bounded attributes | aggregates, never tenant/run labels | change, actor, reason, units |
| Revocation reason | PostgreSQL | reason code, not free text | reason code | aggregate scope/outcome | full controlled reason plus references |
| Database/Valkey/KMS credentials | secret provider only | redact | never | never | credential/key identifier and lifecycle event only |
| Error/panic | ephemeral | sanitized type/request ID; stack only protected sink | sanitized | bounded code | security-relevant code and request/event ID |

## Enforcement

- HTTP middleware MUST NOT log headers or bodies by default. `Authorization`, cookies, proxy credentials, and configured secret headers are always replaced with `[REDACTED]`.
- Domain types containing Restricted fields implement no unrestricted string serialization; logs use explicit safe projections.
- OpenTelemetry span attributes and Prometheus labels use an allowlist. High-cardinality run, tenant, principal, objective, and input values are prohibited as metric labels.
- Policy decision records store policy/digest/epoch identifiers, action, resource type/opaque ID, result, reason codes, latency, input hash, and decision ID—not raw policy input.
- SQL errors exposed to clients are mapped to stable problem codes without query text, constraint internals, or values.
- Evidence events minimize content while preserving attribution, action, target, before/after digests, approval references, result, and integrity metadata.
- Development mode follows the same redaction rules. Test fixtures use synthetic credentials and tenant content.
- Debug logging cannot disable redaction. Temporary diagnostic fields require code review, expiry, and a security-approved sink.

## Retention and deletion

Retention is deployment policy. Idempotency bodies and operational logs use the shortest practical TTL. Append-only resource, revocation, policy, and evidence records follow compliance retention and tenant-deletion/legal-hold requirements. Deleting tenant content must not silently destroy security evidence; evidence uses minimized identifiers or irreversible correlation hashes where regulations require separation.

## Verification

Automated tests capture logs, spans, metrics, errors, audit events, and panic recovery while injecting sentinel secrets and payloads. A test fails if any sentinel appears outside its explicitly authorized encrypted store.

The trace facade drops caller-supplied start attributes, exception text, and
arbitrary event attributes; only stable bounded operation names and a closed
attribute allowlist reach exporters. Structured log `error` values are always
rendered as `[REDACTED]`, regardless of their concrete type. Audit metadata and
outbox headers recursively reject restricted field names before a transaction
starts, and policy input rejects restricted payload/credential fields before
evaluation or decision caching. Idempotency and reconciliation keys do not
enter audit metadata.
