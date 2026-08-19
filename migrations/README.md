# Migration boundary

This directory contains ordered Tern SQL migrations and migration test
fixtures. Migration files use contiguous, zero-padded sequence numbers and
descriptive names, for example `001_create_registry.sql`. Up and down SQL are
separated by `---- create above / drop below ----`; omit the separator when a
migration is intentionally forward-only.

Migrations are applied by an explicit migration job through the wrapper in
`internal/adapters/postgres`; API replicas never migrate on startup. Out-of-order
application is disabled. Once a migration has been shared or released it is
immutable, and schema corrections are made with a new forward migration.

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

Tenant-owned relationships use composite foreign keys containing `tenant_id` so
a database write cannot connect rows across tenants. Frequently used access
paths begin with `tenant_id`; globally ordered operational streams are the
documented exceptions. Append-only and immutable tables intentionally have no
`updated_at` column. Runtime roles and repositories will deny updates to those
tables when they are introduced in DATA-007 and DATA-008.
