# Homelab operating envelope and recovery

Outcome: 100 reads/s passed a five-minute confirmation, but admission retry
correctness failed. Hardware-limited capacity and application correctness are
reported separately; no overall correctness sign-off is given.

This is a separate hardware-limited qualification of the retained ARM64 test
cluster. It does not change the production objectives in [slos.md](slos.md) or
close production capacity qualification in OPS-010. The operator identified
WiFi networking and USB flash storage; the three-worker Raspberry Pi topology,
resource bounds and test fixture are described in [initial evidence](ops010-evidence.md).

## Method

An observed operating point requires all scheduled requests to complete
successfully, no invariant failures, bounded in-flight work, and no accumulating
outbox backlog. Short sweeps locate candidate rates; longer confirmation runs
exercise the selected rates. These measurements describe the tested interval,
not a guaranteed maximum or long-term availability estimate. Read, mutation,
and active stream profiles are measured separately; their maxima are not
additive. No durability setting or authorization freshness bound is relaxed.

The external driver supports fractional rates down to 0.01/s. It measures the
entire scheduled window, distinguishes late scheduler drops from full
concurrency slots, and permits at most 100 ms of scheduler catch-up (or one
request interval for slower rates). A semaphore bounds requests without an
idle-worker startup race. There is no hidden request retry in capacity runs.
Recovery probes check completed-response replay separately from uncertain
outcomes. No general retry safety is claimed without a failure-window test.

Stream probes reconcile first and start after the returned authoritative
sequence. A controlled run has no other revocation writer. Each client must
receive at least the successful mutation count, with strictly increasing
sequences and nondecreasing epochs; the driver allows 30 seconds to drain the
last changes. It checks all requested connections and the 25% reconnect count.
This is controlled fanout evidence, not a complete gateway implementation.

The first exploratory read probes overlapped local driver development and are
not used to establish a stable envelope. Their reports remain available as
exploratory evidence. Subsequent capacity runs have no concurrent builds,
integration tests, other load scenarios, or injected faults.

The read confirmation used the driver in this change with `-mode read -rate
100 -workers 64 -duration 5m`; the lower admission confirmation used `-mode
admission -rate 0.025 -workers 1 -duration 320s`. Both use the same private
fixture directory and retained public endpoint as the initial report. Each
invocation includes the driver's prerequisite admission/isolation/replay smoke
checks before the measured window. The read result is on image `55b646ff…`;
subsequent repairs address revocation paths and were validated separately.

## Correctness repair found by the rehearsal

Live mTLS reconciliation returned HTTP 500 even with an empty log: application
reconciliation probes 10,001 records to choose between the published 10,000-item
delta and a snapshot, but the PostgreSQL adapter limited queries to 1,000.
The adapter now accepts the bounded 10,001-record probe and rejects 10,002.
The real PostgreSQL regression test passed, and the deployed mTLS endpoint
subsequently returned HTTP 200 with an authoritative delta. The public contract
and production capacity objectives are unchanged.

The corrected API image is
`sha256:55b646ff1c7dfebf5d8b392ddfeba0802d727e87f296acdce5abeee1e1e5e693`.
This development image was built from the implementation accompanying this
report, labeled `uncommitted-homelab`. Existing fixture, OPA, PostgreSQL,
identities, Services and PVCs were reused.

## Admission retry defect — correctness blocker

A controlled failure-window test on image `55b646ff…` delayed completion of one
synthetic idempotency record by 20 seconds. PostgreSQL's statement deadline
caused HTTP 503 after about 10.1 seconds, but one Run was already committed.
The test disabled its trigger, waited 65 seconds for the one-minute acquisition
lease to expire, then submitted the exact same key and body. The retry returned
201 and created a second Run: **two durable Runs for one idempotency key**.

This is a software correctness failure, not a reduced hardware capacity target.
Admission and idempotency response completion currently commit in separate
transactions. Completed-response replay can work while this failure window
still duplicates effects. Do not treat a retry of an uncertain admission as
safe. Correctness qualification remains failed until the mutation and replay
outcome have an atomic recovery boundary and the fault test passes. The
remaining independent diagnostics do not override this failure.

The key-specific trigger `ops010_delay_idempotency` is retained **disabled** in
the disposable load database, along with its helper function, for reproducibility.
No other keys were targeted. Neither helper is a migration or production
artifact. The aggregate reproduction report contains no keys, identities, or
request bodies.

## Measurements and recovery results

