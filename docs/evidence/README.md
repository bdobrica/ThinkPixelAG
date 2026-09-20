# Integration-RC qualification evidence

This index substantiates the scoped integration candidates, not an implementation
log. Decisions belong in [ADRs](../adr/README.md), version changes in the
[changelog](../../CHANGELOG.md), and operating guidance in
[operations](../operations/README.md). Results below are dated observations,
not a live cluster status or a new production qualification.

<a id="candidate-rc2"></a>
## 0.1.0-rc.2: administration and harness guidance

Qualified on **2026-09-20**, from source `0008dc0bf8c3883558f282c0d8a7d6fb06fb04ba`,
with schema **23**. The [candidate inventory](results/integration-candidate-rc2.json)
records platform digests, bundle checksums, compatibility and runtime scope.

| Component | Published immutable image index |
|---|---|
| AG | `quay.io/bdobrica/thinkpixelag@sha256:4d6784edc05a1c0873bb1fe1f8d88b66ac6705e7c4e5d67d4e26786d7fe3cb21` |
| Optional console | `quay.io/bdobrica/thinkpixelag@sha256:86d4d3730f4f76f9b16b92d2f7e508373b56937315973d2bbe2ac39b44d554fd` |

Both components passed AMD64 and ARM64 image gates with zero HIGH/CRITICAL
findings. `make verify` and `make verify-console` passed; the latter ran 18 tests.
The real local-development OIDC/software-signing walkthrough exercised policy
editing through independently approved rollback, role mappings, OPA updates,
dynamic guidance, admission/read/cancellation, restart persistence and API
operation with the published console stopped. Native ARM64 restricted-container
startup passed on K3s; this is not full enterprise installation or AR execution.
The [installation checks](results/candidate-installation.json) separately record
real bootstrap and server-side validation of the staged team manifests.

Versioned images and BuildKit attachments are published to Quay. Four local
platform bundles contain binaries (AG), matching source/installers/helper,
API/deployment assets, SBOM, scan and checksum inventories. No semantic Git tag,
GitHub release/assets, cosign signature or GitHub OIDC attestation was produced.
Unsigned metadata and BuildKit source checks do not authenticate a remote builder.
The release workflow follow-up adds the ARM emulation prerequisite exercised by
the local publication; it does not change the image's application source.

Local key custody, same-host development identities, the absent independent
receiver in this new local walkthrough, and unqualified enterprise SSO/HA remain
explicit limits. [Production capacity and cross-component deferrals](../adr/0012-integration-rc-qualification-deferrals.md)
are unchanged. Use the [current quick start](../quickstart.md) and
[packaging guide](../operations/releasing.md); bundled source documentation is
its pre-publication snapshot.

## Historical 0.1.0-rc.1

## Artifacts

The final candidate was built and checked on **2026-09-19**, from source
`a64d323d5c3bfa667e063b5f9dcd69f3cb89258f`, with schema 18.

```text
quay.io/bdobrica/thinkpixelag@sha256:82ad793f6b19460e58aeb43203bfd2f8a3563bc2d8bf53a120e92dbafa78a53a
```

| Platform | Manifest digest | Runtime evidence |
|---|---|---|
| AMD64 | `sha256:a07335298fe22e296419ae8b882f14a3f85eeb9000eae4f59cf3abb481fa838a` | [Restricted container smoke](results/runtime-amd64.json): non-root/read-only, probes and graceful termination |
| ARM64 | `sha256:a52f300345d7eb9fbde9c391b5fdc46ebaedbc9e80d2a49a83bb85182ace3c5b` | [Kubernetes smoke](results/runtime-arm64.json): admission, exact replay, tenant/token denial and Run read |

The [artifact inventory](results/artifacts.json) records source/version labels,
platform bundle checksums, SBOM/scan inventories and attachment relationships.
Both platform scans found zero HIGH/CRITICAL findings and four MEDIUM plus two
UNKNOWN binary occurrences each. These are dated findings; refresh scans before
publication. [Recorded runtime state](results/runtime-state.json) had schema 18,
zero pending outbox messages and three ready API replicas.

The OCI image and BuildKit attachments were pushed to Quay. Local release
bundles were generated but not published as GitHub assets. No semantic Git tag,
formal GitHub release, cosign signature or GitHub OIDC attestation was produced.
BuildKit and unsigned local metadata were checked for consistency; this does
not authenticate a trusted remote builder.

## Verification

