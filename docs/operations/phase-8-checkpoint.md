# Phase 8 evidence checkpoint (OPS-014)

Current status: [Phase 8 integration-RC closeout](phase-8-closeout.md) supersedes
the earlier completion blockers below under the owner-approved
[qualification deferrals](deferred-qualification.md). Historical results and
production targets remain unchanged.

Review date: 2026-09-18 UTC. **Phase 8 and OPS-014 remain open.** This checkpoint
consolidates committed operational evidence and fresh artifact-generation
results; it is not a release approval or a waiver of OPS-010/OPS-011.

## Evidence map

| Area | Implemented/verified evidence | Source |
| --- | --- | --- |
| OCI, manifests, hardening, runbooks and release automation | OPS-001–004, OPS-008, OPS-013; historical multiarch image, scan, install/uninstall and release contract checks | `967165a`, [Phase 8 evidence](../phase-8-evidence.md) |
| Readiness, telemetry, alerts | OPS-005–007; freshness-bound readiness, live collectors and 27 validated alert rules | `e528009`, `01135d6`, `41ab200`, `bd203e5`, [Phase 8 evidence](../phase-8-evidence.md) |
| Backup and PITR | OPS-009; encrypted physical backup/WAL recovery, authoritative invariants and forward migration | `fa04a0c`, [recovery procedure](recovery-testing.md) |
| Capacity and correctness | OPS-010 partial; 100 reads/s hardware confirmation, smaller fanout/recovery and a reproducible admission atomicity failure | `8ceb610`, `982ecba`, [homelab results](homelab-qualification.md) |
| Resilience | OPS-011 partial; retained-cluster faults, quiesced promotion preserving 41 tables, real-adapter probes | `8e3a1de`, [OPS-011 report](ops011-evidence.md) |
| Lifecycle | OPS-012 passed; governed HTTP lifecycle, 73 image-transition checks, observed 3→4→3 HPA scaling and 1,360 successful reads | `3da7230`, [OPS-012 report](ops012-evidence.md) |
| Updated image dependency | gRPC patch and its required x/net update; current artifact and repository gates | `99c8ae9`, results below |

Historical passing reports remain tied to their original revisions, images,
scanner databases and hardware. They do not certify later images or silently
replace current failures. Production targets remain in [SLOs](slos.md).

## Fresh artifact-generation evidence

The initial rerun targeted the OPS-012 image
`sha256:3d0e53d9c0bfab3772482915eb39e757594ce4815d432006ff0b77fb1a373dc4`.
Trivy 0.69.3 refreshed its vulnerability database to 2026-09-18 and found one
fixable HIGH finding: **CVE-2026-84445**, gRPC v1.83.1. The release target failed
at its threshold as intended, before writing provenance and checksums. This
supersedes the earlier clean scan for current vulnerability status, not the
historical record of what was tested.

The [upstream advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj)
describes an xDS-server denial of service and identifies v1.83.2 as patched.
The dependency is included through the OTLP tracing exporter; this finding is
not evidence that AG exposes an xDS server. The image gate checks bundled
packages, while `govulncheck` checks reachable code, so a passing call-graph scan
does not substitute for the failing image gate.

Commit `99c8ae99064e2d71e5a4cec3bdb4d80fbb89c090` updates gRPC to v1.83.2
and its required `golang.org/x/net` to v0.58.0. Focused telemetry/runtime tests
and `make vulnerability-check` passed before rebuilding. The first full gate
passed integration/end-to-end/adversarial security tests but then failed the read-only
dependency check because `go mod tidy` had pruned full-graph checksums.
`go mod download all` restored those entries in `20f8a2e`, and the 104-module
dependency check passed. The net dependency diff is only the two version pins
and their generated checksums; no new direct dependency or public contract
was introduced. Image source remains `99c8ae9`; the checksum-only follow-up does
not change selected module versions or application source.

