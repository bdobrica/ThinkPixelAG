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
