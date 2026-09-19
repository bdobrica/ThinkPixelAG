# Decision and history preservation review (RC-009)

Reviewed 2026-09-19 against the exact `5acce19` snapshots. The four previously
accepted ADRs are byte-for-byte unchanged. The historical plan, all 126 checklist
IDs, completion descriptions, progress rows, commit references and deviations
are preserved exactly, with source commit and hashes. New ADRs capture durable
decisions rather than importing obsolete sequencing as current architecture.
See [machine checks](preservation-audit.json) and [history](history/README.md).

## Coverage map

| Historical plan material | Durable destination |
|---|---|
| §1 purpose, §2 ownership | ALIGNMENT.md; ADR-0005; README integration scope |
| §3.1 service shape | ADR-0005; system architecture and platform composition |
| §3.2 PostgreSQL, tenant isolation, OPA, cache | ADR-0001/0002/0004/0007; database schema and policy lifecycle |
| §3.3 interfaces and trust | ADR-0003/0005/0010; OpenAPI and workload identity |
| §3.4 primitives and entities | ADR-0005/0006/0007; primitive/domain/resource contracts |
| §3.5 Run/version state machines | ADR-0006; domain contract, lifecycle examples and Phase 4 evidence |
| §3.6 allocation equations and replay | ADR-0007; resource accounting and Phase 5 evidence |
| §3.7 revocation scopes, epochs, freshness | ADR-0008; revocation contract and Phase 6 evidence |
| §3.8 typed policy and denial | ADR-0004/0008; policy contract and frozen-boundary evidence |
| §3.9 outbox, delivery and telemetry | ADR-0009; evidence schemas, data classification, observability and SSD reports |
| §4 Go structure, dependencies, I/O, developer gates | ADR-0005/0011; development guide, dependency policy and AGENTS.md |
| §5 public/trusted/admin APIs | ADR-0005/0006/0010; OpenAPI, runtime configuration and explicit composition limits |
| §6 database/migration constraints | ADR-0001/0002/0006/0007/0008/0009; schema and recovery evidence |
| §7 restricted deployment | ADR-0011; deployment assets, runbooks and supported-version matrix |
| §8 tests | ADR-0011; Make targets and RC-002/004/005 aggregate/fuzz/live evidence |
| §9 phase exits | Original snapshots, per-item checklist audit and Phase 0–8 evidence reports |
| §10 coding/commit rules | AGENTS.md and development guide; historical sequencing retained only as history |
| §11 transition and §12 release gates | ADR-0011; contract freeze, risk/capacity/game-day records; RC-010–012 remain current release work |
| §13 initial risks | ADR-0004 authorization drift; ADR-0007 allocation races; ADR-0008 staleness; ADR-0002 tenant leakage; ADR-0010 governance compromise; ADR-0011 evidence/plan maintenance |

## Rationale, alternatives and consequences

ADRs retain the reasons for authoritative PostgreSQL, repository tenant scoping,
bounded OIDC/policy freshness, append-only activation, exact resource arithmetic,
transactional replay and evidence, version-bound keys and narrow emergency
authority. Alternatives explain why header trust, unbounded cached ALLOW,
unfenced ownership, floating-point/partial allocation and best-effort evidence
do not satisfy those invariants. Consequences retain database contention,
availability loss during governance faults, provider integration cost and
operational verification responsibilities.

The ADRs dated 2026-09-19 are retrospective records and current evaluation of
alternatives, not invented contemporaneous meeting minutes. Their source and
implementation references permit checking the original history. Existing
accepted decisions were not silently rewritten to match a test or deployment.

Initial names/examples and phase sequencing are not promoted over final
contracts: for example, the implementation uses `internal/application` and
`cmd/thinkpixelag-migrate`; runtime administration/AR composition is explicitly
limited. The final policy contract takes precedence over the plan's abbreviated
JSON illustration. Historical failed capacity samples remain failed, and the
owner-approved deferrals remain separate from security/correctness gates.

The snapshot is immutable evidence, not a replacement living TODO. Current
release tasks remain in the root ledger until RC-010 supplies the durable
release checklist and RC-011 validates removal. Final artifact identity and
publication/signing status must still be recorded under RC-012.
