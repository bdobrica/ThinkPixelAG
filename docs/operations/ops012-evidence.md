# OPS-012 retained-cluster lifecycle evidence

Qualification date: 2026-09-18 UTC. **OPS-012 lifecycle qualification passed.**
This rehearsal adds the governed core
workflow, observed autoscaling, and immutable-image upgrade/rollback to the
previous disposable Kind install/migration/disruption/uninstall evidence at
`4fff867`. The homelab resources are retained at the operator's request.
Production SLOs and HPA configuration are unchanged.

## Environment and images

The existing four-node ARM64 Raspberry Pi 4 K3s cluster uses WiFi and USB flash
storage. Three API replicas run the actual governed executable with OIDC, OPA,
PostgreSQL, mTLS and the persistent test evidence sink. The current database
writer remains `ops011-standby`; the old `postgres` Deployment remains at zero.
Schema version is 18. Dependencies and fixture provenance are recorded in
[OPS-011 evidence](ops011-evidence.md) and
[homelab qualification](homelab-qualification.md).

The two immutable images in `quay.io/bdobrica/thinkpixelag` are:

| Role | Image digest | Provenance |
| --- | --- | --- |
| Original and rollback | `sha256:7f52eba9bd8c31d6e6c499032561336f1e53359990626584967f1edecb9aeef9` | Retained OPS-010 recovery image; source provenance in homelab report |
| Candidate | `sha256:3d0e53d9c0bfab3772482915eb39e757594ce4815d432006ff0b77fb1a373dc4` | ARM64 build from `8e3a1de`, version `ops012`, standard pinned Dockerfile, pushed to Quay |

These are separate application builds with different version/revision metadata
and the same compatible schema and application behavior. This proves the image
rollout/rollback mechanism and state compatibility for this pair; it does not
claim compatibility across arbitrary functional releases or schema changes.
The operational runner is test-only and is not included in either image.

## Executed lifecycle matrix

The [posture report](ops012-results/posture.json) passed live Pod checks for
non-root UID 65532, RuntimeDefault seccomp, read-only root filesystems, disabled
privilege escalation, all capabilities dropped and disabled service-account
token mounting, plus schema 18, readiness and metrics.

The [upgrade report](ops012-results/upgrade.json) contains **73 passing checks**:

- Before upgrade, on the candidate, and after rollback: admit a Run through
  authenticated HTTP, replay the exact response, reject changed content under
  the same key, read authorized state, hide it from another tenant and reject
  an invalid token.
- Accept and replay a durable signal; cancel and replay the terminal result;
  stream exactly the three ordered events and resume strictly after an issued
  cursor; verify exactly three durable events and restored-state invariants.
- Complete explicit candidate migration Job `ops012-migrate-3d0e53d9c0bf`, leaving
  schema 18 unchanged. No down-migration or API-startup migration was used.
- Converge ready Pods on the candidate digest and then the original digest.
  Replay previously admitted responses and preserve terminal state across both
  transitions, including state created by the candidate after rolling back.
- Repeat the restricted Pod/schema/readiness checks on both images.

The workflow starts with the fixture's approved agent and validated policy.
It does not execute an AR harness, register new agents through an uncomposed
administration route, or qualify trusted metering/settlement. Requests are
serial; successful admissions are replayed, but uncertain admissions are never
retried. The separate OPS-010 admission atomicity defect remains open.

## Autoscaling

The [HPA report](ops012-results/hpa.json) records controller metrics, replica
observations and all read outcomes. Actual scale-out from three to four and
scale-in back to three passed; all **1,360 protected reads succeeded** with zero
errors. The diagnostic uses API-container CPU with
a 20m average target, three-to-four replicas and a 60-second downscale window.
The bounded workload is at most 40 protected reads/second with 16 clients.
Scale-in is observed after stopping traffic while retaining the same target.

After the drill, the retained HPA uses a 175m API-container target and a
300-second downscale window. Resource requests/limits were not changed.
This qualifies metric-driven controller behavior on the small cluster, not the
production whole-Pod 70% CPU policy, 3–12 replica capacity or production SLOs.
The [workflow instructions](lifecycle-testing.md) explain those distinctions.

## Retention and verification

Original API image restored; database role topology unchanged. The migration
Job, HPA, existing Deployments, Services, Secrets and PVCs remain for reuse.
No homelab uninstall was performed. Clean install, migration, PDB-blocked drain,
pod replacement and complete uninstall remain covered by the earlier disposable
Kind run; live eviction/restart follow-up is in the OPS-011 report.

Four local `make test-lifecycle` safety regressions passed: mutable-tag rejection,
rollback on candidate failure, cross-image durable-state checks, and no retry
after uncertain admission. Full `make verify` passed on the source committed
with this report: generation, formatting/static/OpenAPI checks, unit/race tests,
27 policy cases, PostgreSQL integration (148.458 seconds), end-to-end database
tests (179.327 seconds), adversarial security, Compose/Kubernetes validation,
dependency/vulnerability checks, build and hardened-container smoke. The
isolated `ops_verify` database was used; no operational faults ran concurrently.
Private identities, admission checkpoints, build logs and credentials remain
outside source control; reports contain aggregate checks only.
