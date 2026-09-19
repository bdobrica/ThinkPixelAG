# Phase 8 closeout for the integration RC

Later Phase 9 source, artifacts and the current retained API deployment are
recorded in [final RC qualification](../releases/final-artifacts.md). This report
retains the original Phase 8 image and execution evidence.

Phase 8 is complete under the project owner's 2026-09-19 integration-RC scope.
The objective is to make AG available for development and testing of dependent
components, including ThinkPixelAR with a real Codex harness. This is not a
production-capacity or whole-platform readiness claim.

The [deferred qualification register](deferred-qualification.md) preserves
unmet production performance and unavailable worker/gateway/HA scenarios.
They do not block this Phase 8 closeout or the first integration candidate.
The earlier [checkpoint](phase-8-checkpoint.md) remains dated historical
evidence; its broader closeout prerequisites are superseded by this scope.

## Completed operational scope

| Area | Evidence |
|---|---|
| Images, deployment, isolation, probes, metrics, alerts and runbooks | [Phase 8 foundation](../phase-8-evidence.md), OPS-001–008 |
| PostgreSQL backup, WAL/PITR, forward schema and invariant checks | [Recovery qualification](recovery-testing.md), OPS-009 |
| Admission atomicity, approval concurrency, replay and measured homelab limits | [SSD PostgreSQL qualification](ssd-qualification.md), [durable evidence](ssd-evidence-qualification.md) |
| OPA faults, DB latency/crash, eviction, rolling restart, lease fencing and reconciliation | [OPS-011 evidence](ops011-evidence.md), [wired recovery](wired-qualification.md) |
| Durable sink loss, exporter restart, sink crash and receipt replay | [SSD evidence recovery](ssd-evidence-qualification.md) |
| Deployed optional Valkey throughput-cache loss, malformed state and recovery | Results below |
| Install/uninstall, hardened runtime, governed HTTP lifecycle, HPA, upgrade/rollback | [OPS-012 evidence](ops012-evidence.md) |
| Final repository gate and architecture-specific artifacts | Results below, OPS-013–014 |

Historical tests retain their original source/image/topology. They are not
represented as fresh executions against every later image. The final source
passes the aggregate repository gate, and the changed Valkey composition was
exercised through the deployed API.

## Final runtime change and live verification

Source `30ff25f113ba6587adcfaa079e0d9dcac6a3f952` connects the existing optional
Valkey throughput adapter to trusted usage handling. PostgreSQL continues to
enforce usage idempotency, balances and rate limits. Valkey carries only
integrity-protected exhausted-window hints; failure cannot approve consumption
or expand authority. OPA and revocation checks remain live. No worker API,
harness execution, schema migration, dependency or new wire contract was added.

Focused runtime/application/Valkey tests passed. The live drill used an isolated
synthetic Run activated through the existing governed lease service, and one
existing mTLS fixture identity temporarily bound to the dedicated meter role.
This setup is not an AR worker qualification.

The deployed HTTP checks passed:

- Healthy Valkey: usage returned 202 and cache GET traffic was observed.
- An unsigned malformed cache entry: usage returned 202 through fallback.
- Valkey scaled to zero: new usage and exact replay returned 202; replay kept
  the same usage ID and did not append another ledger entry.
- Overspending during cache loss: 409, with no ledger change.
- Public authenticated reads remained available during the cache outage.
- Restored Valkey: usage returned 202 and cache GET traffic resumed.
- Exactly four new accepted usage entries, with restored database invariants
  passing afterward.

See [aggregate results](phase8-closeout-results/valkey-runtime.json). Initial
setup attempts stopped before fault injection: a private observation tunnel
needed reopening, then timestamps needed the contract's UTC `Z` spelling.
Those rejected metering requests did not enter the ledger. Final checks use
canonical UTC timestamps. The temporary identity binding is restored, the
synthetic Run is cancelled through the public API, and retained services remain
healthy. No fixture resources or stored evidence are removed.

## Repository and artifact gate

The final integration candidate is:

```text
quay.io/bdobrica/thinkpixelag@sha256:73f5929a7e3319a84e95d9e943cf1721638d57e4a0143e4f09cd9191ef204d87
```

The initial live Valkey drill used the same source with an unset OCI creation
label. The final image supplies the source commit timestamp; both architectures
were rescanned and their generated artifact checksums reverified. The filesystem
layers match the live-tested image on both architectures. The final digest is
deployed and passed admission/replay, cross-tenant denial, forged-token denial
and Run-read smoke checks; see [final image smoke](phase8-closeout-results/final-image-smoke.json)
and [retained state](phase8-closeout-results/final-state.json).


`make verify` passed from the clean source checkout at `30ff25f`: generated
artifact checks, formatting/static/OpenAPI checks, unit/race suites, 27/27 Rego
cases, real PostgreSQL integration and end-to-end suites, adversarial security,
Compose/Kubernetes validation, dependency/license/vulnerability checks, build
and shell-free non-root/read-only/SIGTERM container smoke. The verification
PostgreSQL database is isolated from the operational fixture and RAM-backed;
it is software coverage, not persistent-storage evidence.

Both explicit linux/amd64 and linux/arm64 manifests pass the image vulnerability
gate with zero HIGH/CRITICAL findings. Each report retains four MEDIUM and two
UNKNOWN occurrences across the two binaries. SBOMs, vulnerability reports,
OpenAPI/schema copies, Kubernetes archives and SHA-256 inventories were
generated; every checksum verifies and shared contract/manifest outputs match
between architectures. See the [artifact inventory](phase8-closeout-results/artifacts.json).

BuildKit SBOM/provenance attestations are included. The release helper's
`provenance.json` remains unsigned local metadata, not independently verified
SLSA provenance. Cosign/GitHub signing, tag-triggered publication and a formal
release were not executed. This closeout provides an integration candidate;
formal RC naming, contract freeze and release documentation belong to Phase 9.

Production SLOs are unchanged. The demonstrated conservative operating point
is 25 admissions/s for five minutes: 7,500/7,500 successful requests,
publication p99 1.19 seconds and zero final backlog. It is a tested homelab
point, not a production maximum. The record of failed 200/s publication and
higher-rate dropped requests remains available.
