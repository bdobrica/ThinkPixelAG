# ADR-0011: Restricted deployment and evidence-bound integration releases

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

AG needs reproducible deployables and recovery evidence, while limited homelab
hardware and unavailable dependent components cannot establish production-scale
qualification. The owner explicitly prioritized an integration RC without
weakening correctness/security or rewriting failed performance evidence.

## Decision

Build pinned multiarchitecture OCI images with static binaries, version/revision
metadata and a minimal shell-free non-root runtime. Kubernetes enforces read-only
roots, dropped capabilities, seccomp, bounded resources, explicit secret delivery,
default-deny networking, topology placement and disruption budgets. HPA/monitoring
CRDs are optional. Environment overlays supply real destinations and image digests.

Run migrations explicitly before readiness, never implicitly from each API pod.
Readiness includes PostgreSQL, valid active policy and bounded revocation
freshness; liveness remains process-only to avoid dependency restart storms.
Bound database pools, HTTP/OPA work, streams and telemetry cardinality.

Use aggregate Make verification, contract compatibility, real PostgreSQL,
race/fuzz, adversarial tests and live deployment evidence. Record source,
platform, scope and immutable digest; historical and current tests are distinct.
Artifacts include API/schemas, Kubernetes assets, SBOMs, scans and checksums with
provenance/signature hooks. Unsigned metadata is not authenticated provenance.

Keep production SLO numbers unchanged. DQ-001–004 defer production capacity,
AR/gateway composition and intended-production HA under the owner's integration
scope. Hardware-limited results stay separate; known correctness, security,
evidence-loss or critical/high findings still block release. Retain homelab
resources until the owner requests cleanup.

## Alternatives considered

Mutable tags/unpinned dependencies obscure provenance. Privileged or writable
images enlarge exposure. Automatic per-pod migration creates rollout races.
Treating RAM storage as durable qualification or weakening SLO numbers would
hide limitations. Waiting for undeveloped peer components would block the
integration candidate needed to build them.

## Consequences

Production overlays and provider qualification remain real operational work.
The current SSD database/sink share a failure domain. Process recovery is not
host-loss HA. Root plan/checklist removal requires preserving their rationale,
history, risks and remaining release work in durable documents first.

## Security impact

Operational exceptions do not waive fail-closed behavior, tenant isolation,
monotonic authority, conservation or independent evidence requirements. Backups
and forward migration must preserve those invariants. Explicit scan thresholds
and expiring dependency exceptions remain release controls.

## Operational impact

Use tested runbooks for installation, compatible rollback, PITR/forward recovery,
policy rollback, revocation gaps and provider rotation. Observe bottlenecks before
scaling; the demonstrated 25 admissions/s is a conservative point, not a maximum.
Formal publication and signing must be distinguished from local artifact builds.

## References and evidence

- [Runbooks](../operations/runbooks.md), [supported versions](../operations/supported-versions.md)
- [Phase 8 closeout](../evidence/homelab/phase-8-closeout.md), [deferrals](0012-integration-rc-qualification-deferrals.md)
- [RC verification](../evidence/rc/verification.md), [capacity](../operations/capacity.md), [risks](../evidence/rc/risk-review.md)
- [Historical plan](../evidence/rc/history/PLAN.md), sections 7–13; OPS and RC entries in the [ledger](../evidence/rc/history/TODO.md)
- Commits `967165a`, `fa04a0c`, `3da7230`, `72715e1`, `79d2ac9`, `af620d1`
