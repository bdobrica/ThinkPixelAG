# Integration-RC security qualification

The `0.1.0-rc.1` review is scoped to the implemented AG component. The
[dated qualification evidence](../evidence/README.md) records its source,
images, test scope and limitations. Passing tests do not establish the absence
of vulnerabilities or qualify a different production deployment.

| Area | Disposition |
|---|---|
| Dependency/image findings | Final 2026-09-19 platform scans: zero HIGH/CRITICAL, four MEDIUM and two UNKNOWN occurrences per architecture; details in the [inventory](../evidence/results/artifacts.json). Reachable-code scanning in the RC gate reported no affected-code findings; refresh scans before publication. |
| Authorization and tenant isolation | Repository, policy, adversarial and runtime denial checks passed in the recorded scope. Runtime tenant isolation uses repository predicates, not PostgreSQL RLS. |
| Network isolation and overload (T14/T22) | Restricted deployment checks exist. Qualify the selected production CNI, exposure policy, abuse limits and workload; homelab results do not establish these. |
| Supply chain (T20) | Pinned artifacts, SBOMs, scans and checksums exist. OCI license metadata is Apache-2.0. No formal release or authenticated signing/attestation ceremony was completed. Unsigned metadata is not SLSA verification. |
| Managed signing and identity (T16) | Component guard/rotation and approval tests use synthetic providers/identities. The selected production KMS/HSM and IdP ceremonies remain unqualified. |
| Recovery (T21) | Database, receipt replay and component recovery evidence exists. The SSD services share a host; process recovery is not host/zone HA. Rehearse backup/PITR in the intended environment. |

[ADR-0012](../adr/0012-integration-rc-qualification-deferrals.md) owns the
accepted capacity and cross-component deferrals and their revisit criteria.
The [dependency policy](dependencies.md) owns dependency exceptions and update
requirements. Neither permits a known authorization bypass, evidence loss,
accounting corruption or unresolved critical/high finding. A newly demonstrated
failure reopens the release gate.
