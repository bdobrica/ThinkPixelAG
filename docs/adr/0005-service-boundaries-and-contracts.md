# ADR-0005: Modular governance service and explicit contracts

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

AG needs durable authority without absorbing harness execution, tool side
effects, credentials or another component's database. Early independent
microservices would add deployment and distributed-transaction cost before
the governance boundaries stabilize. This record extracts implemented choices
from historical plan sections 2–5; it does not introduce a new platform role.

## Decision

Use a Go modular monolith with domain, application, ports and adapters.
PostgreSQL owns durable governance state; provider/harness/ThinkPixel-specific
behavior stays behind replaceable contracts. HTTP, outbox publishing and
streaming may share a process; independent enablement and fenced ownership
preserve extraction seams. AR owns harness execution and its future worker API.

REST/JSON OpenAPI 3.1 is canonical, with SSE for resumable ordered events.
Versioned wire contracts and stable IDs replace cross-repository internal types
or direct database access. UUIDv7 IDs, UTC injectable clocks, bounded exact
decimal strings, authenticated versioned cursors and closed typed errors are
the shared primitives. Transport maps errors once to safe problem details;
deadlines/cancellation propagate through I/O and shutdown drains within a bound.

## Alternatives considered

A distributed service per module was deferred to avoid premature operational
coupling. Direct peer database access would bypass authority ownership.
Harness-specific domain types would prevent replacement. gRPC remains a possible
future transport, not a reason to change domain semantics. Generated transport
types are useful only where they do not leak into the domain.

## Consequences

One deployable is operationally simpler, but module boundaries need continuous
review. Internal service availability does not imply runtime composition: the
current executable does not compose registry/policy administration or an AR
worker. Future integrations need versioned contracts rather than private imports.

## Security impact

Verified identity and policy establish authority. Skills, memory, Workspace
membership, model output and guardrail results cannot enlarge it. Dependencies
are non-authoritative unless explicitly assigned ownership; credentials stay
outside untrusted harness state. ADR-0001–0004 retain database, tenant, identity
and policy decisions without alteration.

## Operational impact

The root Makefile is the developer/CI interface. Strict configuration, bounded
I/O, independent dependency readiness and explicit shutdown support predictable
operation. Contract changes require compatibility review; frozen fingerprints
detect drift but do not prove semantic compatibility.

## References and evidence

- [Alignment](../../ALIGNMENT.md), [architecture](../architecture/system.md)
- [Primitive contract](../contracts/primitives.md), [contract freeze](../contracts/compatibility.md)
- [Historical plan](../evidence/rc/history/PLAN.md), sections 2–5; ENG-001–012 in the [ledger](../evidence/rc/history/TODO.md)
- Commits `5287791`, `8ff8f77`, `72319f1`, `7e03c72`, `562cc4d`, `79d2ac9`
