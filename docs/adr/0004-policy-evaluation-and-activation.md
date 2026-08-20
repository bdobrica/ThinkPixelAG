# ADR-0004: Fail-closed OPA evaluation and append-only activation

- Status: Accepted
- Date: 2026-08-20
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Authorization policy must be independently promotable without letting network
failure, malformed output, stale artifacts, or rollback erase decision history.

## Decision

Use a strict typed Go boundary around the versioned OPA document. Verify bundle
digest and signature before compile validation and persistence. Activate only
validated bundles with serializable, monotonically versioned, append-only
records. Model rollback as another activation and require bounded local active
metadata freshness before every evaluation.

The optional Valkey decision cache uses versioned, epoch-bound, normalized
input keys and bounded TTLs. Keys hide identifiers behind SHA-256 and entries
are authenticated with a dedicated HMAC secret. Cache failures and invalid
entries bypass to OPA.

## Alternatives considered

Embedding OPA in the service would enlarge dependency and update coupling.
Loading repository Rego directly in production would bypass promotion controls.
Updating activation rows in place would destroy evidence and complicate cache
invalidation. These alternatives were rejected.

## Consequences

OPA and PostgreSQL remain separate operational dependencies. Policy unavailability
denies protected work. Reconciliation must refresh process-local activation
metadata after restart or a post-commit local update failure.
Valkey improves repeated-decision latency but adds no availability or authority
dependency; rotating its HMAC secret safely invalidates all prior entries.

## Security impact

Unknown actions, reason codes, obligations, expanded constraints, invalid
signatures, stale metadata, timeouts, and malformed responses fail closed.
Decision records expose only bounded identifiers, digests, codes, and duration.

## Operational impact

Operators promote immutable signed artifacts and monitor bundle freshness and
OPA latency. Rollback advances the activation version, giving consumers an
unambiguous invalidation signal.

## References and evidence

- `docs/contracts/policy-decision.md`
- `docs/security/policy-lifecycle.md`
- `internal/adapters/opa`
- `internal/adapters/postgres/policy.go`
- `policies/authorization.rego`
