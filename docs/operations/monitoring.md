# Monitor service health and governance

Enable Prometheus metrics, restrict `/metrics` to your monitoring network, and
install the optional [ServiceMonitor and alert rules](../../deploy/kubernetes/optional)
when Prometheus Operator CRDs exist. Configure OTLP only when a trusted collector
is available. The [metrics reference](observability.md) documents names and
bounded labels; [logging](logging.md) explains safe structured diagnostics.

| Watch | What it tells you / action |
|---|---|
| Ready replicas, request count/errors and duration by route | Availability and client latency; investigate dependencies before restarting healthy processes |
| OPA decision outcomes and duration | Authorization dependency failures; preserve denial while restoring verified policy |
| PostgreSQL health, operation latency and pool saturation | Authority bottleneck; budget connections across all replicas and workers before scaling |
| Outbox count/oldest age, receipt/checkpoint progress | Independent evidence backlog; check database and sink durability/latency; page at five minutes |
| Revocation lag, gaps and freshness | Stale authority; remove stale replicas from readiness and reconcile consumers |
| Admission/allocation outcomes and settlement lag | Accounting or lifecycle problems; verify conservation and replay before restoring traffic |
| CPU, memory, storage latency and network | Hardware saturation; distinguish it from software contention and offered-load drops |

Use the [SLO definitions](slos.md) and shipped rules for burn-rate alerting.
Correctness, unauthorized access and evidence loss have no error budget. Record
both client offered/completed/dropped counts and server metrics: low server
latency can hide requests that the load generator could not send.

The wired Pi cluster with SSD PostgreSQL and evidence sink demonstrated 25
admissions/s for five minutes with no drops and publication p99 1.191 s. A
separate read-only run completed 1,000 reads/s for one minute. These are separate
measured points, not a combined workload guarantee. Higher-rate runs failed;
see [capacity and scaling](capacity.md) for exact counts and evidence. Keep
those hardware results separate from unchanged production targets.

Do not put tenant IDs, raw URLs, objectives, tokens or payloads in labels or
trace attributes. Link incidents using safe aggregate diagnostics and the
[incident procedures](incidents.md).
