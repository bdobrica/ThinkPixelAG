# Integration-RC security qualification

The `0.1.0-rc.2` review is scoped to implemented AG and its optional console. The
[dated qualification evidence](../evidence/README.md) records its source,
images, test scope and limitations. Passing tests do not establish the absence
of vulnerabilities or qualify a different production deployment.

| Area | Disposition |
|---|---|
| Dependency/image findings | Final 2026-09-20 platform scans: zero HIGH/CRITICAL; AG has six MEDIUM and three UNKNOWN occurrences per architecture, and the console has none. Details are in the [rc.2 inventory](../evidence/results/integration-candidate-rc2.json); the [rc.1 inventory](../evidence/results/artifacts.json) is historical. Reachable-code scanning in the RC gate reported no affected-code findings; refresh scans before publication. |
| Authorization and tenant isolation | Repository, policy, adversarial and runtime denial checks passed in the recorded scope. Runtime tenant isolation uses repository predicates, not PostgreSQL RLS. |
| Network isolation and overload (T14/T22) | Restricted deployment checks exist. Qualify the selected production CNI, exposure policy, abuse limits and workload; homelab results do not establish these. |
| Supply chain (T20) | Pinned artifacts, SBOMs, scans and checksums exist. AG OCI license metadata is Apache-2.0. No formal release or authenticated signing/attestation ceremony was completed. Unsigned metadata is not SLSA verification. |
| Managed signing and identity (T16) | The real local-development walkthrough uses software signing and two distinct OIDC-authenticated development identities under ADR-0016. Same-host selectable identities do not qualify independent enterprise approval/key custody. Production KMS/HSM and IdP ceremonies remain unqualified. |
| Optional console | OIDC/PKCE, CSRF, bounded requests, escaped policy source and denied/stale change checks passed. The BFF forwards caller authority and holds transient sessions; it has no database/signing-key access. One replica and local provider qualification only. |
| Recovery (T21) | Database, receipt replay and component recovery evidence exists. The SSD services share a host; process recovery is not host/zone HA. Rehearse backup/PITR in the intended environment. |

[ADR-0012](../adr/0012-integration-rc-qualification-deferrals.md) owns the
accepted capacity and cross-component deferrals and their revisit criteria.
The [dependency policy](dependencies.md) owns dependency exceptions and update
requirements. Neither permits a known authorization bypass, evidence loss,
accounting corruption or unresolved critical/high finding. A newly demonstrated
failure reopens the release gate.
