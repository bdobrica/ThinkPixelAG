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

## External HTTP driver and isolated fixture

`test/operations/fixture` is a test-only executable, excluded from the production
image. `prepare DIRECTORY` creates private synthetic identities for two tenants,
5000 distinct workload certificates, an OIDC discovery/JWKS document, and
23-hour caller tokens. Set `OPS_ISSUER` to the fixture HTTPS URL first. Preparation
refuses to overwrite an existing identity document. Retain this directory
securely; do not commit it or print its tokens/keys. A fresh preparation is
needed when credentials expire.

After applying migrations, `seed DIRECTORY` uses `OPS_DATABASE_URL` to provision
synthetic principals, an approved agent version, resource dimensions, and the
repository policy. It requires the pinned `opa` executable and verifies the
bundle using an explicitly test-only software signing authority. This is not a
production policy promotion path. Run seeding only in an isolated test database.
`serve DIRECTORY` serves HTTPS on port 8443 using `server.crt`/`server.key` and
an authenticated evidence sink. Supply `OPS_SINK_TOKEN` through secret delivery
and `OPS_RECEIPTS_FILE` on persistent storage. The sink fsyncs deliveries and
receipts before acknowledging them and checks replay hashes and chain sequence.

Deploy the governed runtime described in [configuration](../configuration.md),
with the fixture issuer/CA and the generated workload bindings. Preserve PVCs,
identities and deployment resources between scenarios; there is no automatic
cluster teardown in either executable.

```sh
make test-load LOAD_ARGS='-base https://api.example.test -identities /private/ops-identities -mode smoke'
make test-load LOAD_ARGS='-base https://api.example.test -identities /private/ops-identities -mode read -rate 1000 -workers 128 -duration 1m'
make test-load LOAD_ARGS='-base https://api.example.test -trusted https://trusted.example.test -identities /private/ops-identities -mode streams -clients 5000 -rate 100 -workers 64 -duration 1m'
```

For the fixture trusted listener the TLS server name is `ops-api`; supply a
certificate covering that name. Modes also include `admission`, `signal`, and
`revocation`. Every invocation first checks real admission, exact idempotent
replay, own-tenant read, foreign-tenant denial, and forged-token denial. The
bounded open-loop driver reports offered, completed, successful and dropped
requests separately; overload is not hidden by a closed-loop request rate.
Latency includes transport, so obtain process-side percentiles from Prometheus
as well. Reports contain aggregates only. Exit status is nonzero for request
failures/drops or detected invariants. Zero is not full OPS-010 certification.

Streams use distinct mTLS principals, track monotonically increasing sequences
and epochs, and reconnect 25% midway through the measurement. Initial connection
attempts are bounded to 32 at once. Reported lag includes historical replay and
is bucketed in 10 ms intervals (the final bucket means at least 60 seconds).
Stream gaps require authoritative reconciliation; this driver counts them and
ends the affected stream. It does not implement a complete gateway reconciler
or prove delivery of every committed event to every client. Missing peak connections, incomplete reconnect counts, missing controlled
fanout deliveries and gaps fail the run;
review these counts alongside request outcomes. Repeated stream runs
replay retained history, so distinguish catch-up from live propagation.

The current driver is an initial diagnostic harness: it does not yet automate
trusted-usage load, contested child allocations, global-change fanout, lifecycle
completion/settlement, or a 15-minute outbox backlog. Real database integration
tests cover allocation invariants but do not substitute for those capacity
scenarios. Keep OPS-010 open until the full matrix and intended topology pass.

## Hardware-limited rehearsal

Keep production targets unchanged when testing a smaller deployment. Record a
separate observed operating envelope, with all offered requests, failures,
scheduler/concurrency drops, duration, percentiles and backlog behavior. The
`-rate` option accepts fractional rates (minimum 0.01/s); for example,
`-mode admission -rate 0.05 -workers 1 -duration 5m` schedules one admission
every 20 seconds. A request's client timeout is not increased to hide a stall.

For live fanout, reconcile the gateway first and pass the returned sequence as
`-after-sequence`. This avoids treating retained historical replay as current
propagation lag. Run without another revocation writer and inspect
`CompleteStreams`, `StreamEvents`, `StreamGaps`, and `TransportErrors` alongside
latency. Each receiver must observe the successful mutation count; a 30-second
drain window follows offered traffic. Connection setup time is reported
separately. This controlled measurement does not implement durable gateway
state or replace partition/reconciliation checks.

See [homelab qualification](../../evidence/README.md#capacity) for hardware-limited
measurements and recovery evidence; the production objectives remain in
[slos.md](../slos.md).
