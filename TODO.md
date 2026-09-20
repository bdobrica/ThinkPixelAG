# Next RC implementation checklist

Scope and design defaults: [PLAN.md](PLAN.md). Checked items are complete; unchecked items remain pending.
Each numbered item is a coherent commit boundary; split only when review size
requires it. Implement contracts with handlers and tests, not as unsupported
API promises. Keep accepted ADRs and existing security contracts authoritative.

## 1. Decisions and prerequisite correctness

- [x] **RC-101 — Record the implementation decisions.** Refine proposed ADR-0013
  for static bootstrap plus dynamic retrieval. Add a decision for the optional
  console/BFF and an ADR for dynamic role mappings/configuration ownership,
  explicitly superseding affected parts of ADR-0003/0010 if necessary. Specify
  management authorization, tenant scope, file/API mode, refresh and lockout
  recovery. Select one signing/approval-provider path for the RC walkthrough;
  identify any credentials or external setup needed before promotion work.
  Update ALIGNMENT.md only for changed boundaries. Done: decisions distinguish
  accepted behavior, proposed changes and external dependencies without claiming
  that new endpoints exist. Completed: ADR-0013–0016 and ALIGNMENT.md; owner
  selected local-development signing and OIDC approvals. Checked doc links
  and architectural/contract consistency; no runtime tests for this decision item.

- [x] **RC-102 — Fix authoritative constraint inheritance.** Trace HTTP admission,
  version resolution, OPA validation, caches and root issuance. Normalize absent
  caller constraints, resolve all applicable authoritative ceilings, preserve
  independent Go non-expansion checks and define handling of missing authority.
  Update Rego and the objective-only example. Done: omitted/partial constraints
  preserve deadlines and resource grants; stricter caller bounds work and larger
  bounds never expand authority. Check: focused Go/Rego tests for omission,
  malformed/partial policy results and cached results, plus persisted admission
  integration coverage. Completed: approved/deployment/caller/policy limit
  intersection, OPA/cache inheritance and exact numbers, persisted root grants,
  and objective-only source example. Focused tests, 29 Rego cases and
  `make verify` passed; reviewed contract fingerprints regenerated.
  [Verification](docs/evidence/results/admission-constraint-inheritance.json).

## 2. Administration API before UI

- [x] **RC-103 — Compose signed policy upload and activation.** Depends on RC-101.
  Reuse existing bundle/persistence services and published upload/activation
  schemas. Wire authenticated handlers, action authorization, validation/signing
  adapters and runtime dependencies. Reconcile the actual OPA bundle with active
  metadata across replicas and restart; fail closed on mismatch. Preserve
  idempotency and transaction-bound evidence. Done: a signed artifact can be
  uploaded and activated using HTTP, with a subsequent decision demonstrably
  using that digest/version. Check: invalid signatures, incompatible artifacts,
  denied tenants/roles, retries and failed OPA load/activation recovery.
  Completed: opt-in local signed upload/activation, per-artifact OPA selection,
  transactional replay/evidence and schema 19 immutability. Focused Go and
  PostgreSQL/OPA HTTP integration checks passed. Editor/approval APIs follow in RC-104.

- [x] **RC-104 — Add policy editor support and rollback.** Depends on RC-103.
  Specify and implement tenant-scoped list/detail/source, draft validation and
  activation-history operations with bounded payloads and pagination where
  needed. Persist revisioned drafts; editing creates a new revision and cannot
  mutate active artifacts. Provide exact-digest promotion and approval status/
  submission workflow required by rollback, using a verified provider rather
  than free-form approval references. Done: CLI can edit, validate, promote,
  activate and roll back; rollback appends history. Check: stale edits, invalid
  Rego, requester/approver separation, expired/replayed approvals, source access
  isolation and unchanged active policy on failure. Completed: additive editor/
  history APIs, immutable drafts and authenticated local approval receipts
  (schema 20), exact promotion and transactional rollback. HTTP/PostgreSQL/OPA
  workflow, expiry/replay/isolation checks, focused tests and OpenAPI lint passed.

