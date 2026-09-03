# Test boundary

This directory is reserved for cross-package contract, integration, security,
and end-to-end test suites and their non-sensitive fixtures. Unit tests remain
next to the Go packages they exercise.

Test suites must be deterministic, parallel-safe, tenant-isolated, and runnable
through the Make targets introduced in ENG-008. The initial Go package verifies
that the required target surface and aggregate verification gates cannot be
removed accidentally.

Container contract tests enforce immutable builder/runtime pins, static build
flags, numeric non-root identity, OCI metadata, and the allowlisted Docker build
context. `container_smoke.sh` supplies the daemon-backed runtime proof through
`make container-smoke` and the aggregate `make verify` gate.

`make test-security` is the stable adversarial acceptance gate. It groups the
authorization, approval, artifact-integrity, cache, freshness, replay, and
evidence controls described in `docs/security/adversarial-testing.md`. Its
evidence-suppression cases require the same real PostgreSQL instance as
`make test-integration`.

`make test-postgres-pitr` creates an isolated PostgreSQL primary with continuous
WAL archiving, encrypts a physical base backup, and restores to named points on
both sides of an atomic governance transaction. Each recovered copy is migrated
forward from the prior schema and checked for epoch, evidence/outbox, checkpoint,
and allocation invariants. The fixture and its ephemeral encryption key are
removed after the rehearsal; the command never uses the developer database.

`go test -run '^$' -bench . ./test/operations` runs deterministic policy and
stream/cursor component baselines. These benchmarks catch local regressions but
do not constitute OPS-010 evidence; the production-shaped qualification must use
the deployed API composition and topology described in
`docs/operations/load-testing.md`.
