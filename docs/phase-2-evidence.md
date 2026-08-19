# Phase 2 Authoritative Persistence Evidence

Phase 2 exited on 2026-08-19 after real-PostgreSQL qualification of the
authoritative schema and persistence adapters. The audited implementation tree
is commit `b79ca07`; the follow-up ledger-only commit records its short
SHA without changing the qualified implementation.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.6 linux/amd64` |
| PostgreSQL | `18.4` on the pinned `postgres:18.4-alpine3.23` image |
| PostgreSQL transport | loopback TCP with password authentication |
| Race detector | enabled for the PostgreSQL adapter concurrency suite |

The test database was disposable and contained no production data. Integration
helpers created uniquely named databases for destructive migration scenarios
and removed them during test cleanup.

## Real-database qualification

The following commands completed successfully with
`THINKPIXELAG_TEST_DATABASE_URL` or the Makefile's equivalent
`TEST_DATABASE_URL` pointed at the pinned PostgreSQL service:

```sh
go test -count=1 -race ./internal/adapters/postgres
make test-integration
make verify
```

The race-enabled adapter run and integration gate proved:

- pool connectivity, statement timeouts, readiness failure/recovery, and
  transaction commit/rollback behavior;
- migration from an empty database, seeded prior-version upgrade, immutable
  checksums, constraint enforcement, tenant-leading index plans, transactional
  migration failure, and forward recovery;
- enumeration-safe cross-tenant reads and writes plus rollback across every
  tenant-addressable Phase 2 repository kind;
- exactly one owner under concurrent idempotency acquisition, stable replay,
  request-hash conflicts, expiry replacement, lease takeover, and stale-owner
  fencing;
- atomic mutation/audit/outbox commit and rollback, disjoint concurrent
  `SKIP LOCKED` claims, at-least-once replay, fenced publication, bounded retry,
  and durable poison-message dead lettering.

No required PostgreSQL test was skipped. The complete `make verify` gate also
passed generation drift, formatting/vet/module/OpenAPI checks, unit and full
race tests, policy/e2e targets, Compose validation, dependency source and
license policy, reachable-vulnerability analysis, static binary build, OCI
image build, and hardened non-root runtime smoke.

## Continuous enforcement

`make test-integration` now always supplies a database URL, disables the Go test
result cache, and therefore fails when real PostgreSQL is unavailable instead
of accepting skipped persistence tests. The CI integration job starts the same digest-pinned PostgreSQL 18.4
image, waits for authenticated readiness, and passes its disposable database
URL through `TEST_DATABASE_URL`. Ordinary unit and race targets retain optional
database behavior so they remain hermetic; the required integration and
aggregate verification gates do not.

## Exit conclusion

All DATA-001 through DATA-013 deliverables and the Phase 2 isolation, rollback,
migration, concurrency, and replay exit conditions are satisfied. Repository
enforcement remains the accepted RC tenant-isolation boundary under ADR-0002;
later phases build typed domain mutations on this qualified persistence layer.
