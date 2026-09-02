# Production runbooks

These procedures preserve PostgreSQL as the sole governance authority and keep
authorization fail closed. Commands are templates: select one namespace,
release digest, and backup identifier explicitly before changing production.
Never paste credentials, tokens, policy payloads, or evidence bodies into an
incident channel or ticket.

## Install and configure

1. Create separate least-privilege runtime and migration database roles. Create
   `thinkpixelag-runtime` and `thinkpixelag-migration` Secrets through the
   platform secret manager; do not render secret values into Git.
2. Copy `deploy/kubernetes/base` to an environment overlay. Replace the image
   placeholder with a verified immutable digest and restrict every required
   egress destination (PostgreSQL, OPA, IdP/JWKS, telemetry, evidence sink,
   managed signer, optional Valkey). The base intentionally denies all other
   egress and therefore is not directly deployable.
3. Render with `kubectl kustomize`, apply the migration Job first, verify its
   version output, then apply the API Deployment. Confirm `/livez`, `/readyz`,
   `/metrics`, NetworkPolicies, PDB, and all three replicas.

## Migration, upgrade, and rollback

Back up first. Verify the target image signature, SBOM, digest, migration
checksums, and compatibility notes. Run `/thinkpixelag-migrate` with only the
migration Secret; API pods never migrate implicitly. Deploy at `maxUnavailable:
0`, observe readiness/error/latency/revocation metrics, and stop on invariant
alerts. Application rollback uses the prior immutable digest only when its code
supports the current schema. Database recovery is forward-only: fix a failed or
incompatible schema with a new migration, never edit or down-migrate a released
file.

## Backup, restore, and point-in-time recovery

Use the PostgreSQL platform's encrypted physical backup plus continuous WAL
archive. Record server version, backup/WAL identifiers, schema version, recovery
target, encryption-key reference, and checksums outside this repository. At
least quarterly, restore into an isolated database, replay WAL to a chosen
transaction boundary, run `scripts/check-restored-invariants.sql`, apply any
forward migrations, and exercise a core workflow. Never connect a restored
copy to production publishers, evidence sinks, or gateways. Promote only after
epoch values do not regress, outbox/audit/evidence links verify, allocations do
not exceed grants, and migration checksums match.

## Policy rollback

Select a previously validated and signed policy digest. Obtain the required
four-eyes approval, activate it as a new monotonically increasing activation,
and confirm all replicas refresh within the bundle-age bound. Do not mutate an
activation row or reuse its version. If OPA is unavailable or returns malformed
data, protected work remains denied while the dependency is restored.

## Revocation gap or staleness

Remove stale replicas from readiness. Compare the gateway cursor and epoch
vector with the authoritative snapshot, call the reconciliation endpoint with
strong workload identity, atomically replace local state, then resume from the
returned cursor. A retention gap requires snapshot reconciliation; skipping a
sequence or lowering an epoch is prohibited.

## Service or database outage

For a database outage, stop mutating traffic, preserve fail-closed readiness,
inspect connection saturation/latency and managed-service failover, and avoid
raising pool sizes until the global connection budget is checked. After
recovery, verify schema version, epochs, outbox age, allocations, and evidence
checkpoints before restoring traffic. A process crash or eviction is recovered
by Kubernetes; readiness prevents traffic until PostgreSQL and security
freshness pass.

## OPA or Valkey outage

OPA outage/malformed output is an authorization outage: restore the last
verified bundle and dependency; never bypass policy. Valkey is disposable and
non-authoritative. Disable or flush it, allow evaluation to bypass to OPA, and
investigate latency; cached ALLOW never substitutes for high-risk authoritative
checks.

## Outbox backlog

Compare pending count/oldest age with database and evidence-sink health. Scale
bounded publishers only after checking sink rate limits and DB capacity.
Duplicate delivery is safe only with the stable event ID; do not delete or edit
pending messages. At five minutes, page the owner. Verify receipts, monotonic
checkpoints, and chain continuity when caught up.

## Key rotation

Create a new non-exportable managed KMS/HSM key version, verify its purpose and
algorithm, obtain four-eyes approval, publish signed trust metadata, and overlap
verification with the old public key. Activate monotonically, verify new
signatures and evidence, then retire the old signing version only after all
artifacts and rollback windows expire. Rotating the Valkey HMAC key intentionally
invalidates cache entries. Rotation never places private key material in files,
Secrets, logs, or agent state.

## Break glass

Use only the narrow policy/revocation recovery scopes in the versioned contract.
Require fresh phishing-resistant MFA plus digest-bound four-eyes approval,
activate for no more than 15 minutes, and use the one-time credential once.
Monitor the separate activation/use/expiry/revocation evidence in real time.
Revoke immediately after recovery and investigate any unused or expired grant.

## Latency or capacity

Identify the affected bounded route, policy latency, PostgreSQL saturation,
outbox/revocation lag, and Go runtime pressure. Preserve security timeouts and
freshness bounds. Scale replicas only within DB and external-dependency budgets;
use the last Phase 8 load report to distinguish CPU, DB contention, OPA, and SSE
fanout limits.
