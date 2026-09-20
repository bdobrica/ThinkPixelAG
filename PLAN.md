# Next integration release candidate

Status: implementation in progress; RC-101 decisions and RC-102 constraint
inheritance are implemented. Section 2 administration APIs, managed configuration
and protected operator bootstrap are implemented in the local profile. Harness
discovery and the helper walkthrough are implemented. The execution-handoff
proposal is recorded; its implementation remains pending. The optional console
shell is implemented; policy/configuration screens are next.
Execution checklist: [TODO.md](TODO.md). Scope: dynamic harness guidance and
usable administration through APIs and an optional UI.

## Outcome and boundaries

An operator can install AG, configure supported integrations and external role
mappings, edit and promote policy, and inspect governance state without database
edits. An existing harness can use a small static AGENTS.md bootstrap to obtain
current, authenticated platform instructions through AG.

AG remains the harness-facing governance entry point. AR owns execution/session
adaptation; TG and LLMGW own their respective enforcement and credentials. The UI
does not own governance data or become necessary for AG startup or operation.

The current executable evaluates policy, admits and manages Runs, and exposes
some administrative operations. Policy upload/activation are composed in the opt-in local-development profile
(RC-103); production managed-key adapters remain a separate prerequisite. Roles support deployment-owned or live tenant-scoped API-managed
OIDC mappings (RC-105); protected first-install provisioning and recovery are implemented in RC-107. Dynamic instructions and their trusted helper are implemented under
[ADR-0013](docs/adr/0013-dynamic-harness-instructions.md). RC-102 corrects omitted
caller/policy limits and applies approved manifest ceilings in root admission;
the pinned `0.1.0-rc.1` image still predates that source fix.

## Planning defaults and decisions

RC-101 records these choices in accepted ADR-0013 through ADR-0016. The
features remain unimplemented until their checklist items close. Those records
partially supersede the older static-mapping/local-signing decisions explicitly.

| Topic | RC choice |
|---|---|
| Optional console | Separate `console/` component and image in this repository; FastAPI with Jinja2 and a small amount of JavaScript. A backend-for-frontend (BFF) talks only to AG's published HTTP APIs. No React build pipeline is needed initially. |
| Console authentication | OIDC authorization-code flow with PKCE; tokens held server-side, browser receives a protected session cookie. AG validates the actual caller token and authorizes each operation. No shared administrator identity or trusted forwarded-role headers. |
| Role ownership | IdP owns users/groups/membership. AG manages explicit mappings to its existing closed internal role catalog. Arbitrary custom roles and IdP administration are deferred. |
| Configuration ownership | Deployment explicitly selects file-managed or API-managed mode per supported configuration class. File-managed values are visible but read-only in the UI/API; no silent merging or precedence surprises. |
| Dynamic configuration | Start with role mappings and a bounded catalog of supported integration connection settings. Issuer/audience, listeners, database, signing trust roots and initial bootstrap authority remain deployment-managed. |
| Integration secrets | Accept protected secret references, not credentials in forms or instruction documents. Credentials are resolved by the owning trusted adapter. |
| Harness bootstrap | Publish an appendable static AGENTS.md snippet plus a trusted fetch helper. Use structured, versioned discovery with a Markdown instruction rendering, retrieved through AG. |
| First real harness | Codex is the first manual interoperability target; keep the core discovery contract harness-neutral. |
| Execution scope | Ship working discovery and existing governance operations. Full AR-backed execution is a separately tracked integration dependency, not silently implied by fetching instructions. |

The owner selected the explicit local-development signing profile and two
OIDC-authenticated operators in [ADR-0016](docs/adr/0016-local-development-policy-promotion.md).
No cloud setup is required. RC-103/104 implement the software-key adapter
and authenticated approval receipts; production KMS/HSM and external-provider
qualification remain separate. [ADR-0015](docs/adr/0015-managed-governance-configuration.md)
selects the OPA decision connection as the first integration setting; RC-106
implements its bounded connection API and validates the active signed artifact
before committing a reloadable revision.

## Workstream 1: authoritative limits and administration APIs

RC-102 makes absent caller constraints inherit applicable authoritative ceilings.
The effective limits are the strictest applicable agent approval, tenant policy,
deployment, parent and optional caller bounds. Go must independently preserve
those bounds even if policy output omits a dimension. Cover deadlines, resource
grants and partial requests; do not merely change the example Rego.

Complete the existing policy upload/activation contracts using the current
signature, validation, append-only activation and evidence mechanisms. Add only
the read/draft/validation/history surfaces needed by the editor. Draft text has
no authority. Validation compiles against the supported decision contract;
promotion signs the exact reviewed artifact; activation is separate. Rollback
appends a new activation and consumes the required digest-bound approval.
Make the active OPA artifact and AG activation metadata converge safely: mixed
versions or failed loading must not authorize requests under the wrong policy.