The replacement image is built with the standard pinned Dockerfile for both
linux/amd64 and linux/arm64, with BuildKit SBOM/provenance enabled. Aggregate
scan results and generated artifact hashes are recorded in
[artifact inventory](ops014-results/artifacts.json). Both explicit platform-manifest scans passed with zero HIGH/CRITICAL
findings, and every generated SHA-256 checksum verified. Six shared API/schema/
Kubernetes outputs were byte-identical between runs. Each platform still reports
four MEDIUM and two UNKNOWN findings counted across the two binaries (three
unique advisories in x/crypto); these are retained in the inventory, not called
a zero-vulnerability scan. `govulncheck` found none reachable.

The first index-based ARM64 scan selected amd64 despite the supplied environment
hint. That attempt is not ARM64 evidence; final runs used explicit child-manifest
digests and checked the architecture reported by Trivy. The private build and
artifact output directory is `.cache/ops14`; credentials and raw operational
payloads are excluded from committed evidence.

The script's `provenance.json` is unsigned helper metadata, not verified SLSA
provenance: its subject uses a Git revision and its builder string names the
GitHub workflow even in a local invocation. The inventory separately binds the
actual immutable image digest and exact source revision. BuildKit attestations
are generated, but no cosign signature, GitHub OIDC verification, tag workflow
or public release was invoked in this local rehearsal. Those are not claimed
as passing local verification.

## Reproduction and repository checks

Use source `99c8ae9`, version `ops014-checkpoint` and `SOURCE_DATE_EPOCH=1789771649`.
Build the normal Dockerfile with Buildx platforms `linux/amd64,linux/arm64`,
`--provenance=mode=max --sbom=true`, and version/revision build arguments. Resolve
the resulting index to each platform manifest; for each one run:

```sh
OUTPUT_DIR=/private/artifacts-amd64 make release-artifacts VERSION=ops014-checkpoint REVISION=99c8ae99064e2d71e5a4cec3bdb4d80fbb89c090 IMAGE=registry.example/ag@sha256:<platform-manifest-digest>
(cd /private/artifacts-amd64 && sha256sum --check SHA256SUMS)
```

The Make target derives the timestamp from the checked-out commit. Keep registry
authentication outside source and use a separate output directory per platform.
Scanner database updates and SBOM metadata can change report bytes; the recorded
checksums identify these exact outputs, not a guarantee of reproducible scanner
reports. Source archive/API/schema identity was checked independently.

Full `make verify` passed at source `20f8a2e` with the checkpoint documents
staged: generation/static/OpenAPI checks, unit/race tests, 27 policy cases,
PostgreSQL integration (260.837 seconds), end-to-end PostgreSQL tests (380.366
seconds), adversarial security, dependency/vulnerability/license checks,
Compose/Kubernetes validation, build and hardened-container smoke. Tests used
the isolated `ops_verify` database; no operational faults ran concurrently.
The initial gate failure and its checksum-only correction are recorded above.
Updated documentation links resolve; diff/credential checks passed, and
`docs/operations/homelab.md` is excluded from the commit.

## Closeout blockers and next work

| Gate | Required evidence before OPS-014 can close |
| --- | --- |
| OPS-010 capacity | Complete the API/policy/admission/allocation/SSE/outbox/revocation workload matrix on the intended topology; preserve original production targets and report hardware limits separately. |
| OPS-011 resilience | Qualify production-composed Valkey/Run-worker failures and intended-topology partition/failover behavior. Test-process probes and quiesced promotion do not close those gates. |
| Final closeout | After those gates pass, refresh image/artifact evidence and run the full repository gate on the final source, then commit the Phase 8 completion record. |

The admission defect identified at this checkpoint was subsequently fixed in
`406db54`, with deployed retry and concurrent-key checks recorded in the
[wired retry](wired-qualification.md). The remaining workload and topology
gates above still prevent Phase 8 completion and Phase 9 release-candidate
closure.

## Retained environment

No cluster resources were removed or changed for this checkpoint. API remains
on the OPS-012 rollback image; the promoted `ops011-standby` writer and fenced
old `postgres` Deployment retain their roles. The patched image is available
for later deployment qualification but was not rolled into the homelab here.
Local `homelab.md`, private identities, credentials, lease/admission checkpoints
and raw logs remain uncommitted.
