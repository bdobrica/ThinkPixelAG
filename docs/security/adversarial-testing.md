# Adversarial Security Testing

`make test-security` is the stable acceptance gate for attacks that cross the
authorization, policy, cache, persistence, and evidence boundaries. It runs
deterministic Go and Rego tests, then uses real PostgreSQL for controls whose
proof depends on transaction, lease, uniqueness, or append-only behavior.

| Attack class | Required proof |
|---|---|
| Privilege escalation | Tenant and role substitution, umbrella roles, human/workload confusion, bearer identity on workload routes, and cross-role access are denied. |
| Approval bypass | Self-approval, expired or digest-substituted approval, unverifiable provider assertions, concurrent reuse, and mutation of recorded approval evidence fail closed. |
| Replay | Idempotency reuse with different content conflicts; approval consumption is single-use; stale leases cannot acknowledge delivery; retry preserves the stable event identity. |
| Cache poisoning | Malformed or unavailable cache entries are ignored, every authorization dimension and epoch is key-bound, invalidation advances reachability, and authoritative operations bypass caches. |
| Policy tampering | A changed payload, digest, artifact kind/version/revision, key version, algorithm, signature, malformed OPA result, or unsupported reason code is rejected. |
| Stale authorization | Gaps, lag, unhealthy clocks, expired freshness budgets, revocation/policy epoch changes, and high-risk non-authoritative evaluation deny without reviving an old ALLOW. |
| Evidence suppression | Business state, audit evidence, and outbox records commit atomically; rollback suppresses all three; publisher crash replay, fencing, immutable approval evidence, receipt binding, and monotonic checkpoints prevent selective loss or replacement. |

The gate intentionally overlaps the broader unit, policy, integration, and
end-to-end suites. Keeping it as a named target makes the release-critical
attack set independently runnable and prevents a broad suite reorganization
from silently removing the security acceptance boundary.

For a local PostgreSQL instance on a non-default port, run:

```sh
make test-security TEST_DATABASE_URL='postgresql://USER:PASSWORD@127.0.0.1:PORT/DATABASE?sslmode=disable'
```

Use only disposable local test credentials. No production identity, policy,
evidence sink, or database participates in this gate.
