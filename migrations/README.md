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
