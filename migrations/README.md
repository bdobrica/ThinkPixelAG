# Migration boundary

This directory contains ordered Tern SQL migrations and migration test
fixtures. Migration files use contiguous, zero-padded sequence numbers and
descriptive names, for example `001_create_registry.sql`. Up and down SQL are
separated by `---- create above / drop below ----`; omit the separator when a
migration is intentionally forward-only.

Migrations are applied by an explicit migration job through the wrapper in
`internal/adapters/postgres`; API replicas never migrate on startup. Out-of-order
application is disabled. Once a migration has been shared or released it is
immutable, and schema corrections are made with a new forward migration. The
`checksums.sha256` manifest makes that compatibility rule executable: every SQL
migration must be listed, and changing a released migration fails before any
database transaction begins. A new migration and its checksum are reviewed as
one change.

Each migration must be safe to apply to the immediately preceding released
schema while preserving existing rows and old application reads/writes during
the supported expand/migrate/contract window. Destructive renames, column or
table removal, incompatible type changes, and newly mandatory fields require a
later contract migration after the compatibility window. Failed migrations are
transactional: operators correct the failure with a new artifact or forward
migration and rerun the explicit job; they do not edit a released migration.

The initial schema is split along durable consistency boundaries:

- `001` contains tenant identity, agent registry, immutable versions and policy
  activation metadata;
- `002` contains runs, immutable version-resolution snapshots, signals and
  ordered append-only events;
- `003` contains resource definitions, immutable grants, mutable balances,
  reservations, trusted usage and exactly-once settlement records;
- `004` contains revocations, monotonic epoch rows, the globally ordered change
  log and durable gateway checkpoints;
- `005` contains request-hash-bound idempotency, audit evidence and the
  claim/retry/dead-letter transactional outbox.
- `006` enforces database-level immutability for registered agent versions and
  their normalized capability declarations, and aligns skill declaration
  storage with the bounded public manifest contract.
- `007` enforces database-level append-only approval, deprecation, and
  revocation history for agent versions.
- `008` makes persisted run version-resolution evidence immutable.
- `009` validates canonical resource units and class-specific aggregation and
  deadline representations.
- `010` enforces database-level immutability for issued envelope grants while
  leaving their paired balance rows mutable.
- `011` makes the committed dimension vector of each resource reservation
  immutable while leaving reservation headers available for exactly-once
  settlement state changes.

Tenant-owned relationships use composite foreign keys containing `tenant_id` so
a database write cannot connect rows across tenants. Frequently used access
paths begin with `tenant_id`; globally ordered operational streams are the
documented exceptions. Append-only and immutable tables intentionally have no
`updated_at` column. Repositories expose no mutation methods for immutable
artifacts, and security-critical agent version/manifest rows additionally use
database triggers to reject direct updates or deletes.
