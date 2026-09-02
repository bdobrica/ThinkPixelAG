# Phase 8 production operations evidence

- Status: in progress
- Review date: 2026-09-02
- Implementation commit: `2c919cd`
- Host: Linux/amd64, Docker, kind v0.30.0 (Kubernetes v1.34.0), kubectl v1.35.0, PostgreSQL 18.4

## Implemented controls

The OCI build now produces the API and explicit forward-only migration binary
for `linux/amd64` and `linux/arm64` from digest-pinned builder/runtime images.
The runtime remains shell-free UID/GID 65532, contains CA roots, supports a
read-only root, carries deterministic build labels, and drains on SIGTERM.

The Kubernetes base provides Deployment, Service, ServiceAccount without token
mounting, non-secret ConfigMap, managed Secret references, pre-deploy migration
Job, restricted security contexts, default-deny networking, resource bounds,
topology spreading, and PDB. Process-only startup/liveness use `/livez`;
governed readiness uses `/readyz`; the probe abstraction supports PostgreSQL and
composed security-freshness checks without making disposable Valkey authoritative.
The current production process assembly wires PostgreSQL but not the policy and
revocation probes, so OPS-005 remains open.
Optional HPA, ServiceMonitor, dashboard, SLO alerts, bounded metric definitions,
and operator runbooks are separate from the base. Application/background-worker
metric observations still need production assembly wiring, so OPS-006 remains
open.

Release automation builds and pushes an immutable multi-platform image, emits
BuildKit SBOM/provenance attestations, generates CycloneDX SBOM, JSON
vulnerability report, deterministic manifest/API archives and SHA-256 checksums,
provides cosign sign/attest/verify hooks, enforces zero fixable critical/high
image vulnerabilities, and creates draft release notes. GitHub Actions and
container dependencies are immutable-pinned; release authority is limited to
the tag workflow.

## Executed evidence

- `make kubernetes-check`: contract checks and Kustomize render passed with
  digest-pinned kubectl v1.35.0.
- A Buildx OCI export for `linux/amd64,linux/arm64` passed; manifest-list digest
  was `sha256:b6fc5c9712f1b27ba2046a1fa8bcbdedf5bbb307d782a50035203b42491d7d16`.
- `scripts/release-artifacts.sh` generated the SBOM, vulnerability JSON,
  provenance, API/Kubernetes archives, and checksums under `/tmp`. Trivy 0.69.3
  reported zero vulnerabilities in the Debian runtime and both Go binaries
  after updating `x/crypto` to 0.55.0 and gRPC to 1.83.1. The signing hook was
  intentionally not invoked because no private signing authority was supplied.
- `make test-backup-restore` made a custom-format backup of PostgreSQL 18.4,
  restored it into an isolated temporary database, and passed schema,
  epoch/checkpoint, outbox/evidence-link, and extension-aware allocation checks.
- `test/cluster_smoke.sh` passed on a disposable kind cluster: Secret creation,
  install, default-deny network policy, explicit migration completion, restricted
  API runtime, live/ready/metrics probes, HPA presence, PDB-blocked voluntary
  drain, pod replacement, rolling configuration upgrade, rollback, uninstall,
  and automatic cluster cleanup.
- Clean-tree `make verify` passed at `2c919cd`, including generation drift,
  lint/OpenAPI, unit/coverage, race, 26/26 Rego, PostgreSQL integration/e2e and
  security tests, Kubernetes rendering, dependency policy, govulncheck, license,
  static build, image build, and hardened container smoke.

## Remaining exit qualifications

The evidence above does not qualify production probe/telemetry composition,
PITR, production-shaped capacity, or the complete fault matrix. Before Phase 8
can close:

- OPS-005 and OPS-006 must assemble the policy/revocation readiness sources and
  application/background-worker metric observations in the production command.

- OPS-009 must restore an encrypted physical backup plus WAL to chosen points
  around governance transactions and record RTO/RPO and invariant output.
- OPS-010 must run the documented full API/policy/admission/allocation/SSE/outbox/
  revocation workloads against the intended production topology and report
  percentiles, saturation, and tuned limits.
- OPS-011 must finish environment fault injection for DB failover/latency and
  cross-zone stream behavior; deterministic tests already cover OPA timeout and
  malformed responses, cache loss/poisoning, revocation gaps/reconciliation,
  worker lease recovery, pod eviction, and rolling restart.
- OPS-012 still requires a governed core API workflow in-cluster. The current
  process composition exposes operational endpoints but does not yet assemble
  the previously implemented application handlers in `cmd/thinkpixelag`.

These are release blockers, not accepted residual risks. OPS-014 and the Phase
8 checkbox remain open until all four are executed and the full clean-tree gate
passes.
