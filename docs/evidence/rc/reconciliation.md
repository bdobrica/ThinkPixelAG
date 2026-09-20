# Release checklist reconciliation (RC-006)

The 2026-09-19 audit at `85ba0e7` inventories all **126 unique checklist IDs**:
119 completed in their recorded scope and seven remaining release-sequencing
items, RC-006–012. The [per-item audit](checklist-audit.json) preserves each
entry's implementation/verification description, resolving full commit IDs and
existing phase evidence. Every completed item has both a commit reference and
an evidence document. This is traceability checking, not a claim that every
historical deployment test was rerun on the latest image.

The implementation evidence is layered: contracts/ADRs describe authority,
phase reports describe their original implementation and tests, RC-002/004/005
provide current aggregate/recovery verification, and Phase 8 records actual
cluster/storage/artifact qualification. The internal worker, signing, approval
and registry services do not by themselves establish a deployed AR integration,
production KMS/IdP ceremony, or complete operational administration surface.
The executable's mounted routes remain the source of runtime composition
evidence; component coverage is explicitly distinguished in the game-day record.

## Resolved discrepancies

- The historical SEC-006 progress row's commit placeholder is resolved to
  `3dca78d`, the evidence-export implementation in this branch's history.
- RC progress rows are part of the existing progress table and completed
  predecessor commits are recorded, rather than leaving dangling placeholders.
- Old Phase 8 failure/blocked statements remain dated evidence, with the current
  integration closeout and deferral register taking precedence for release scope.
  Admission atomicity/concurrency and pending evidence replay fixes remain
  linked to their actual commits; performance misses are not erased.
- The OCI license mismatch is fixed in RC-004. Existing registry images retain
  their historical metadata; RC-012 must rebuild from the final source.
- The root plan's quality gate is reconciled with the owner's integration scope.
  Production capacity, AR/gateway composition and production HA remain deferred,
  while security/correctness invariants continue to block on any known failure.

## Remaining work and tracked limitations

| Item | Disposition |
|---|---|
| Production performance / composed worker and gateway / HA | DQ-001–004 in the [deferral register](../../adr/0012-integration-rc-qualification-deferrals.md); accepted for this integration RC, never marked passed |
| Production KMS and IdP ceremonies | Unqualified provider-specific deployment work; component scope and runbooks in [game days](game-days.md) |
| Dependency pseudo-versions | Exact, reviewed exceptions in `dependency-policy.json`; expiry dates remain 2027-02-10 and 2027-02-19; current dependency gate passes |
| Medium/unknown advisories | Explicit [risk review](risk-review.md); no known critical/high or reachable affected-code finding in the recorded scans |
| README/support matrix | RC-007 must replace stale Kubernetes/ARM64 placeholders using actual observed evidence; retained server reports `v1.36.4+k3s1` on ARM64 |
| Durable decisions and history | RC-008/009 must extract and audit before planning files are removed |
| Release notes, checklist and inventory | RC-010; proposed version is not yet a published release |
| Final source and artifacts | RC-011/012; current Phase 8 images are not final frozen-contract artifacts; exact-source builds, fresh scans, digests and provenance still required |

No unresolved implementation failure was discovered by this reconciliation.
Completing the ledger audit does not mark those later release steps complete.
Formal publication/signature execution remains explicit in the artifact record;
local unsigned metadata cannot be advertised as verified signing provenance.
