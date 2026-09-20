# Integration-RC risk review (RC-004)

Review date: 2026-09-19. The reviewed integration scope has no known unresolved
critical/high finding, migration correctness defect, uncovered required-test
skip, or demonstrated fail-open path. This conclusion is limited to the
implemented AG component and recorded tests; it is not a production deployment
audit or a claim that testing proves the absence of vulnerabilities.

## Evidence and disposition

| Area | Evidence / disposition |
|---|---|
| Reachable dependency vulnerabilities | RC-002 `govulncheck -test ./...` reports zero affected-code findings; three module-level advisories are not called by the scanned code |
| Image vulnerabilities | Both explicit AMD64/ARM64 manifests in the [Phase 8 artifact inventory](../homelab/phase8-closeout-results/artifacts.json) have zero HIGH/CRITICAL findings, including unfixed findings; four MEDIUM and two UNKNOWN binary occurrences per architecture remain recorded |
| Security invariants / fail-closed behavior | Fresh [RC-002 gate](verification.md), 27 Rego cases, adversarial API/application tests, real PostgreSQL evidence/approval cases and live tenant/token denial checks; [deployed faults](../homelab/phase-8-closeout.md) cover OPA, database and non-authoritative Valkey behavior |
| Migration / restore | Fresh migration/checksum qualification and real PostgreSQL suites; historical [encrypted WAL/PITR and forward migration](../implementation/phase-8-evidence.md), [SSD preservation/recovery](../homelab/ssd-qualification.md), and [durable receipt replay](../homelab/ssd-evidence-qualification.md); no Phase 9 schema change |
| Required tests / flakes | RC-002 records every initial default-suite skip and its passing integration/race execution; zero unresolved skips or observed test failures in that run; no claim of statistical flake elimination |
| Packaging license | Corrected OCI license label from MIT to the repository's Apache-2.0; existing container contract now checks it. Previously built images retain their historical label; the final RC must be rebuilt |

The image scan database was updated on 2026-09-19. Application sources and
dependencies have not changed since the scanned image. The license-label
correction changes image metadata, not executable contents; final artifacts
still require fresh scanning and exact-source/digest checks under RC-012.

## Remaining bounded risks and qualification

The Phase 7 risk register is historical. Its current dispositions are:

- **T14/T22:** application authorization and bounded-input tests pass; cluster
  isolation/lifecycle evidence exists. Intended-production network enforcement,
  abuse capacity and full load qualification remain deployment work, including
  DQ-001. A homelab result is not proof of another CNI or exposure policy.
- **T20:** pinned builds, SBOMs, checksums and image scans are implemented and
  exercised. Formal RC publication/signature verification has not occurred.
  Unsigned local provenance metadata is not authenticated SLSA provenance.
  Supply-chain finalization remains RC-010/012, not a completed release claim.
- **T16:** managed-signing contract and negative tests exist. No production KMS
  provider or production key rotation is qualified by this review; RC-005 must
  distinguish component game-day evidence from provider-specific operation.
- **T21:** repository backup/PITR, forward migration and retained SSD recovery
  are exercised. Intended-production HA/failure domains remain DQ-004.
- **Dependency advisories:** the recorded MEDIUM/UNKNOWN image findings remain
  visible. Reachability assessment is for this source and tool database; rerun
  scans after dependency, code or database changes and before publishing.

These are not permission to ship a known authorization bypass, lost evidence,
accounting corruption or a critical/high finding. Any newly demonstrated such
failure reopens the gate. AR worker and production gateway scenarios stay in
the owner-approved [deferral register](../../adr/0012-integration-rc-qualification-deferrals.md).

## Verification of this change

The focused container contract and `make verify` passed with PostgreSQL enabled
for all suites and authenticated Valkey configured. All 2,294 test/subtest
executions passed with zero skips; 27/27 policy cases and the rebuilt restricted
container smoke also passed. See [aggregate record](rc004-verification.json).
Changed Markdown links and `git diff --check` passed. This is staged-candidate
verification; the separate clean-checkout evidence remains RC-002.
