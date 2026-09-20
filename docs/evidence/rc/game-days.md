# Integration-RC game days (RC-005)

This record consolidates the available AG operational rehearsals under the
owner-approved integration-RC scope. Historical installation, PITR and upgrade
results retain their original source/topology; they were not repeated merely
to change the release label. No homelab resources were removed.

| Scenario | Executed evidence | Scope / limit |
|---|---|---|
| Install and restricted runtime | Disposable Kubernetes installation/migration/disruption at `4fff867`; retained posture and RC-002 live smoke | [Lifecycle evidence](../homelab/ops012-evidence.md); not every production distribution |
| Upgrade and image rollback | 73 retained-cluster checks, explicit migration Job, durable state/replay across candidate and rollback | Compatible image pair, schema 18 unchanged; no promise of arbitrary binary rollback |
| Backup/restore, PITR and forward recovery | Encrypted PostgreSQL 18.4 physical base backup plus WAL; exact pre/post-governance targets; schema 17→18; epoch/accounting/evidence invariants | [OPS-009 evidence](../implementation/phase-8-evidence.md), source `fa04a0c`; selected-target RPO zero, local RTO 3–4 seconds; not a provider SLA |
| Durable process recovery | SSD PostgreSQL crash with 42-table content comparison; sink/exporter crash and exact receipt-chain replay | [Database](../homelab/ssd-qualification.md) and [sink](../homelab/ssd-evidence-qualification.md); one SSD host, not host-loss HA |
| Policy rollback | Real PostgreSQL activation 1→2→3 with the third selecting the earlier signed bundle; cache invalidations and loaded authority checked | `TestPolicyActivationAndRollbackAppendVersions`; component repository boundary, not a deployed administrative UI |
| Revocation reconciliation | Real mTLS test consumer disconnected 31 seconds; freshness denied; authoritative reconciliation restored monotonic state | [OPS-011 partition](../homelab/ops011-evidence.md), plus fresh component gap/snapshot tests; production gateway remains DQ-003 |
| Key rotation | New sequential managed-key guard rehearsal: alias/version mismatch denied, explicit old/new verifier overlap, retirement, outage and recovery | Provider-contract simulation; no production KMS/HSM adapter, grants, trust publication or key was exercised |
| Break glass | Real PostgreSQL approved activation, approval replay denial, bound use, expiry, substitution denial and matching event/outbox counts; application strong-authentication failure tests | Synthetic identity/approval fixtures; not a production IdP ceremony or emergency credential issued on the homelab |

## Repeatable component rehearsal

Run `make test-governance-recovery` with `TEST_DATABASE_URL` pointing to an
isolated verification database. The target executes managed signing,
revocation reconciliation/freshness and break-glass application tests with the
race detector, then policy rollback, monotonic revocation and break-glass
integration tests against PostgreSQL. It changes only test fixtures, not live
homelab authority. `make verify` also includes these tests in its broader suites.

Key rotation uses the existing non-exportable-key/version contract. Instances
remain bound to the inspected immutable version; changing a provider alias
does not silently rotate an existing signer. Overlap is represented by two
explicitly bound verifiers. The fake provider supplies metadata and outcomes;
this test establishes guard behavior, not cryptographic provider correctness.
Production provider rotation, signed trust publication and retirement require
the selected deployment's adapter, grants and operator procedure before that
configuration is advertised as qualified. Local/test signing may remain
disabled under the [managed-signing contract](../../contracts/managed-signing.md).

Use the [operator runbooks](../../operations/runbooks.md) for the corresponding
procedures. Unavailable AR/gateway and intended-production HA rehearsals remain
in the [existing deferral register](../../adr/0012-integration-rc-qualification-deferrals.md).
The scope does not waive signature verification, four-eyes approval, monotonic
activation/epochs, single-use authority, or transactional evidence.

## Verification

On 2026-09-19 the focused rotation race test, `make test-governance-recovery`
and `make verify` passed. The database was supplied to every suite and Valkey
integration was enabled: zero failing or skipped test executions. The recovery
target also rejects an empty database URL before running tests. See the
[aggregate record](rc005-verification.json).
