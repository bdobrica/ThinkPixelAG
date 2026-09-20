# Historical implementation and qualification evidence

This archive preserves dated results and traceability. Start with the
[operations guide](../operations/README.md) for current instructions and the
[changelog](../../CHANGELOG.md) for version scope.

- [Implementation reports](implementation/): original phase reviews, retained
  for provenance rather than used as the documentation structure.
- [Homelab closeout](homelab/phase-8-closeout.md): experiments and machine results,
  including failed and hardware-limited runs.
- [Final RC artifacts](rc/final-artifacts.md): exact-source image identity and
  platform smoke results; [publication status](rc/status.md).
- [RC verification](rc/verification.md), [risk review](rc/risk-review.md) and
  [recovery scope](rc/game-days.md).
- [Frozen planning snapshots](rc/history/README.md): byte-preserved historical
  PLAN/TODO and their original checksum manifest.

Machine results and frozen planning snapshots retain original bytes, including
historical paths and commands. Resolve those paths in their recorded source
revision; do not treat them as instructions against the current tree. Markdown
report links outside the frozen snapshots have been rebased for navigation.
Decisions live in [ADRs](../adr/README.md), not in this archive.