- [x] **RC-105 — Implement managed external role mappings.** Depends on RC-101.
  Add tenant-scoped revisioned storage, read/update API and the selected narrow
  authorization rule. Preserve the closed internal roles and explicit OIDC
  verification. Resolve mappings under the verified tenant before authorization;
  define bounded cache invalidation and existing-token behavior on removal.
  Enforce file-managed read-only mode and safe administrator recovery. Done:
  allowed operators can change mappings without restarting API-managed instances;
  no IdP user/group CRUD or custom role hierarchy is introduced. Check: self-
  escalation, service-role assignment, cross-tenant claims, concurrent updates,
  last-admin lockout and removed-role denial across replicas. Implemented live
  verified-tenant mappings, schema 21 revisions, file-mode rejection and atomic
  approval-bound edits. OIDC replica/removal and PostgreSQL approval/conflict/
  isolation checks passed. Protected provisioning/recovery commands follow in RC-107.

- [x] **RC-106 — Implement bounded integration configuration and status.** Depends
  on RC-101. Inventory actually supported adapters; publish an explicit field/
  capability catalog and versioned read/update/status contracts. Store API-managed
  revisions and secret references; expose file-managed settings as read-only.
  Validate allowed destinations/protocols, bound connectivity checks and prevent
  arbitrary URL probing or secret exposure. Define safe reload versus restart
  requirements and keep last valid configuration on failed updates. Done:
  configure and check at least one real supported integration; unavailable peer
  contracts remain marked unsupported. Check: invalid endpoints/references,
  unauthorized edits, revision conflicts, refresh, restart and redacted status. Implemented OPA
  field catalog and APIs, schema 22, live reload, protected token aliases and
  active-artifact checks before commit. Transport-boundary and real OPA/database
  validation/replay/failed-update tests passed; provisioning follows in RC-107.

- [x] **RC-107 — Provide operator bootstrap and read views.** Depends on RC-103,
  RC-105 and RC-106. Add a documented protected provisioning command for the
  first tenant/admin/policy and sample approved agent using application services.
  Reuse agent/version contracts where possible; compose missing minimum registry
  operations rather than requiring SQL edits. Add only authorized read views
  needed for console agents/Runs, policy and integration status. Done: a clean
  installation is usable through commands without the UI or fixture seeding.
  Check: provisioning replay, unauthorized rebootstrap, tenant isolation,
  persistence after restart and no credentials in output/evidence. Implemented
  protected local bootstrap/recovery command, schema 23 receipts, published
  registry write composition and per-object-authorized Run listing. Real
  PostgreSQL/OPA command replay/reopen, recovery approval/evidence, HTTP registry
  isolation/replay and Run-list filtering/cursor checks passed. Section-wide
  `make verify` passed; final operator-audit clarification passed focused checks.
  [Verification](docs/evidence/results/administration-api.json).

## 3. Dynamic harness guidance

- [x] **RC-108 — Implement scoped capability/instruction discovery.** Depends on
  RC-101, RC-102 and RC-106. Define OpenAPI/schema and compatibility behavior for
  authenticated structured capabilities plus Markdown, revision/ETag, expiry
  and optional Run scope. Compose trusted templates from effective configuration
  and supported adapters. Specify presence/readiness versus permission; validate
  current authorization even on conditional requests. Done: responses reflect
  real supported state without revealing another tenant's context or secrets.
  Check: context isolation, unsupported versions, removal/expiry, conditional
  refresh, injected descriptions and bounded responses. Update contract guide.
  Implemented v1 JSON/Markdown routes, live caller/Run checks, revision/expiry
  and conditional reauthorization; focused HTTP/application/contract checks passed.

- [x] **RC-109 — Ship the bootstrap snippet and fetch helper.** Depends on RC-108.
  Provide an appendable AGENTS.md section and helper installation/configuration
  commands. Keep tokens in trusted host storage; return only safe instructions
  to the harness. Pin the AG origin, reject unsafe redirects, refresh at session
  setup/Run changes/expiry and explain unavailable guidance. Do not overwrite
  user instructions. Done: real Codex session retrieves guidance and performs
  supported admission/read/cancel operations; configuration change triggers a
  refresh. Check helper behavior plus one recorded manual harness walkthrough,
  including expiry and a denied operation. Do not label this AR execution.
  Implemented private host helper, appendable snippet and installation guide.
  `make verify`, seven focused helper tests and the real Codex admission/read/cancel, configuration
  revision, expiry and existing-token role-removal walkthrough passed.
  [Verification](docs/evidence/results/harness-guidance.json).

