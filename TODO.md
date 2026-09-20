# Scoped candidate checklist

Sections 1–4 are implemented. Their detailed completed sequencing remains in Git
history; accepted decisions and operator guidance are linked from [PLAN.md](PLAN.md).

## 5. Package and close the scoped candidate

- [x] **RC-114 — Update runnable installations and operator guides.** Retained
  real-bootstrap local installation and staged Kubernetes PoC support an optional
  console. Operator/harness workflows, persistence and configuration limits are
  documented and checked. [Installation evidence](docs/evidence/results/candidate-installation.json).
- [x] **RC-115 — Verify and publish the next integration candidate.** Compatibility,
  migration review, aggregate and console gates, local operator/harness workflow,
  restart and console-disabled API operation passed. Independent AMD64/ARM64
  images and local release bundles are prepared as `0.1.0-rc.2`; OCI images and
  BuildKit attachments are published to Quay. [Exact results and publication scope](docs/evidence/README.md#candidate-rc2).

No required work remains for this scoped candidate. Full execution follows the
[joint AG/AR/gateway proposal](docs/contracts/execution-handoff-proposal.md).
[Production deferrals](docs/adr/0012-integration-rc-qualification-deferrals.md) and
[local signing/provider limits](docs/adr/0016-local-development-policy-promotion.md)
remain unchanged; they are not silently reclassified as passed.
