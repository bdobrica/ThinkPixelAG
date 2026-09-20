# Retained-cluster resilience qualification

OPS-011 separates three kinds of evidence: deterministic boundary tests,
real-adapter fault probes, and faults through the deployed application. A
passing component probe does not prove that a disabled component is wired into
the production executable. Keep remaining composition gates explicit.

`make test-resilience` runs OPA/cache/freshness/worker/evidence boundary tests
and the promotion orchestrator's safety regressions. It needs Go and Python 3;
the Python harness uses only the standard library. `make verify` remains the
aggregate repository gate. The operational commands below are opt-in and are
not run by `make verify`.

## Existing isolated fixture

Use the governed runtime and private identities from [load testing](load-testing.md).
The retained-cluster runner requires SSH access to a controller with passwordless
`sudo kubectl`, a disposable fixture namespace, and PostgreSQL administrative
access inside its Pod. Its defaults match the OPS-010 fixture. Public API
requests authenticate with the private `caller.token`; no credentials or raw
runtime bodies are printed in reports. Use a private state directory outside
source control. Run one fault scenario at a time and keep other writers and
operators out of this fixture during promotion.

Unlike the older Kind cluster script, this runner does not uninstall resources
or delete PVCs. Temporary scaling and pod eviction are deliberate faults;
Deployments, Services, Secrets, configuration and storage remain reusable.
The old Kind script still owns and tears down its own disposable cluster and
must not be pointed at a retained cluster.

```sh
make test-retained-resilience RESILIENCE_ARGS='--ssh-host operator@controller --namespace isolated-ops --base http://test-api.example:30080 --identities /private/ops-identities --state-dir /private/ops011 --report /private/prepare.json --scenario prepare --standby-node worker-2 --pod-cidr 10.42.0.0/16 --allow-fault-injection'
```

Preparation reuses resources if present. It provisions a 4 GiB PostgreSQL 18
standby on the selected worker, a dedicated replication role/Secret, a bounded
physical replication slot, an authenticated disposable Valkey instance and a
separate OPA probe instance using the fixture's existing policy/image. It sets
`wal_keep_size` and `max_slot_wal_keep_size` to 256 MB for this small fixture;
it does not change `fsync` or commit durability. It adds a disruption budget
protecting all but one API replica, the production manifest's five-second API
preStop drain, and a ten-second drain for the fixture's OPA sidecar.

An existing nonempty standby data directory is never erased or silently cloned
over. Failed/incomplete backups require operator inspection. Preparation refuses
to proceed after the private promotion-attempt marker exists. Do not delete
that marker merely to rerun setup against a topology that has changed roles.

Replace `--scenario prepare` with:

| Scenario | Checks and restoration |
| --- | --- |
| `runtime` | OPA unreachable, malformed decision output, restoration, actual `policy/v1` Pod eviction, rolling restart |
| `disruptions` | Eviction and rolling restart only, useful after correcting fixture draining |
| `latency` | PostgreSQL five-second pre-auth delay, closed old connections, protected requests fail closed, original zero delay restored |
| `promote` | Quiesced standby promotion and application reconnection; topology changes persist |

OPA configuration changes wait for both replacement readiness and termination
of every previously serving API Pod before fault assertions. Deployment rollout
completion alone can overlap an old healthy Pod's drain period.

Use a separate aggregate `--report` path for each scenario. HTTP status zero
means transport failure, not HTTP success. OPA fault assertions require at
least one explicit 503 and no successful protected response; transport failures
remain separately counted. Rolling-restart availability requires every sampled
read to return 200. A passing short sample is not a continuous-availability SLA.

## Promotion safety and limits

The promotion drill validates that the writer Service selects the declared old
primary and that the standby is recovering on a different worker. It stops API
replicas, captures table-content hashes and the primary's flushed WAL position,
and waits for standby replay through that position. It compares all 41 declared
authoritative tables, scales the old primary to zero, and waits for its Pod to
be deleted before calling `pg_promote`. It then verifies recovery has ended,
updates the writer Service, compares the same tables again, runs restored-state
invariants, and reconnects the API. An existing Run's complete read response
must match before and after the transition.

This is **quiesced, operator-driven promotion**, not automatic HA failover or
proof of zero loss during an unplanned asynchronous-primary failure. Its
interruption time includes the deliberately paused API and invariant checks.
Streaming replication is asynchronous here; a captured replay barrier proves
preservation only for that barrier. See PostgreSQL's [standby documentation](https://www.postgresql.org/docs/18/warm-standby.html)
and [base-backup procedure](https://www.postgresql.org/docs/18/app-pgbasebackup.html).

A durable private marker is written before attempting promotion. If its response
is uncertain, the runner leaves the old primary fenced and the API stopped.
Inspect `pg_is_in_recovery`, Pod state and the Service selector before resuming;
never automatically restart the old writer. After success, the Service routes
to the promoted Deployment and the original Deployment remains at zero with
its PVC retained. Rejoining that old volume requires an explicit
[rewind/reinitialization procedure](https://www.postgresql.org/docs/18/app-pgrewind.html),
not simply scaling it up. For later drills, pass the **current** writer's name
as `--primary`; the generic names do not automatically discover or elect a leader.

## Real-adapter component probes

Build `go build -o /private/resilienceprobe ./test/operations/resilienceprobe`.
Supply secrets through the environment, never command arguments or evidence:

- `OPS_DATABASE_URL`, `OPS_TENANT`, `OPS_WORKER` for a synthetic tenant and
  operator-selected workload principal with an available Run;
- `OPS_VALKEY_URL`, `OPS_CACHE_KEY`, `OPS_OPA_URL` for the retained probe services;
- `OPS_TRUSTED_URL` for the real mTLS revocation listener.

Use private SSH tunnels to the ClusterIP services when running outside the
cluster. No production database, real tenant or concurrently consumed Run queue
is suitable for these probes.

| Probe invocation | Evidence |
| --- | --- |
| `resilienceprobe worker /private/new-lease.json` | Child process claims a real Run, is killed, lease expires, successor claims the same Run with a higher fence, stale heartbeat/mutation fail, new heartbeat succeeds |
| `resilienceprobe cache-healthy /private/unused` | Real OPA decision is cached in real authenticated Valkey; a new evaluator proves a remote hit; authoritative requests bypass cached ALLOW |
| `resilienceprobe cache-down /private/unused` | Run with Valkey scaled to zero; decisions fall back to real OPA, with cache misses and authoritative bypass; restore Valkey afterward and wait for client-side reachability before rechecking |
| `resilienceprobe partition /private/ops-identities` | Real mTLS stream connection is disconnected for 31 seconds; the existing normal-write freshness boundary denies, then authoritative reconciliation restores service without epoch/sequence regression |

The worker probe retains a 0600 lease checkpoint and append-only database
records. It does not execute an AR harness. The partition probe is an ephemeral
test consumer, not a durable production gateway. The cache probe's active-policy
metadata is synthetic; the actual OPA process runs the repository fixture policy.
These probes do not qualify production worker/cache composition, signed policy
promotion, or a gateway's durable apply boundary.

See [OPS-011 results](../../evidence/README.md#recovery) for the hardware-specific outcomes and
remaining qualification gates. Production targets are unchanged.
