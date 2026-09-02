# Load qualification

Run production-shaped tests from outside the cluster against a non-production
deployment with the intended policy bundle, dataset cardinality, payload sizes,
replica count, database tier, and zone topology. Measure the SLOs in
`slos.md`: API and policy percentiles, 200 run admissions/s sustained and 400/s
burst, allocation contention with no oversubscription, 5,000 SSE clients plus a
25% reconnect storm, 100 revocations/s fanout, and outbox recovery at twice the
peak committed rate.

The report must include commit and image digest, generator version/command,
environment, dataset/policy size, warm-up and duration, throughput, percentiles,
errors, saturation, resource use, DB pool/locks, OPA, cache, outbox and revocation
lag, bottleneck, and tuned limits. Any cross-tenant result, duplicate Run,
allocation expansion, evidence loss, stale authorization acceptance, or epoch
regression fails immediately regardless of throughput.

Use `go test -run '^$' -bench . ./test/operations` for the repository-local
deterministic baseline. It is regression evidence, not a substitute for the
production-shaped cluster qualification required before checking OPS-010.
