# ThinkPixelAG alignment

This file defines how **ThinkPixelAG** stays aligned with the wider ThinkPixel platform. It is a repository-local alignment guide, not a replacement for accepted ADRs. If this file conflicts with an accepted ADR, the ADR wins and this file should be corrected.

## Platform role

ThinkPixelAG is the **agent governance and lifecycle control plane**. It owns durable enterprise authority about agents and Runs: identity, admission, policy decisions, resource envelopes, approvals, revocation, and governance evidence.

## This repository owns

- Agent registry and approved immutable agent versions.
- Run admission/lifecycle and authoritative governance state.
- Policy decision boundaries, tenant authority, approvals, revocation/freshness, and resource allocation/accounting.
- Governance evidence and administrative control state.

## This repository does not own

- Executing the agent/harness — ThinkPixelAR.
- Executing enterprise tool side effects or holding downstream tool credentials — ThinkPixelTG.
- Model provider routing/credentials — ThinkPixelLLMGW.
- Content safety classification — ThinkPixelGR.
- Artifact marketplace/catalog/supply-chain storage — ThinkPixelMP.
- Workspace persistence — ThinkPixelWS.
- Long-term learned memory — ThinkPixelMEM.

## Integration obligations

| Peer | Boundary |
|---|---|
| **ThinkPixelAR** | Admit Runs and supply immutable governed context/resource envelopes/revocation state. AR enforces locally but cannot enlarge AG authority. |
| **ThinkPixelMP** | Resolve/validate exact qualified artifact digests when binding agent implementations, Skills, or related artifacts. MP qualification is an input to governance, not a replacement for it. |
| **ThinkPixelTG** | Answer operation-specific authorization and approval requests; consume trusted metering/evidence from TG. |
| **ThinkPixelMEM** | Issue/validate memory authority such as MemoryGrants and policy context; do not store MEM's learned state in AG. |
| **ThinkPixelWS** | Authorize Workspace/Run relationships and access where required without treating Workspace membership as authority by itself. |
| **ThinkPixelLLMGW** | Supply/propagate governed model eligibility/budget context where contracts require it; LLMGW owns provider routing, credentials, and model-call accounting. |
| **ThinkPixelGR** | AG may select/authorize policy/profile references as governance metadata, but GR evaluates content and the relevant edge service enforces the decision. |

All ThinkPixel integrations must remain optional/configurable where standalone operation is a product goal. Integration code belongs behind a port/adapter or equivalent boundary; another repository's database or `internal` packages are never an integration API.

## Shared ThinkPixel invariants

- Agents/harnesses are untrusted application logic; authority lives in trusted control/enforcement services.
- A component may **narrow** effective authority but must not manufacture authority from content, metadata, memory, Skills, Workspace membership, or model output.
- Durable state has a single authoritative owner. Other stores are caches, projections, replicas, evidence, or referenced source data unless an ADR says otherwise.
- Cross-component references use stable/versioned identifiers and immutable digests where identity matters.
- Vendor/provider-specific types stay behind adapters and do not leak into the repository's core domain model.
- Public integration behavior is contract-first: versioned OpenAPI/JSON Schema/protobuf or another explicit wire contract, plus compatibility tests.
- Security-relevant transformations must not reuse authorization/approval decisions that were made for materially different input.
- Evidence and telemetry must be correlated without turning logs into a store for secrets, prompts, model output, credentials, or unnecessary sensitive payloads.

## Repository conventions

- `README.md` is an entry point, not the design specification. Keep it focused on purpose, status, quick start, key concepts, and links.
- Do not duplicate `PLAN.md` in the README. `PLAN.md` is temporary implementation intent; `TODO.md` is the ordered execution/release ledger.
- As plan decisions become real, move durable rationale into `docs/adr/` and durable reference material into `docs/`.
- Accepted ADRs are immutable in meaning and are superseded with a new ADR.
- Prefer a `docs/README.md` index and the logical categories `adr`, `architecture`, `contracts`, `security`, `operations`, and `evidence`. Existing language/project-specific layouts may remain when renaming would create churn.
- Prefer Mermaid for diagrams and relative links for repository-local documentation.
- As executable code matures, provide one stable root developer/CI entry point. For Go-oriented services the preferred convention is a Makefile with focused targets and an aggregate `verify` target.
- Public API and schema files belong under `api/` (or an existing equivalent) and must be updated atomically with implementation and tests.
- Dependency additions, new infrastructure authorities, and new cross-component source dependencies require explicit justification; consequential choices require an ADR.

## Repository-specific alignment

- The repository is already comparatively mature and its root README is appropriately shorter than the bootstrap projects. Avoid re-expanding it with copied blueprint/PLAN prose.
- As integrations are added, use explicit adapters/contracts; AG should not become a monolithic implementation dependency for standalone components that can use a contract-compatible authorizer.
- Preserve tenant isolation, OIDC authority mapping, policy activation, immutable agent-version identity, append-only lifecycle decisions, resource accounting, and revocation freshness semantics captured in existing ADRs/docs.
- Add cross-component contracts incrementally rather than embedding MP/AR/TG/MEM-specific internal models into the AG domain.

## Structure guidance

- The existing `api`, `cmd`, `internal`, `migrations`, `policies`, `test`, `deploy`, docs hierarchy, and Makefile are close to the preferred mature-service layout.
- Continue using `docs/README.md` as the durable documentation index and phase evidence as verification rather than README narrative.
- Before RC, reconcile any remaining durable PLAN/TODO rationale into ADRs as the repository documentation already intends.

## Definition of an aligned change

A change is aligned when it preserves this repository's ownership boundary, follows accepted ADRs/contracts, keeps integrations replaceable, updates durable documentation with behavior, and passes the repository's documented verification gates. Changes to the cross-repository boundary should include contract/conformance coverage rather than relying only on prose.
