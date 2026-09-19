# Root planning-file retirement (RC-011)

The root `PLAN.md` and `TODO.md` are retired after contract freeze, verification,
scope/risk reconciliation, ADR extraction and preservation review. Their exact
historical snapshots remain in [history](history/README.md); Phase 9's subsequent
completion records and remaining artifact task are in [release status](status.md).
The snapshot's pending entries are historical, not new blockers or instructions.

ADR-0001–0003 receive reference-link repairs only, directing the former root
file references to the preserved snapshots. Their accepted decision bodies are
unchanged. ADR-0004 is unchanged, and ADR-0005–0011 record the extracted decisions.
The root README and documentation index now point to the durable release record.

The implemented scope, production targets, qualification deferrals, dependency
exception expiry dates and medium/provider risks survive the transition through
the [reconciliation](reconciliation.md), [risk](risk-review.md),
[capacity](capacity-envelope.md) and [game-day](game-days.md) records.
AGENTS.md remains the repository contribution authority; its conditional guidance
about planning files does not require recreating retired files.

The commit containing this transition is the source to use for RC-012's final
multiarchitecture build. The artifact record will identify its exact SHA and
digests; later evidence-only commits must not be confused with the image source.

Verification passed on 2026-09-19: 89 tracked Markdown files and 356 local link
targets, snapshot hashes and accepted decision bodies, `git diff --check`, and
`make verify` in the resulting tree. All 2,303 test/subtest executions and 27
policy cases passed with zero skips; the rebuilt restricted container smoke
also passed. See [aggregate evidence](rc011-verification.json).
