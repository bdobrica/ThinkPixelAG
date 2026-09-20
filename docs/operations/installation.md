# Install and accept a deployment

For a working local service and real API calls, use the [local installation](../examples/local-sandbox/README.md).
For Kubernetes, start from the [team PoC installation](../examples/team-poc/README.md).
The base manifests deliberately require environment-specific configuration.

## Prepare dependencies and trust

Provision durable PostgreSQL, an OPA evaluator, an OIDC issuer and an independent
evidence receiver. Choose the [tested versions](supported-versions.md). Keep
runtime and migration database roles separate. Establish backups before storing
authoritative state. Valkey is optional and is not a readiness dependency.

Deliver database URLs, cursor HMAC keys and evidence authentication through the
secret manager. Use authenticated encrypted transport and configure trusted CA
roots. Production signing requires the managed KMS/HSM boundary; qualification
of a selected provider is separate from the RC's component tests.

Provision the first tenant, signed policy and approved agent using the
[protected operator bootstrap](bootstrap.md). The source candidate composes
registry and policy administration in the explicit local-development profile.
Production KMS/HSM custody and external-provider qualification remain separate.
No console is required, and no peer accesses AG's database directly.

## Configure and migrate

Set issuer, audience, role mappings, database/OPA endpoints and bounded pools
using the [configuration reference](../configuration.md). Mount a runtime JSON
file and set `THINKPIXELAG_RUNTIME_FILE` to enable governed routes. Deliver a
shared `THINKPIXELAG_CURSOR_HMAC_KEY` separately. Trusted APIs require the
additional mTLS listener, certificates and workload bindings.

Render the Kubernetes overlay and review its immutable image digest, secret
references, dependency egress, ingress, resources and topology. Run the migration
Job with the migration role **before** starting API traffic. Migration is never
an API-pod startup action. Schema 23 is the current source-candidate baseline; older databases
need the forward migration/restore rehearsal in the [runbooks](runbooks.md).

## Accept the deployment

Check `/livez` for process health and `/readyz` for database, active-policy and
revocation freshness. A healthy process or OPA process alone does not prove
that governed routes are ready. Check all intended API replicas and restrict
metrics access to monitoring systems.

With controlled test identities, verify admission, exact idempotency replay,
Run read and cross-tenant/invalid-token denial. Check evidence receipts and
outbox drain, allocation invariants and revocation reconciliation. Start below
the [measured capacity point](capacity.md) and observe the complete dependency
path before increasing traffic. Record your own offered/completed/dropped
counts and failure domains; homelab results are not your deployment's guarantee.
