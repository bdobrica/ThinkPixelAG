# Run Lifecycle API Examples

The OpenAPI 3.1 document is the canonical API contract. These examples show the
implemented Phase 4 public workflow. Replace the example IDs, bearer token, and
base URL with values issued for your tenant. Tenant and principal identity come
only from the verified bearer token; no request field or forwarding header can
select a tenant.

## Admit and inspect a run

Every mutation requires a unique 16–128 character `Idempotency-Key`. Retrying
the same normalized request in the same tenant, principal, and route scope
returns the stored response. Reusing the key for different content returns
`409 idempotency_key_reused`.

```sh
curl --fail-with-body https://governance.example/v1/agents/0198d720-7c2a-7000-8000-000000000001/runs \
  --request POST \
  --header 'Authorization: Bearer <access-token>' \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: admit-0198d720-7c2a-7000-000000000001' \
  --data '{
    "objective": "Summarize the approved incident record",
    "input": {"record_ref": "incident-42"},
    "constraints": {
      "max_execution_time_seconds": 300,
      "max_llm_tokens": 12000,
      "max_tool_calls": 20
    }
  }'
```

A successful response is `201 Created`, includes a canonical `Location`
header, and returns the admitted projection. The objective and input influence
idempotency only; they are not copied into governance state, policy evidence,
audit records, outbox messages, or logs.

```json
{
  "id": "0198d721-0170-7000-8000-000000000002",
  "agent_id": "0198d720-7c2a-7000-8000-000000000001",
  "version_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "state": "ADMITTED",
  "state_version": 1,
  "envelope": {"version": 1, "resources": []},
  "created_at": "2026-08-22T10:00:00Z",
  "updated_at": "2026-08-22T10:00:00Z"
}
```

Fetch the current authorized projection with:

```sh
curl --fail-with-body \
  --header 'Authorization: Bearer <access-token>' \
  https://governance.example/v1/runs/0198d721-0170-7000-8000-000000000002
```

Absent, malformed, cross-tenant, and policy-hidden run identifiers all produce
the same enumeration-safe `404` contract.

## Send a durable signal

Signals are commands and do not themselves change lifecycle state. `PAUSE`
accepts an optional `reason_code`, `RESUME` requires an empty payload, and
`CUSTOM` requires a `name` and bounded object `data`. `CANCEL` is intentionally
not a signal type.

```sh
curl --fail-with-body https://governance.example/v1/runs/0198d721-0170-7000-8000-000000000002/signals \
  --request POST \
  --header 'Authorization: Bearer <access-token>' \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: signal-0198d721-0170-7000-8000-000000000002-01' \
  --data '{
    "type": "CUSTOM",
    "payload": {"name": "runtime.refresh", "data": {"scope": "tools"}},
    "expected_state_version": 1
  }'
```

The `202 Accepted` response is the ordered `run.signal.accepted` event. The
signal, event, audit record, and outbox message commit atomically.

## Stream and resume events

```sh
curl --no-buffer --fail-with-body \
  --header 'Authorization: Bearer <access-token>' \
  https://governance.example/v1/runs/0198d721-0170-7000-8000-000000000002/events
```

Each event contains a run-bound authenticated cursor in `id`, its event type,
and sanitized `RunEvent` JSON:

```text
id: <opaque-run-bound-cursor>
event: run.admitted
data: {"id":"0198d721-0170-7000-8000-000000000003","run_id":"0198d721-0170-7000-8000-000000000002","sequence":1,"type":"run.admitted","occurred_at":"2026-08-22T10:00:00Z"}

```

Persist the last fully processed `id` and reconnect strictly after it:

```sh
curl --no-buffer --fail-with-body \
  --header 'Authorization: Bearer <access-token>' \
  --header 'Last-Event-ID: <opaque-run-bound-cursor>' \
  https://governance.example/v1/runs/0198d721-0170-7000-8000-000000000002/events
```

The `after` query parameter is equivalent. When both forms are supplied they
must match. Invalid or cross-run cursors return `400`; a cursor outside the
10,000-event logical retention window returns `410`, after which the client
must refetch the authoritative run projection. Authorization is evaluated on
every connection, and comment heartbeats may appear while the stream is idle.

## Cancel a run

Cancellation has its own `runs.cancel` authorization and serializes with worker
completion, failure, and timeout. If another terminal transition wins first,
the response returns that established terminal projection without overwriting
it or emitting duplicate terminal evidence.

```sh
curl --fail-with-body https://governance.example/v1/runs/0198d721-0170-7000-8000-000000000002/cancel \
  --request POST \
  --header 'Authorization: Bearer <access-token>' \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: cancel-0198d721-0170-7000-8000-000000000002' \
  --data '{"reason_code":"caller.request","expected_state_version":2}'
```

The `200 OK` response is the stable terminal run projection. Exact retries
return the original response. A stale version on a still nonterminal run
returns `409`; an already established terminal result is replayed unchanged.
