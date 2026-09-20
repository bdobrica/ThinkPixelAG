# Back up and recover authoritative state

PostgreSQL is the governance authority. Use encrypted physical backups and
continuous WAL archiving with a recovery target and retention policy chosen for
your deployment. Keep backup credentials and encryption keys in managed secret
storage, outside the harness and outside this repository.

Preserve schema/migration checksums, policy activation and approval state,
revocation epochs, Run/admission identities, allocations, outbox/audit records
and delivery checkpoints. Preserve the independent evidence sink's durable
receipts/history and its own backup procedure. Track approved policy artifacts,
workload bindings, trusted public key metadata and secret-manager recovery
references alongside the database recovery plan. Valkey is disposable.

At least quarterly, restore into an isolated environment with no production
publisher, gateway or evidence receiver connectivity. Replay WAL to the chosen
boundary, then run [check-restored-invariants.sql](../../scripts/check-restored-invariants.sql)
and the [backup/PITR rehearsals](testing/recovery-testing.md). Apply necessary
forward migrations and test a core governed workflow. Record the actual RPO,
RTO, backup/WAL IDs and invariant results.

Before promotion, fence the old writer, verify non-regressing epochs and
balances, matching migration checksums, allocation conservation and continuous
evidence/outbox links. Reconcile consumer cursors and evidence checkpoints;
never assume a database restore alone reconciles external effects. Follow the
[detailed recovery procedure](runbooks.md#backup-restore-and-point-in-time-recovery).

The homelab SSD database and sink share a host. Process recovery evidence does
not establish host-loss HA. RAM disks may accelerate disposable test state;
they do not qualify durable recovery. Production failure-domain and failover
qualification remains [explicitly deferred](../adr/0012-integration-rc-qualification-deferrals.md).
