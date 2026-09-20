# Architecture Decision Records

ADRs record consequential decisions that should remain understandable after the temporary implementation plan is removed.

## Naming and lifecycle

- Name records `NNNN-short-kebab-case-title.md`, beginning with `0001`.
- Copy `template.md`; do not repurpose an accepted ADR.
- Allowed statuses are `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, and `Deprecated`.
- Materially changing an accepted decision requires a new ADR that supersedes it.
- Keep alternatives and consequences honest; an ADR is a decision record, not promotional documentation.

## Index

- [ADR-0001: PostgreSQL access and migrations](0001-postgresql-access-and-migrations.md)
- [ADR-0002: Repository-enforced tenant isolation for the RC](0002-repository-enforced-tenant-isolation.md)
- [ADR-0003: OIDC authentication and tenant authority](0003-oidc-authentication-and-tenant-authority.md)
- [ADR-0004: Fail-closed OPA evaluation and append-only activation](0004-policy-evaluation-and-activation.md)

- [ADR-0005: Modular governance service and explicit contracts](0005-service-boundaries-and-contracts.md)
- [ADR-0006: Atomic Run authority, replay and fenced ownership](0006-run-lifecycle-and-replay.md)
- [ADR-0007: Transactional resource conservation](0007-authoritative-resource-accounting.md)
- [ADR-0008: Monotonic revocation and bounded freshness](0008-revocation-freshness-and-reconciliation.md)
- [ADR-0009: Transactional evidence and replay-safe independent delivery](0009-transactional-and-independent-evidence.md)
- [ADR-0010: Narrow privileged authority and managed key boundaries](0010-privileged-authority-and-managed-keys.md)
- [ADR-0011: Restricted deployment and evidence-bound integration releases](0011-restricted-deployment-and-release-evidence.md)

- [ADR-0012: Integration-RC qualification deferrals](0012-integration-rc-qualification-deferrals.md)

The [supported-version matrix](../operations/supported-versions.md) is a maintained
operator reference. Deployment/release policy is recorded in ADR-0011; the
matrix records tested versions without rewriting accepted decisions.
