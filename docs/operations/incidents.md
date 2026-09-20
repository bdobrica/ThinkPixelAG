# Handle an incident

Identify the affected traffic class and scope using safe metrics and structured
logs. Preserve fail-closed behavior, stable replay identities and evidence.
Record the deployed digest, schema version and dependency health; keep secrets
and runtime payloads out of tickets.

| Symptom | Procedure |
|---|---|
| Database unavailable or saturated | [Database outage](runbooks.md#service-or-database-outage); inspect global pool budget and storage before increasing replicas |
| Protected requests denied / policy unavailable | [OPA outage](runbooks.md#opa-or-valkey-outage) or [policy rollback](runbooks.md#policy-rollback); never bypass OPA |
| Stale revocation or cursor gap | [Reconciliation](runbooks.md#revocation-gap-or-staleness); never skip sequence numbers or lower epochs |
| Evidence not draining | [Outbox backlog](runbooks.md#outbox-backlog); preserve events and stable delivery IDs |
| Availability budget burning | [Burn-rate response](runbooks.md#availability-error-budget-burn); stop unrelated rollouts and use compatible rollback |
| High latency or dropped load | [Capacity response](runbooks.md#latency-or-capacity) and [measured envelope](capacity.md) |
| Compromised/expiring signing key | [Key rotation](runbooks.md#key-rotation); use approved managed signing procedures |
| Privileged recovery is necessary | [Break glass](runbooks.md#break-glass); only supported scope, fresh MFA and four-eyes approval |

After recovery, confirm readiness, admission/replay/denial, monotonic epochs,
resource conservation and evidence chain/checkpoint continuity before restoring
normal load. If storage endpoints changed, fence old writers first. A retained
old database or sink must not restart behind an authoritative Service.