- [Aggregate gate](results/verification.json): `make verify`, 2,303 passing
  test/subtest executions, 27 Rego cases, no skips, and restricted container
  smoke during preparation of source `a64d323`. The record identifies the base
  commit of that staged change; it is not a fresh gate run on each later docs commit.
- [Compatibility comparison](results/compatibility.json): the `72715e1` baseline
  retained API/schema/policy wire behavior; see the maintained
  [compatibility guide](../contracts/compatibility.md).
- [Fuzz results](results/fuzz.json): bounded RC verification rehearsals;
  original clean-checkout source `79d2ac9`, not final-image capacity evidence.

## Subsequent correctness checks

[RC-102 constraint inheritance](results/admission-constraint-inheritance.json)
records focused admission/policy tests, persisted PostgreSQL grants and the
aggregate gate for the source fix. This does not qualify a replacement for the
pinned release image above or change the production-capacity deferrals.

## Capacity

Keep the exact samples behind the [capacity guide](../operations/capacity.md),
including the failed higher-rate attempts:

| Workload on 2026-09-19 | Retained measurements | Result |
|---|---|---|
| 25 admissions/s, 300 s | [Requests](results/admissions-25.json), [publication](results/publication-25.json) | 7,500/7,500; no drops; publication p99 1.191 s |
| 200 admissions/s, 60 s | [Requests](results/admissions-200.json), [publication](results/publication-200.json) | 9,587/12,000; 2,413 drops; publication p99 107.7 s; failed qualification |
| 1,000 reads/s, 60 s | [Requests](results/reads-1000.json) | 60,000/60,000; no drops |
| 2,000 reads/s, 300 s | [Requests](results/reads-2000.json) | 599,230/600,000; 770 drops; failed qualification |

The wired ARM64 Pi cluster used SSD PostgreSQL on an AMD64 workstation.
Read samples precede the evidence-export fix (API source `bd148a9`);
admission/publication samples use API/exporter source `c1d63b0`, fixture
`bd148a9`, and an SSD sink/exporter on that workstation. Four API replicas
were held for the admission measurement. These are separate workloads and
historical builds, not a combined capacity test of the final image. Database
and sink flushes were enabled; the SSD services share one failure domain.

## Recovery

| Scope | Retained evidence | Limit |
|---|---|---|
| SSD database crash | [42-table comparison and invariants](results/database-recovery.json) | 2026-09-19 SSD setup, API `bd148a9`; process restart, not host-loss HA |
| Sink/exporter interruption | [Receipt/checkpoint recovery](results/evidence-recovery.json) | 2026-09-19 setup, exporter `c1d63b0`; durable replay on one host |
| Trusted usage during Valkey faults | [Runtime outcomes](results/valkey-recovery.json) | Source `30ff25f`; optional hints, live PostgreSQL/OPA authority |
| Disconnected consumer | [Freshness denial and reconciliation](results/consumer-reconciliation.json) | Real mTLS test consumer disconnected 31 s; no production gateway |
| Deployment and image lifecycle | [Posture](results/deployment-posture.json), [autoscaling](results/autoscaling.json), [upgrade/rollback](results/upgrade-rollback.json) | 2026-09-18 WiFi/flash cluster, candidate `8e3a1de`; one compatible schema-18 image pair |
| Policy rollback, key guards, break glass and revocation | [Component rehearsal](results/component-recovery.json) | 2026-09-19 staged change based on `af620d1`; real PostgreSQL and synthetic identity/provider fixtures, not production KMS/IdP ceremonies |

Earlier PITR, installation and diagnostic runs remain in Git history. Repeatable
procedures live in the [recovery guide](../operations/backup-recovery.md) and
[test instructions](../operations/testing/recovery-testing.md).
Production capacity, real AR/harness and gateway recovery, and intended-production
HA remain [deferred](../adr/0012-integration-rc-qualification-deferrals.md).
The [security qualification summary](../security/qualification.md) states the
remaining deployment limitations. Correctness/security and evidence-loss gates
remain unchanged.

## Retention and provenance

Retained JSON files preserve their original bytes, including historical paths
and base-commit fields. The index distinguishes final-image checks from earlier
component and topology tests. Each retained file supports a claim above; do not
add phase narratives, copied plans, checklists or duplicate gate reports here.

The removed material remains available at Git revision `130fbd21ae27e72912174ce6d2c42fa0318a6989`.
For example, `git show 130fbd21ae27e72912174ce6d2c42fa0318a6989:docs/evidence/rc/history/PLAN.md`
retrieves the old planning snapshot without keeping a second plan in this tree.
