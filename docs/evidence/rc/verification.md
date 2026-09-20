# Integration-RC verification (RC-002)

On 2026-09-19, `make verify` passed with a clean working tree before and after
execution at `79d2ac91cc89846de6baf30c570d562260edafc1` (RC-001).
The committed report is a subsequent evidence-only change.

The gate covered generated artifacts, frozen contracts, formatting/static and
OpenAPI validation, unit/coverage, race, 27 Rego cases, PostgreSQL integration,
end-to-end and adversarial security suites, Compose/Kubernetes contracts,
dependency/license/vulnerability checks, compilation and the restricted
container smoke test. PostgreSQL used the isolated RAM-backed verification
database; this run makes no durability claim. Authenticated Valkey and a wrong
credential URL were supplied, so its real integration tests executed.

Go test invocations used `-json -count=1`; the wrapper only recorded events and
forwarded output/exit status. Seven untagged PostgreSQL tests initially skipped
in the default unit and race invocations because those targets do not export
the database variable. All seven passed in the integration target and in a
supplemental `go test -count=1 -race ./internal/adapters/postgres` with
`THINKPIXELAG_TEST_DATABASE_URL` supplied. There are zero unresolved skipped
tests or failing invocations in this record. This is an observed result, not a
claim that a single run proves absence of flakes.

Every domain fuzz target also passed a bounded campaign:
`go test ./internal/domain -run '^$' -fuzz '^TARGET$' -fuzztime=10s -parallel=2`.
The six targets cover identifiers, decimals, cursors, allocation conservation,
checked arithmetic and Run lifecycle properties. This is bounded fuzz evidence,
not exhaustive input coverage.

A fresh smoke test on the retained ARM64 Kubernetes deployment passed admission,
idempotent replay, cross-tenant denial, forged-token denial and Run reads.
Three API pods were ready with probes, read-only roots, dropped capabilities and
privilege escalation disabled. Its pinned image is from `30ff25f`; comparison
of `cmd`, `internal`, `migrations`, `go.mod` and `go.sum` to the verified source
found no application-source changes. This is a live smoke of equivalent
application source, not a new deployment of the RC-001 metadata or a repeat of
the historical [full installation/lifecycle rehearsal](../homelab/ops012-evidence.md).
All homelab resources were retained.

## Archived evidence

- [Gate invocations, counts, resolved skips and private-log fingerprints](rc002-results/verification.json)
- [Fuzz execution counts and outcomes](rc002-results/fuzz.json)
- [Live Kubernetes image, readiness, posture and smoke](rc002-results/kubernetes-smoke.json)

Reports retain aggregate outcomes, not credentials, fixture identities or
runtime payloads. Full redacted logs remain in the ignored local `.cache/rc/`;
their hashes identify this run but do not substitute for published raw logs.
Reproduction requires Go/Docker and the documented verification dependencies,
`TEST_DATABASE_URL`, `OPA`, and the two `THINKPIXELAG_TEST_VALKEY_*URL` values.
Export `THINKPIXELAG_TEST_DATABASE_URL` globally as well to avoid the default
PostgreSQL skips. Never put credentials in committed commands or reports.