The two-minute 100/s read probe passed 12,000/12,000 requests at 32 client
slots (p95/p99 17.6/23.0 ms). At 200/s with the same bound, 23,955 requests
succeeded and 45 were dropped because all slots were occupied; no server or
transport errors occurred (p95/p99 24.2/76.5 ms). This is a bounded-client result,
not a claim that 200/s is the server's absolute limit. The sweep stopped there.

An initial three-minute admission probe at 0.1/s and one slot completed 14
successful admissions, one transport timeout, and three concurrency drops out
of 18 offered requests. Client median was 94.7 ms but p95 reached the 15-second
header timeout. That driver version still had an idle-worker startup race,
subsequently removed; the observed timeout remains valid evidence of flash
storage variability. It does not establish a retry-free write envelope.

The five-minute read confirmation at 100/s with 64 bounded client slots passed
30,000/30,000 requests, with no drops, transport errors, or invariant failures.
Client p95/p99 were 17.95/35.26 ms. A mid-run process-side histogram sample was
10.0/22.0 ms; OPA was 4.82/4.98 ms. All three scrape targets were up, pending
outbox count and age were zero, and aggregate API CPU was about 0.49 cores with
96.9 MB resident memory. These are sampled monitoring values, not maxima.

Use **100 reads/s with at most 64 in-flight requests** as the observed read
profile for subsequent homelab work. It is not a production capacity guarantee
or a promise that larger concurrent mutation/stream loads can be added freely.

The lower admission confirmation at one request every 40 seconds ran for
320 seconds: 7/8 succeeded and one returned 503, with no generator drops.
Median client latency was 121 ms; the slowest response was about 15 seconds.
No retry-free write operating point was established. Lowering arrival rate
alone does not eliminate intermittent durable-write stalls, and the separate
retry defect prevents recommending automatic retries as a workaround.

An initial four-client stream test established all four connections and one
reconnect, but both revocation requests returned 503. Inspection found another
live-path defect: revocation authorization omitted the empty constraint maps
required by the policy input contract. The service now supplies those maps;
its test evaluator validates the real input contract. This fault is distinct
from storage latency. Fanout is rerun after deploying that correction.
The second image is
`sha256:e7e993843d87ed65d8c827042d8e2cb8aeb34ed0084ba66acf8af8ad20606146`.

The four-client rerun delivered the one successful revocation to all four
clients (920 ms p99 observed lag), but the next write returned 503 and the API
became unready. This exposed a third software defect: the runtime combines a
tenant-filtered log sequence with the global security epoch. Another tenant's
revocation can advance that epoch without advancing this tenant's sequence.
The freshness tracker rejected that authoritative observation as a conflicting
duplicate, including on startup at sequence zero. A restart alone did not help.

Authoritative reconciliation now accepts nondecreasing epochs at the same
filtered sequence. Stream duplicate conflicts and sequence/epoch regressions
remain rejected. A regression test covers initial reconciliation at sequence
zero, a later global epoch advance, and fail-closed epoch regression. This is
consistent with the existing reconciliation contract, not a freshness bypass.
The recovery image is
`sha256:7f52eba9bd8c31d6e6c499032561336f1e53359990626584967f1edecb9aeef9`.

Recovery uses sequential synthetic operations and bounded, reversible service
interruptions; it does not claim a stable mutation throughput. Early attempts
were interrupted by an admission 503, a reconciliation 500, and Service endpoint
withdrawal during rollout. Completed admissions were reused for subsequent
recovery; uncertain admissions were not retried. An initial revoked-read probe
incorrectly expected 403; the observed enumeration-safe 404 matches the Run
visibility contract, and the corrected probe passed. The reconciliation 500
remains a failed attempt even if a later probe succeeds.

Aggregate reports are retained in [homelab-results](homelab-results/). Connection
details, credentials, raw runtime payloads and local operator notes are excluded.
Production targets remain unchanged regardless of these outcomes.

The final four-client probe on image `7f52eba9…` passed three scheduled
revocations over 120 seconds, with one reconnect and all 12 expected deliveries.
There were no drops, errors, gaps or incomplete clients. Mutation p95 was
10.95 seconds and observed fanout lag p99 was 10.94 seconds. This is successful
small-sample delivery/reconnect correctness, not compliance with production
latency or proof of long-term write stability.

The subsequent 16-client probe also passed all three revocations over 120
seconds, all 48 expected deliveries, and four reconnects, without errors or
gaps. Mutation p95 was 7.62 seconds and delivery lag p99 was 7.93 seconds.
Use at most 16 streams for further bounded correctness diagnostics; this is the
largest tested small profile, not a demonstrated capacity ceiling. The restored
SQL invariant checks passed afterward, and the fault trigger was disabled.