Add tenant-scoped administration of external-to-internal role mappings and
supported integration settings. Define a narrow configuration-management action
and its authorized existing role in an ADR; do not infer that any administrator
can change identity authority. Mapping changes cannot grant cross-tenant access
or service-only roles to ordinary users. Use revision checks, audit/outbox
evidence, and bounded replica refresh/invalidation. Define effects on existing
tokens and sessions, including role removal, before writing handlers.

Bootstrap must be usable without the console and without hand-editing SQL.
Provide a narrowly scoped operator provisioning command for initial tenant,
administrator mapping, policy and sample approved agent, reusing application
services. Define one-time/replay behavior and deployment-controlled recovery
from accidental administrator lockout; no unauthenticated bootstrap endpoint.

## Workstream 2: dynamic harness guidance

Implement accepted ADR-0013 with the static bootstrap plus trusted helper workflow.
Specify an authenticated versioned response with capability revision, expiry,
caller/tenant scope, optional Run scope, supported operations and Markdown
instructions. Endpoint paths and schemas are finalized in the contract task.
Return only supported, configured capabilities with explicit readiness and
authorization prerequisites; a configured peer URL does not prove integration.

Use trusted templates and validated configuration. Never interpolate arbitrary
peer descriptions, Rego text, model output or secrets as executable instructions.
Validate Run ownership before returning Run context. Keep task inputs and raw
objectives out of discovery. Conditional refresh must respect caller scope and
current authorization rather than treating an old ETag as permission.

The static snippet asks the harness to invoke the installed helper at session
start and when context changes. The helper handles credentials outside model
state and retrieves guidance from the configured AG origin. Refresh after Run
admission, on expiry and on a stale-revision response. On unavailable/expired
guidance, stop dependent platform operations and explain the failure; never
silently bypass AG. Do not overwrite the user's existing AGENTS.md.

A real Codex walkthrough must demonstrate discovery, refresh and permitted AG
operations. It must also show that a denied operation remains denied regardless
of instruction text. This establishes harness guidance interoperability, not
end-to-end execution, accounting or recovery of an undeployed AR worker.

The [execution-handoff proposal](docs/contracts/execution-handoff-proposal.md)
maps the remaining joint AG/AR/gateway work and first real execution acceptance
scenario. It is a proposal, not an implemented worker wire contract. Section 3
is complete; continue with the optional console (RC-111).

## Workstream 3: optional administration console

Use the same APIs available to CLI clients. The BFF holds only transient session
state and presentation logic; it has no AG database, OPA, signing-key or peer
administration access. Build and run AG without Python or the console image.
For this RC, one console replica with expiring in-memory sessions is sufficient;
a console restart requires login again and does not affect governance state.

Provide four small views: platform/integration status; agents and Runs; policy
source/validation/history/activation; external role mappings and integration
configuration. Plain text editing and a readable diff suffice. Show configured,
healthy, unavailable and unsupported states accurately; avoid a universal
platform dashboard that invents peer telemetry.

All writes use AG authorization and revision checks. The BFF uses an allowlisted
AG origin, bounded requests, CSRF protection, safe template escaping, secure
session handling and redacted logs. Policy text is never executed by the console
or rendered as trusted HTML. Destructive authority changes and rollback present
the exact pending change and required approval state.

## Delivery sequence and RC acceptance

Execute [TODO.md](TODO.md) in dependency order, committing each coherent item.
Keep the API usable before building its UI. Update contracts, compatibility
records and operational documentation with each implemented surface.

The candidate is ready for these two features when:

- Objective-only admission inherits limits, and caller requests cannot widen them.
- An operator can provision a clean installation and manage policy, mappings and
  supported settings through documented commands and the optional console.
- A real local-development policy promotion/activation/rollback walkthrough
  uses the ADR-0016 signing and approval boundaries; its custody limitations
  and separate production-provider qualification are explicit.
- A real harness obtains and refreshes scoped guidance through AG, while
  forbidden/stale operations remain denied by authoritative services.
- Restart preserves AG configuration and policy history; console absence or
  failure does not impair API operation.
- Focused security/contract tests and the final applicable repository gate pass.

Do not reopen deferred production throughput, p99, HA, multi-provider or broad
chaos qualification from [ADR-0012](docs/adr/0012-integration-rc-qualification-deferrals.md).
Custom RBAC, IdP user management, multiple OIDC issuers, arbitrary plugins,
visual policy builders, console HA and full peer orchestration are outside this
candidate. These exclusions do not defer known correctness or authority defects.

## Verification and records

Run focused tests for changed behavior, PostgreSQL tests for persistence and
concurrency, and schema/compatibility checks for changed contracts. At integrated
closeout run `make verify` and the added console checks once, repeating gates
only for changes or unresolved failures. Documentation-only planning needs diff
and link checks, not application or cluster suites.

Keep concise release evidence in the established documentation area; retain
useful resources between live checks. Never commit credentials or
`docs/operations/homelab.md`. Move accepted durable decisions into ADRs and remove
completed checklist material when appropriate rather than growing a new archive.
