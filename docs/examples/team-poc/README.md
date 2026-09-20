# Team Kubernetes PoC

This overlay provides three restricted AG replicas, a separate migration Job,
a runtime ConfigMap, Service, default-deny networking and a disruption budget.
It pins the [qualified RC image](../../evidence/rc/final-artifacts.md) and reuses
the maintained Kubernetes base. It is a template for an internal team PoC;
external dependencies and trust must be supplied before applying it.

## Supply your environment

Use a namespace with NetworkPolicy enforcement and enough node capacity for
three replicas plus rollout headroom. Provide durable PostgreSQL, an OPA service,
an OIDC issuer and evidence receiver, plus secret/certificate delivery. Follow
[installation](../../operations/installation.md) for approved agent/policy
provisioning and the runtime's composition limits. This overlay does not create
an IdP, managed signer, AR worker or harness.

Copy this directory to an ignored environment directory and adjust the base
path, or maintain your customized overlay in your infrastructure repository.
Set the following before deployment:

| Input | Where / requirement |
|---|---|
| Namespace and image | `kustomization.yaml`; verify the digest and available artifact metadata |
| Non-secret endpoints | Patch the base `thinkpixelag` ConfigMap: OPA, OIDC issuer/audience, role mappings and managed signer reference |
| Runtime database and credentials | Secret `thinkpixelag-runtime`: runtime `THINKPIXELAG_DATABASE_URL`, shared cursor HMAC key and configured evidence authentication |
| Migration database | Secret `thinkpixelag-migration`: `THINKPIXELAG_DATABASE_URL` using the migration role |
| Runtime constraints | `runtime.json`; ceilings do not grant caller authority |
| Egress | Add exact API destinations for database, OPA, JWKS, sink and any enabled telemetry/signer/cache; adapt migration-network.yaml separately |
| TLS and ingress | Private authenticated TLS ingress to HTTP port 8080; use the namespace labels expected by the base NetworkPolicy |
| Trusted services | Extend runtime JSON with the complete mTLS settings, mount certificates/bindings, add a separate container/Service port and narrowly scoped ingress |

Use a secret manager; never put values in these example files. Production mode
requires managed signing configuration and encrypted dependency transport. A
placeholder key ID passing configuration validation does not qualify a provider
or implement the absent management routes. Keep the PoC internal until those
prerequisites are satisfied.

## Render and deploy in order

From the repository root, inspect the supplied example without changing a cluster:

```sh
kubectl kustomize docs/examples/team-poc > /tmp/thinkpixelag-poc.yaml
```

The output still contains environment placeholders by design. After customizing,
render again and review all resources. Create the namespace, secrets and
network policies first. Apply the ServiceAccount and configuration, then apply
**only the migration Job**, wait for success and inspect its schema/version
output. Finally apply the API Deployment, Service and disruption budget. Do not
apply the whole rendered file as a substitute for sequencing: kubectl does not
wait for a Job before creating a Deployment, and the Argo CD annotations only
sequence an Argo-managed sync.

The migration Job's pod label is `thinkpixelag-migrate`; the API egress policy
does not select it. The included separate migration policy prevents overlooking
this under default-deny egress. Adjust its namespace/pod selectors for your DB.
For later upgrades, use a fresh versioned migration Job name to avoid immutable
Job-template conflicts; follow the [upgrade runbook](../../operations/runbooks.md#migration-upgrade-and-rollback).

## Accept and observe

Confirm all replicas are ready, then test admission, replay and denial with
controlled identities. Validate evidence delivery, revocation freshness and
accounting. Install the optional [Prometheus assets](../../../deploy/kubernetes/optional)
only when their CRDs are present; follow [monitoring](../../operations/monitoring.md).
Begin below the [measured operating point](../../operations/capacity.md), take
backups and rehearse restoration. Replica count alone establishes neither
production throughput nor database/evidence HA.