## Deployed recovery results

| Scenario | Observed result |
| --- | --- |
| Completed admission replay / changed body | Identical 201 replay / 409 conflict |
| Tenant isolation / trusted producer spoofing | Foreign read 404; unauthorized metering and spoofed producer 403 |
| Revocation and lift | Create 201, revoked read 404, lift 200; subsequent reconciliation attempt failed 500 |
| OPA unavailable / restored | Three protected reads 503; authorized read 200 after restoration |
| API rolling restart | Completed admission replay preserved; 12.4 seconds including rollout/readiness |
| PostgreSQL unavailable | Protected read 500; direct pod readiness 503 and liveness 200 |
| PostgreSQL restored | Readiness recovered in 82.4 seconds after scaling up; exact admission replay preserved; outbox pending zero |
| Lifecycle signal / cancellation | Signal 202; cancellation 200 and CANCELLED state; both exact completed-response replays |
| Evidence sink pause | Six successful distinct admissions at 30-second spacing; six deliveries pending after a 185-second pause |
| Evidence sink recovery | Six pending deliveries drained in 7.6 seconds after restoration; no missing receipts, no lost retained outbox rows, zero broken receipt-chain links |

## Recovery scope

These checks exercise the deployed public/trusted handlers, real PostgreSQL,
OPA, and durable fixture sink. The API and database restarts are controlled
Deployment restarts/scaling, not abrupt power loss or PostgreSQL HA failover.
The temporary sink pause reuses its PVC. Its co-located OIDC keys are already
cached during the pause; this is not a cold-start issuer outage qualification.
No workers, optional Valkey cache, managed production signer, or HA database are
composed in this fixture. Their repository tests cannot be presented as live
qualification of those absent components. Contested allocations and settlement
invariants use the separate real-PostgreSQL integration/E2E suite; their
production capacity remains open. A complete deployed gateway is absent, so
its long-partition fail-closed behavior is not claimed from this probe. The
three-minute sink interruption is a reduced correctness rehearsal; it does
not replace the production 15-minute backlog or twice-peak recovery target.

## Reproducing the admission failure window

Run only against the isolated synthetic fixture, with no other Run admissions
for that tenant. The opt-in script retains its key-specific helper trigger and
function and disables the trigger in a `finally` block. It uses the runtime's
one-minute idempotency lease and ordinary 10-second PostgreSQL statement limit.
It exits nonzero unless the fault occurs and the retry produces exactly one
Run in total.

```sh
python3 test/operations/idempotency_window.py \
  --base http://test-api.example:30080 --identities /private/ops-identities \
  --ssh-host test-controller --namespace isolated-ops \
  --allow-fault-injection
```

The script prints aggregate counts and statuses only. Its credential input is
the fixture's existing private token file; PostgreSQL access uses the isolated
fixture's administrative controller path. It is not included in `make verify`,
which cannot safely assume a disposable deployed cluster. Preserve this failing
operational regression until the atomicity repair is qualified.

The checked-in portable script was also executed against recovery image
`7f52eba9…`; its aggregate result is [idempotency-portable.json](homelab-results/idempotency-portable.json). It returned 503 after committing one Run; the later retry returned 500 and the
Run count stayed at one. It exited 1 because retry recovery failed. This second
probe does not independently reproduce the duplicate, nor does it invalidate
the earlier two-Run reproduction; no admission atomicity fix was made.

## Repository verification

`make --silent verify` passed with the pinned OPA tool and the isolated
`ops_verify` PostgreSQL database on the retained cluster. This includes generator
checks, formatting/static analysis/OpenAPI validation, unit/coverage and race
tests, policy tests, real-PostgreSQL integration (325.8 seconds), E2E (331.2
seconds), security tests, Compose/Kubernetes checks, dependency/vulnerability/
license gates, build, and container smoke checks. Focused application/load race
checks and the real-PostgreSQL reconciliation regression also passed. The Python
fault script passed syntax validation and was executed as recorded above.

Passing the repository gate does **not** override the separate failing
operational admission retry probes. The latter remain opt-in because they
require an explicitly disposable deployed fixture and fault injection.

All three API replicas, PostgreSQL and the fixture were restored to readiness;
pending evidence was zero and the injection trigger was disabled. Deployments,
Services, identities, PVCs and diagnostic helpers are retained for reuse.
The local `docs/operations/homelab.md` is ignored and excluded from this change.
