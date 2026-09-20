# Team Kubernetes PoC

Deploy the source candidate against deployment-owned PostgreSQL, OPA and OIDC,
with an optional separate console. The generated bootstrap Job runs the real
migration/operator commands and stores a persistent local-development signing
key. This is the [ADR-0016](../../adr/0016-local-development-policy-promotion.md)
software-key profile on a private company network, not production key custody.
For an all-in-one laptop evaluation, use the [local example](../local-sandbox/README.md).

## Prepare dependencies and identity

Provide a dedicated PostgreSQL database, an OPA management endpoint reachable
only by trusted operators/AG, a private HTTPS ingress controller, a StorageClass,
and trusted CA certificates. Reserve a 1 GiB signing-key PVC and one AG replica;
node-local SSD storage is suitable for this PoC but does not establish HA.
Use separate database/migration roles in a production deployment.

Configure two OIDC operators and an ordinary caller with UUIDv7 tenant/subject
claims and external `operators`, `registrars`, `users` roles as described in
[bootstrap](../../operations/bootstrap.md). Configure the console's public OIDC
client with PKCE S256, the exact HTTPS `/callback` URI and an AG-audience access
token; see [console identity setup](../../../console/README.md). No sample issuer
or passwordless operator-selection page is deployed to the team namespace.

Publish or select immutable AMD64/ARM64 AG and console images from the candidate
artifact inventory. The AG image includes `/thinkpixelag-migrate` and the protected
`/thinkpixelag-operator`; the console image contains neither command nor AG state.
The old rc.1 image does not support this bootstrap/profile.

## Render private configuration

Create an operator-owned `0700` directory outside the repository. Write a `0600`
`settings.json` using this shape, replacing all placeholders. Generate the cursor
key with your secret tooling. The bootstrap object is the complete reviewed
snapshot from the [bootstrap guide](../../operations/bootstrap.md); its issuer
must match the configured IdP. Include the two operators and caller in principals.

```json
{
  "namespace": "thinkpixelag-poc",
  "ag_image": "quay.io/YOUR_ACCOUNT/thinkpixelag@sha256:REPLACE",
  "console_image": "quay.io/YOUR_ACCOUNT/thinkpixelag-console@sha256:REPLACE",
  "ag_origin": "https://ag.poc.example",
  "console_origin": "https://console.poc.example",
  "issuer": "https://identity.example/realms/poc",
  "audience": "thinkpixelag",
  "client_id": "thinkpixelag-console",
  "database_url": "postgresql://USER:PASSWORD@db.private:5432/thinkpixelag?sslmode=verify-full&sslrootcert=/trust/ca.crt",
  "cursor_key": "REPLACE_WITH_AT_LEAST_32_SECRET_BYTES",
  "ca_file": "/private/poc/combined-ca.pem",
  "storage_class": "YOUR_DURABLE_STORAGE_CLASS",
  "ingress_class": "traefik",
  "ingress_namespace": "kube-system",
  "tls_secret": "poc-tls",
  "authority_constraints": {"max_execution_time_seconds": 300, "max_llm_tokens": 1000, "max_tool_calls": 10},
  "ag_egress": [{"cidr": "10.20.0.0/24", "ports": [5432, 8181, 443]}],
  "console_egress": [{"cidr": "10.20.0.0/24", "ports": [443]}],
  "bootstrap": {"REPLACE": "complete reviewed bootstrap object"}
}
```

Supply CA roots needed for AG/OIDC/ingress. PostgreSQL TLS may additionally need
its driver-specific CA configuration. The minimal template uses private tokenless
OPA; for authenticated OPA, mount a protected token reference in AG and the
bootstrap job as described in the configuration guide. No token goes to the BFF.
Optionally configure `evidence_endpoint`, `evidence_sink_id` and `evidence_token`
to drain AG's durable outbox to an independent receiver. Without them, monitor
outbox growth and do not claim independent sink qualification.

```sh
python3 deploy/evaluation/team.py --settings /private/poc/settings.json \
  --output /private/poc/manifests --console
```

Omit `--console` to deploy AG alone. Inspect the private rendered manifests before
applying: immutable images, dedicated database, exact bootstrap IDs, storage,
TLS/ingress and egress rules. Set CIDRs/ports to actual dependency destinations;
account for your CNI's service/NAT handling. BFF egress needs AG and IdP HTTPS,
not database/OPA access. Never commit generated Secret manifests.

## Install in dependency order

```sh
kubectl apply -f /private/poc/manifests/foundation.json
# Create the deployment-owned ingress certificate in this namespace.
kubectl -n thinkpixelag-poc create secret tls poc-tls \
  --cert=/private/poc/ingress.crt --key=/private/poc/ingress.key
kubectl apply -f /private/poc/manifests/bootstrap.json
kubectl -n thinkpixelag-poc wait --for=condition=complete \
  -f /private/poc/manifests/bootstrap.json --timeout=300s
kubectl apply -f /private/poc/manifests/application.json
kubectl -n thinkpixelag-poc rollout status deployment/api --timeout=300s
kubectl -n thinkpixelag-poc rollout status deployment/console --timeout=300s
```

Skip the last command when console is omitted. The bootstrap Job migrates before
provisioning; API startup never migrates. Its name includes the immutable image
digest, and bootstrap replay cannot replace existing authority. Pods run as UID
65532 without privilege escalation or service-account tokens. Only bootstrap/API
mount the signing-key PVC; the console mounts public CA material alone.
Provision/readiness failures remain failures; inspect Job events and private logs
without printing credentials. Existing completed Jobs/PVCs can be retained.

## Use and operate

Open the console HTTPS origin and log in through your IdP. Follow the
[policy/approval and configuration walkthrough](../../../console/README.md).
Install the [harness helper](../../../integrations/harness/README.md) with the AG
origin and a protected caller token, then retrieve guidance and admit/read/cancel
a Run. AG remains the harness entry point; no AR execution is implied.

Use one console worker/replica. `kubectl scale deployment/console --replicas=0`
(in the PoC namespace) leaves AG usable; restoring it requires fresh console
login. Before upgrading, back up PostgreSQL and the local signing-key PVC, retain
bootstrap/trust configuration, apply forward migrations through a reviewed Job,
and restart API/console with the new immutable images. Do not down-migrate or
restart an old database writer to roll back. Use
[monitoring](../../operations/monitoring.md), [recovery](../../operations/backup-recovery.md)
and [operator mapping recovery](../../operations/bootstrap.md#recover-administrator-mappings).
The old fixture-based `deploy/demo/kubernetes.py` remains available for existing
rc.1 evaluations; do not reuse that database as a fresh bootstrap target.
