# ADR-0001: PostgreSQL access and migrations

- Status: Accepted
- Date: 2026-08-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

PostgreSQL is authoritative for governance state. Phase 2 needs one driver,
one migration mechanism, a query approach that preserves explicit tenant
predicates, and transaction ownership that cannot be bypassed accidentally.
The service must support PostgreSQL 18.4, context cancellation, typed values,
batching, and concurrency controls without introducing an ORM abstraction over
database invariants.

Migrations run as an explicit job, never implicitly when an API replica starts.
Released migrations are immutable and forward correction is the normal recovery
path.

## Decision

- Use `github.com/jackc/pgx/v5` v5.10.0 and `pgxpool` for runtime PostgreSQL
  access. PostgreSQL-specific features and errors may be used inside the adapter.
- Write parameterized SQL by hand in tenant-scoped repository adapters. Queries
  depend on the minimal `postgres.DBTX` surface shared by pools and transactions.
  Domain and application packages do not import pgx.
- Use `github.com/jackc/tern/v2` v2.4.1 as a library behind the narrow
  `postgres.Migrator` interface. SQL migrations are ordered, will be embedded in
  the explicit migration binary, are applied only in order, and are tracked in
  `public.thinkpixelag_schema_version`.
- Use `postgres.Transactor` for infrastructure-level atomic work. It alone owns
  begin, commit, and rollback; callbacks receive `DBTX`, not `pgx.Tx`. Application
  use cases will expose purpose-specific unit-of-work ports as repositories are
  introduced rather than a generic driver transaction in `internal/ports`.
- Transaction isolation and access mode are explicit `pgx.TxOptions` at each
  call site. DATA-007 will add pool policy, timeouts, error classification, and
  telemetry; DATA-008 will prove rollback and tenant isolation on real PostgreSQL.

## Alternatives considered

- `database/sql` with lib/pq: stable but lib/pq is maintenance-focused and loses
  pgx's PostgreSQL-native types, batching, and notification/copy facilities.
- An ORM: convenient for basic CRUD, but obscures tenant predicates, locking,
  conditional updates, and the resource-accounting SQL this service requires.
- Generated queries with sqlc: viable later if query volume warrants it, but it
  adds generation/tooling before repository shapes are known. Handwritten SQL
  remains replaceable behind repository interfaces.
- golang-migrate or a standalone Tern CLI: both can apply migrations, but using
  Tern as a library gives the repository one tested wrapper and permits an
  embedded, minimal migration job without requiring an extra runtime binary.

## Consequences

Repository code remains explicit and reviewable, and the same query functions
work with a pool or an owned transaction. The project accepts PostgreSQL/pgx
coupling inside the adapter and must maintain SQL mapping code. Tern's SQL
templating support adds dependencies to the migration binary.

Schema rollback commands are intentionally not part of the service-facing
interface. Recovery normally uses a new forward migration; destructive rollback
is an operator-controlled procedure documented per migration when safe.

The selected graph includes exact pseudo-versions of `pgservicefile` (through
pgx) and `go-ini` (through Tern). Both are version-bound in the dependency policy
with review expiry on 2027-02-19; upgrade or renewal is required before then.

## Security impact

Tenant filters remain visible in every repository query and can be tested.
Parameterized SQL is mandatory. Restricting callbacks to `DBTX` prevents them
from committing partial governance state. Database URLs remain secret
configuration and must never be logged. Migration credentials can be separated
from the less-privileged runtime role in deployment.

## Operational impact

The migration job must complete before replicas requiring the new schema become
ready. Tern records the current version in a dedicated table, serializes jobs
with an advisory lock, and the wrapper rejects missing sequence numbers and
invalid SQL migration filenames. Connection policy, statement timeouts, health,
and telemetry are deferred to DATA-007; migration compatibility and recovery
exercises are DATA-011.

## References and evidence

- `PLAN.md`, sections 3.2, 4, 6, and 10
- `TODO.md`, DATA-001 and DATA-007 through DATA-011
- `internal/adapters/postgres`
- pgx v5 and Tern v2 package documentation
