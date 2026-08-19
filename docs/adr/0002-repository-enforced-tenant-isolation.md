# ADR-0002: Repository-enforced tenant isolation for the RC

- Status: Accepted
- Date: 2026-08-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Every tenant-owned row carries `tenant_id`, and repository queries bind a
server-derived tenant explicitly. DATA-012 evaluates whether PostgreSQL
row-level security (RLS) should add a second read/write boundary for the release
candidate.

Effective RLS requires more than enabling policies. The runtime must connect as
a role that neither owns the tables nor has `BYPASSRLS`; otherwise table-owner
bypass makes the control ineffective unless every table uses `FORCE ROW LEVEL
SECURITY`. Each operation must also bind trusted tenant context without leaking
it between pooled connections. `SET LOCAL` is safe only inside a transaction,
while the current repository contract deliberately supports single-statement
pool operations as well as transaction-bound operations. Session-level settings
can survive pool reuse and are not an acceptable tenant boundary.

Some authoritative operations are intentionally not tenant-local. Outbox
publishing, globally ordered audit and revocation consumption, reconciliation,
health checks, and migrations require cross-tenant or administrative access.
The current local and test configuration also uses one database owner identity;
claiming RLS protection under that identity would provide false assurance.

## Decision

The RC uses mandatory repository tenant predicates, not PostgreSQL RLS, as its
tenant read/write enforcement layer. No RLS policies or session tenant variable
are added in Phase 2.

The compensating database and adapter controls are:

- tenant identity must come from trusted authentication/mapping code and is
  bound when constructing a `TenantRepository` (identity wiring is delivered in
  Phase 3; request data is never an allowed source);
- tenant-addressable SQL includes `tenant_id` as the first predicate and uses
  parameters; callers cannot supply table names;
- cross-tenant identifiers return the same not-found result as absent records;
- composite `(tenant_id, id)` foreign keys prevent cross-tenant relationships;
- tenant-leading indexes support the enforced access paths; and
- integration tests exercise cross-tenant enumeration and transaction rollback
  against real PostgreSQL.

Production deployment must separate migration ownership from the runtime role
and grant the runtime only the statements required by its repositories and
workers. That least-privilege role work remains deployment hardening; it does
not turn RLS into an unrecorded dependency for RC correctness.

RLS must be reevaluated before it is represented as an active control. Adoption
requires all of the following in one tested change:

1. distinct non-owner, non-`BYPASSRLS` tenant-runtime, global-worker, and
   migration/admin roles with explicit grants;
2. transaction-scoped trusted tenant context set before every tenant query, or
   separate tenant-bound pools that cannot leak session state;
3. policies for every tenant-owned table, including deliberate handling of
   nullable `tenant_id` in global audit and outbox records;
4. `FORCE ROW LEVEL SECURITY` or equivalent tests proving owner bypass cannot
   invalidate the claimed boundary; and
5. adversarial integration tests for select, insert, update, delete, foreign-key
   behavior, pooled connection reuse, worker access, migrations, and restore.

## Alternatives considered

- Enable RLS immediately using a custom `current_setting` tenant variable. This
  is rejected because pool-level repository calls cannot safely use `SET LOCAL`,
  session settings can leak, and the table-owning development role bypasses
  ordinary policies.
- Force RLS while retaining one role. This would mix tenant requests,
  cross-tenant workers, migrations, and administration behind bypass paths and
  would not provide meaningful least privilege.
- Require every repository operation to run in a tenant-bound transaction and
  introduce separate worker/admin roles now. This is a viable future design but
  expands DATA-012 into a runtime role, deployment, and repository-contract
  redesign without improving the already tested RC correctness boundary.

## Consequences

Tenant isolation remains visible and reviewable in handwritten SQL and does not
depend on mutable connection state. The application layer remains a critical
security boundary: a future unscoped query could expose tenant data despite
schema constraints. Reviews and tests must therefore reject tenant-owned
repository operations without explicit tenant predicates.

The RC must not advertise RLS as enabled. Deployment documentation must keep
migration and runtime credentials conceptually separate, and future role/RLS
hardening must preserve the cross-tenant worker paths deliberately rather than
granting a broad bypass to the API role.

## Security impact

This decision avoids an ineffective RLS configuration that operators might
mistake for defense in depth. It accepts the residual risk of an application
query omitting its tenant predicate and mitigates it through closed repository
APIs, parameterized tenant-first SQL, composite tenant foreign keys,
enumeration-safe results, and real-database isolation tests.

## Operational impact

There is no tenant session state to initialize, reset, or recover after pooled
connection failures. Global workers retain explicit access paths. Production
credentials should use least privilege even though concrete Kubernetes roles
and grants are delivered with deployment hardening.

## References and evidence

- `PLAN.md`, sections 3.2, 6, 8, and 13
- `TODO.md`, DATA-008 and DATA-012
- `docs/architecture/database-schema.md`
- `internal/adapters/postgres/repository.go`
- `internal/adapters/postgres/repository_integration_test.go`
