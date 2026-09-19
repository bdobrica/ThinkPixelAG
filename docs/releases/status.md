# Integration release status

Candidate: [0.1.0-rc.1](0.1.0-rc.1.md). Phase 9 is complete for the integration scope.
[Final artifacts](final-artifacts.md) are built, scanned and runtime-tested; no
semantic Git tag or formal GitHub release has been published by this work. The candidate is intended to unblock dependent
component development; production targets and accepted deferrals remain explicit.

| Phase 9 item | Status | Evidence / commit |
|---|---|---|
| RC-001 contracts | Complete | [Freeze](contract-freeze.md), `79d2ac9` |
| RC-002 verification | Complete | [Verification](verification.md), `c12a2a1` |
| RC-003 capacity | Complete in integration scope | [Envelope](capacity-envelope.md), `6edba1d` |
| RC-004 risks | Complete in reviewed scope | [Risk review](risk-review.md), `af620d1` |
| RC-005 game days | Complete in integration/component scope | [Game days](game-days.md), `85ba0e7` |
| RC-006 reconciliation | Complete | [Per-item audit](reconciliation.md), `482c034` |
| RC-007 references | Complete | README/support matrix, `5acce19` |
| RC-008 ADR extraction | Complete | [ADR index](../adr/README.md), `28c706b` |
| RC-009 preservation | Complete | [Preservation review](preservation-review.md), `95ee42a` |
| RC-010 release preparation | Complete | [Notes and operator checklist](0.1.0-rc.1.md), `475a495` |
| RC-011 retire temporary planning files | Complete | [Transition](planning-transition.md), [verification](rc011-verification.json), source `a64d323` |
| RC-012 final-source artifacts | Complete | [Exact-source image, inventory and runtime checks](final-artifacts.md); this evidence commit |

The [historical ledger](history/README.md) is immutable evidence, not a current
TODO. This document retains remaining release work after root planning files
are retired. [Qualification deferrals](../operations/deferred-qualification.md)
and [risk dispositions](risk-review.md) remain live references; completion of
the documentation transition does not turn them into passed production tests.
