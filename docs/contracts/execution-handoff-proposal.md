# Minimum execution handoff proposal

Status: **proposal for AG/AR/gateway implementation**, not a published wire API.
RC-110 closes the specification task only. The source candidate implements
[guidance](harness-guidance.md) and admission/read/cancel; it does not dispatch an
objective to an AR worker. This proposal follows [ADR-0005](../adr/0005-service-boundaries-and-contracts.md)
and [platform composition](../architecture/platform-composition.md), without
changing ownership or the [qualification deferrals](../adr/0012-integration-rc-qualification-deferrals.md).

## One bounded integration

Start with one configured AR adapter, one existing harness (Codex), one root
Run and one AR Session. The harness contacts AG. AR owns session durability,
harness adaptation and execution behind that boundary. AG owns admission,
authority and authoritative Run state; gateways own enforcement, credentials
and trusted usage. Workspace/tool/model results are data, never new authority.

Proposed flow: AG admits and durably makes the authorized task available to its
AR adapter; AR accepts it idempotently, binds a Session, acquires current Run
execution authority, drives the harness through gateways and reports a fenced
outcome. The harness sees status through AG. Transport paths, schemas and
push-versus-pull delivery remain to be agreed with AR before implementation.
There is no implied `/workers` route or direct harness-to-AR credential exchange.

## Existing contracts and missing work

| Concern | Existing AG basis | Minimum missing work and owner |
|---|---|---|
| Task delivery | Public admission accepts `objective`; Run creation, version snapshot, root grants and evidence commit together | **AG + AR:** define a versioned task envelope and durable delivery/acknowledgment boundary. Current admission does not durably retain objective/input for dispatch. Specify bounded payload or protected reference, digest, access, retention and deletion; atomically associate delivery intent with admission. AR persists the accepted task and deduplicates delivery. Never put task content in guidance or general logs. |
| Run/Session identity | Stable tenant/principal/agent/Run IDs, resolved version digest and trace conventions in [primitives](primitives.md) | **AR:** own Session ID and durable execution state. **AG adapter:** store an authenticated, tenant-bound reference to the AR Session/attempt, not AR's database state. Agree the Run-to-Session cardinality for retry/recovery; first implementation permits one Session per root Run. |
| Claim and authority | Internal Run lease/fencing services; [workload identity](workload-identity.md), [policy decision](policy-decision.md), [resource accounting](resource-accounting.md) | **AG:** publish authorized claim/renew/transition schemas and compose handlers only after joint review. **AR:** consume the versioned contract, bind each attempt to the current lease/fence and stop on lease/freshness failure. A capability revision, Session ID or OIDC caller token is not an execution grant. |
| Gateway invocation | Authority/constraint, signed-artifact and [revocation](revocation.md) semantics exist; trusted revocation distribution is published | **AG + TG/LLMGW:** agree exact action/resource binding, audience, expiry, policy/epoch and lease/fence propagation plus an implemented authorization/assertion exchange. **Gateways:** validate these before dispatch, retain provider/tool credentials and enforce current budgets/freshness. Existing internal authority types do not constitute a complete shared wire surface. |
| Completion and results | Run state machine/events and internal worker transitions | **AR + AG:** define authenticated, idempotent, fenced success/failure reporting with bounded result references and safe error categories. AG validates and commits the authoritative transition/evidence. Define uncertain-response replay and stale-attempt rejection. AR owns detailed output/session history; AG exposes an authorized result reference when the contract exists. Model text saying “done” cannot complete a Run. |
| Cancellation and signals | Public cancel/signal and Run event APIs; lease fencing/revocation semantics | **AG + AR:** specify reliable delivery/reconciliation of cancellation and supported signals, including disconnected attempts. **AR/gateways:** stop further governed work on cancellation, expiry or revocation; late outcomes cannot resurrect a terminal Run. Record whether work had already dispatched without promising reversal of completed side effects. |
| Trusted usage and settlement | Published `POST /v1/trusted/runs/{run_id}/usage` and `POST /v1/trusted/reservations/{reservation_id}/settle`; exact resource units and replay semantics | **TG/LLMGW and applicable trusted runtime meter:** implement authenticated producers against these existing APIs, durable retry and stable source identities. **AG:** use existing accounting/reconciliation. Specify attempt/operation correlation and completion-versus-late-usage ordering. Harness self-reports are not trusted usage. |

The normative [OpenAPI](../../api/openapi/thinkpixelag.yaml) is the source for
implemented routes. Trusted lifecycle role names alone do not mean claim or
completion handlers are published. No component reads another's database or
imports its private implementation types. Existing mTLS service bindings and
closed roles apply; any new authority decision needs an ADR and compatibility
review before code is merged.

## Implementation checklist for the joint integration

1. **AG + AR:** agree the versioned envelope, identities, bounded protected task
   storage, delivery acknowledgment and recovery/replay semantics. Keep caller
   admission compatible; persist task delivery intent without losing admitted work.
2. **AG:** expose reviewed execution lifecycle contracts through a trusted adapter,
   with policy authorization, tenant binding, lease fencing, exact-artifact
   context and transaction-bound evidence. Do not expose internal Go services.
3. **AR:** implement the single-Session Codex adapter and task acceptance replay;
   retain durable session/results in AR and use AG for governance context.
4. **TG/LLMGW + AG:** implement and verify the missing authority exchange and live
   gateway producers using the existing usage/revocation contracts. Restrict
   the execution environment so instructions are not the only enforcement.
5. **AG + AR:** implement completion/cancel reconciliation and authorized result
   references; document unknown outcomes, bounded retries and operator recovery.
6. **All participating owners:** run the acceptance scenario below against real
   components. Only then advertise execution readiness in discovery, with an
   explicit compatible contract update and deployment capability check.

## First real execution acceptance scenario

An authenticated user asks Codex, through AG, to summarize a synthetic incident
using one authorized read-only tool and one model call. Admit an objective-only
Run and retain its authoritative limits. AR receives that exact task and creates
one durable Session; a repeated delivery does not create a second execution.
Codex uses TG/LLMGW with current Run authority, while credentials stay at gateways.
Trusted producers record actual usage exactly once despite a replay. AR submits
a fenced outcome; AG exposes the terminal Run and authorized result reference.
Observe the real summary, Session correlation, accounting and committed evidence.

Repeat with cancellation during execution: reconcile the cancellation, deny
further dispatch, and reject a stale completion/lease attempt. Also attempt one
policy-denied tool action and prove the gateway does not execute it, regardless
of the harness's instructions. Check that restart/retry of the single task
preserves its identity and does not duplicate delivery or accounting.

This is a small correctness qualification, not a throughput, HA or broad chaos
campaign. It remains **pending** until the real AR/gateway integration exists;
the RC-109 manual governance walkthrough is separate evidence.
