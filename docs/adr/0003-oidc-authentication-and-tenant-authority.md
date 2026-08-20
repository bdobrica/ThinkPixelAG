# ADR-0003: OIDC authentication and tenant authority

## Status

Accepted — 2026-08-20

## Context

The governance plane cannot trust tenant, principal, or role hints supplied by
ordinary callers. Signing-key rotation must not turn an IdP outage into either
an indefinite fail-open cache or immediate rejection of all valid tokens.

## Decision

Use a standard-library OIDC/JWT adapter with one pinned issuer and audience per
process. Pin asymmetric algorithms, require same-origin discovery/JWKS, clamp
cache TTLs, refresh unknown keys, and permit known cached keys during outages
only up to a separate stale bound. Map `sub` and a configured tenant claim only
after complete verification. Map roles through an explicit allowlist. Reject
forwarding and body tenant hints at the HTTP boundary.

## Alternatives considered

- A general JWT library was not selected because this bounded RS256/ES256
  surface benefits from an auditable algorithm switch and no new dependency.
- Trusting ingress headers was rejected because authenticated peer and original
  assertion binding are not implemented.
- Unbounded provider cache advice was rejected because it delegates the
  security freshness contract to an external response.

## Consequences

Deployments configure issuer, audience, claim names, algorithms, role mappings,
time bounds, and cache bounds. Multi-issuer deployments require separate
instances until a reviewed issuer-to-tenant mapping model is added. Providers
that delegate JWKS to another host are intentionally unsupported.

## Security

Algorithm confusion, forged headers, missing tenant identity, stale keys, and
unbounded token lifetime fail closed. Raw token roles cannot directly grant an
application role.

## Operations

Verifier construction requires live discovery/JWKS. Rotation refreshes on an
unknown `kid`; refresh failure uses a known key only within its stale interval,
then returns retryable service unavailability.

## References

- `TODO.md`: AUTH-001 through AUTH-003
- `docs/security/authentication.md`
- `docs/security/threat-model.md`: T01 through T03 and T19
