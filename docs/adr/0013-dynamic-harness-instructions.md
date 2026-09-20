# ADR-0013: AG-provided dynamic harness instructions

- Status: Accepted
- Date: 2026-09-20
- Owners: project maintainers
- Supersedes: none
- Implementation: planned in RC-108/109; no discovery/instruction endpoint exists yet

## Context

The [platform composition](../architecture/platform-composition.md) places AG
between clients/IDEs/automation and the platform runtime. An existing harness
uses AG as its platform entry point; it does not need a direct AR connection.
AR continues to own runtime/session execution and recovery behind the platform's
versioned integration boundaries.

A static AGENTS.md cannot accurately describe an evolving deployment. Available
components, integrations, qualified tools and workflow affordances vary by
deployment and may also depend on the authenticated caller or admitted Run.
The owner requested dynamic guidance and a small static bootstrap appended to
the harness's existing AGENTS.md. The bootstrap locates current guidance; it
does not embed a deployment capability snapshot.

## Decision

AG will expose an authenticated, versioned capability/instruction contract for
harness integrations. A small harness adapter obtains guidance from AG and
presents it through the harness's supported instruction mechanism. A
distributed static AGENTS.md snippet tells the harness to invoke a trusted
fetch helper at session setup. The helper retrieves the versioned structured
capability response and its Markdown instruction rendering through AG over
authenticated HTTPS. It never overwrites existing harness instructions. Other
harness formats remain adapters; none replaces the canonical structured
contract. RC-108 selects the endpoint paths and schemas before implementation.

Capability descriptions originate from deployment-owned configuration and
verified component contracts. AG composes only the capabilities available in
that deployment and appropriate to the requesting context. It does not absorb
AR, gateway, workspace or memory responsibilities, inspect peer databases, or
trust arbitrary marketplace descriptions as authoritative capability claims.

The response identifies its contract version, capability revision and scope
(deployment/caller and, where applicable, Run). Instructions describe how to
use supported operations and their prerequisites. They distinguish a capability
being present from the caller being authorized to invoke it. The ordinary AG
identity, policy, resource and freshness checks still decide every operation.

The adapter refreshes at session setup, after Run admission/context changes,
and when the server indicates its capability revision is stale. The response
carries an explicit expiry and revision/ETag. Refresh on expiry
and stale-revision responses; conditional requests still recheck current caller
and Run authorization. If refresh fails, stop dependent platform operations
once guidance expires and report the failure. The helper pins the configured
AG origin and rejects redirects to other origins; authentication and token
refresh stay in protected host configuration, outside model-visible output.
Stale guidance cannot permit stale authority or silently route around
an unavailable platform service. A capability removal must be enforced by the
service even if a harness still holds older instructions.

Keep credentials, signing keys, provider secrets and sensitive tenant/runtime
payloads out of generated instructions. Authentication and credential refresh
belong in the trusted adapter/host configuration. Treat untrusted descriptive
content as data, not executable directives; the instruction generator must not
turn model output, Skills or capability metadata into new Run authority.

## Alternatives considered

- A complete checked-in capability snapshot drifts. A static retrieval
  bootstrap is retained because it gives an existing harness a stable entry
  point without embedding changing platform state.
- Harness-specific code for every component duplicates discovery and exposes
  internal topology; AG remains the entry point in the intended composition.
- Instructions alone cannot enforce governance. A compliant adapter and the
  service/gateway enforcement boundaries remain necessary.

## Consequences and implementation prerequisites

Before implementation, specify and review the wire schema, authentication,
context filtering, trusted capability sources, revision/expiry behavior and
harness adapter lifecycle. Decide how capability changes are delivered without
inventing an AR worker API or relying on unpublished component types.

Tests must cover tenant/context isolation, stale or removed capabilities,
unsupported contract versions, secret exclusion and injected descriptions.
Add compatibility tests and operator documentation with the actual endpoint.
This decision adds no current executable capability and does not close the AR/gateway
[qualification deferrals](0012-integration-rc-qualification-deferrals.md).

## References

- [Platform composition](../architecture/platform-composition.md)
- [Service boundaries](0005-service-boundaries-and-contracts.md)
- [Identity and tenant authority](0003-oidc-authentication-and-tenant-authority.md)
- [Integration guide](../operations/integrations.md)