- [x] **RC-110 — Specify the remaining execution handoff.** Depends on RC-109.
  Record the minimum AG-facing runtime integration needs: task delivery, Run/
  Session identity, authority, completion, cancellation and trusted usage. Map
  these to existing contracts and enumerate genuinely missing AR/gateway wire
  surfaces. Done: a short integration contract proposal/checklist identifies
  the owning component for each gap and the first real execution acceptance
  scenario. Do not invent an internal worker API or block guidance delivery on
  building ThinkPixelAR. Full execution remains separately labeled pending.
  Recorded the [joint integration proposal](docs/contracts/execution-handoff-proposal.md),
  owner-by-owner gaps, bounded implementation sequence and real execution
  acceptance scenario. Reviewed against existing ownership/wire contracts and
  checked local links; no runtime change or execution qualification claimed.

## 4. Optional console

- [x] **RC-111 — Build the isolated console/BFF shell.** Depends on RC-101 and
  RC-107. Add `console/` with pinned FastAPI/Jinja2 dependencies, separate image,
  OIDC code/PKCE login, protected transient sessions, logout and an AG HTTP client.
  Forward the verified caller token, never an admin service identity. Add CSRF,
  origin/redirect restrictions, escaped templates, request bounds and redacted
  errors. Done: authenticate and show authorized read views; AG builds/runs with
  no console dependencies. Check login/state/nonce/session failure cases, denied
  API requests, CSRF and console shutdown while AG remains usable.
  Implemented the independent FastAPI/Jinja2 shell with verified OIDC/PKCE,
  transient caller sessions, bounded transport and authorized read views.
  Eight focused tests, real Chromium login/list/logout, independent image and
  console-stop/AG-available checks passed. [Evidence](docs/evidence/results/optional-console.json).

- [x] **RC-112 — Add policy editing and promotion screens.** Depends on RC-104
  and RC-111. Implement source editor, validation feedback, draft diff/history,
  promotion, activation and approval-aware rollback using only AG APIs. Display
  exact digest/revision and distinguish draft/validated/active status. Done:
  an operator completes the same policy workflow available through CLI. Check
  one browser success flow plus invalid Rego, stale edits, unauthorized writes,
  unsafe source rendering and approval failure. No private signing keys in BFF.
  Implemented escaped editor/diff, exact reviewed requests, separate validation/
  promotion/activation and independent approval screens. Thirteen focused tests
  and real Chromium edit-to-rollback, invalid/stale/denied checks passed.

- [ ] **RC-113 — Add mappings and integration screens.** Depends on RC-105,
  RC-106 and RC-111. Provide bounded forms, current revision, change review,
  read-only file-managed state, secret-reference fields and connection status.
  Done: configure supported settings through UI and observe the corresponding
  API/discovery change; configured does not imply healthy or authorized. Check
  revision conflict, forbidden edit, reference redaction and unavailable peer.

## 5. Package and close the scoped candidate

- [ ] **RC-114 — Update runnable installations and operator guides.** Depends on
  RC-107, RC-109, RC-112 and RC-113. Add optional console deployment to local and
  team-PoC examples, real bootstrap commands and a policy/role/integration
  walkthrough. Document identity setup, secret references, persistence, recovery
  and remaining execution limitations. Done: a reader can install AG alone or
  with UI and connect the harness without reading tests/Makefiles. Check the
  documented commands on a retained installation; do not repeatedly tear down
  resources or commit homelab/private configuration.

- [ ] **RC-115 — Verify and publish the next integration candidate.** Depends on
  RC-102 through RC-114. Review API compatibility and migrations, regenerate
  affected artifacts through documented generators, and run focused failures
  plus the final `make verify` and console gate. Perform one end-to-end operator/
  harness-guidance walkthrough with the ADR-0016 local signing/approval path, restart and
  UI-disabled API operation. Record exact versions, results and external gaps;
  never count mocks as live-provider qualification. Update CHANGELOG.md and
  concise release evidence, then prepare versioned artifacts under the existing
  release procedure. Keep throughput/p99/production-HA deferrals unchanged.
  Done: all in-scope gates pass; any unverified required workflow leaves this
  item open rather than being silently deferred. Review each diff for secrets
  and commit each completed coherent item with `feat`, `fix`, `docs`, `build`
  or `ci`; retire completed planning entries when appropriate.
