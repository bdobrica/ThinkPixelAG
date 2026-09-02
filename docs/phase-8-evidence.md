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
The production command reconciles currently valid active policy metadata and
the matching authoritative revocation sequence/epoch heads from PostgreSQL in
one bounded query. `/readyz` requires that loaded policy state and revocation
state within the 30-second normal-write bound; empty, stale, invalid, lagging,
gapped, or failed reconciliation remains unready while `/livez` stays independent.
Optional HPA, ServiceMonitor, dashboard, SLO alerts, bounded metric definitions,
and operator runbooks are separate from the base. Production now refreshes
bounded outbox count/age and terminal settlement lag every 15 seconds and
exports pgx pool saturation at scrape time. HTTP, database, revocation, Go, and
process collectors are live; closed policy/allocation/admission/cache outcome
series exist at zero before their first event without synthetic observations.
The alert resource contains 27 bounded-label rules: separate fast/slow
availability-budget burn for public reads, run mutations, and admin/trusted
traffic; p95/p99 API and OPA latency; database, policy, and cache health;
revocation propagation/freshness/gaps; allocation and admission failures; and
warning/critical outbox and settlement lag. Every alert has an anti-noise
window, severity, owner, summary, and a response runbook link.

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
- OPS-005 focused unit/race and PostgreSQL integration checks passed at
  `e4de968`/`bdf7cb0`. Clean-tree `make verify` then passed against an isolated
  PostgreSQL database, including process liveness/fail-closed readiness in the
  hardened container smoke. The disposable-cluster fixture now promotes an
  explicit test-only active policy before awaiting `/readyz`.
- OPS-006 focused unit/race and PostgreSQL integration checks passed at
  `c4799b2`. Clean-tree `make verify` passed against an isolated PostgreSQL
  database, including optional monitoring resource/dashboard contracts and the
  hardened image smoke. The temporary database was removed after verification.
- OPS-007 alert and metrics unit/race checks passed at `30c321f`; the optional
  resources rendered with the pinned kubectl image and Prometheus 3.5
  `promtool` parsed all 27 rules successfully. Clean-tree `make verify` passed
  against an isolated PostgreSQL database, including all integration/e2e,
  security, Kubernetes, supply-chain, image, and hardened-container gates.
- `make test-postgres-pitr` at `fda4c64` created a pinned PostgreSQL 18.4
  primary with continuous WAL archiving, encrypted a physical base backup with
  ephemeral AES-256 key material, and recovered independently to named points
  immediately before and after one atomic governance transaction. The targets
  reproduced exact global/tenant/agent epochs, audit and outbox counts, and
  allocation consumption; both then migrated forward from schema 17 to 18 and
  passed `scripts/check-restored-invariants.sql`. Observed local target RTO was
  3 and 4 seconds, selected-target RPO was zero transactions, and encrypted
  backup SHA-256 was
  `f7bfb405de9d4a3a4d2ce1517234887f67cf43d04e98ceb4630f126c6b77fd32`.
  These measurements qualify the repository rehearsal, not a production
  provider SLA. Clean-tree `make verify` subsequently passed against an
  isolated PostgreSQL database.

## Remaining exit qualifications

The evidence above does not qualify production-shaped capacity or the complete
fault matrix. Before Phase 8 can close:

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
