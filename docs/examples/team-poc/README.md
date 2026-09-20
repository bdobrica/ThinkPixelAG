# Team Kubernetes PoC

This installs a usable AG evaluation in a dedicated namespace: PostgreSQL,
explicit schema migration and sample provisioning, OPA, a sample OIDC/evidence
receiver, and the published AG service. Colleagues can access it through their
Kubernetes credentials and a local port-forward, then use the same HTTP calls
as the [quick start](../../quickstart.md).

You need kubectl access to a cluster with NetworkPolicy enforcement and a default
dynamic StorageClass, plus Python 3 and Docker Buildx on the build machine.
Reserve three 1 GiB persistent volumes and capacity for four small services.
With node-local storage, shared identity/trust volumes constrain placement;
this example is a single-instance PoC, not HA or a production topology.

## 1. Publish the example support image

AG uses its existing published immutable image. The small provisioning/identity
support image is built from this checkout and must be reachable by your cluster.
Choose a repository your account can push to; do not use the example value
literally:

```sh
export DEMO_IMAGE=quay.io/YOUR_ACCOUNT/thinkpixelag-demo-support
# Authenticate with your registry's normal credential helper first.
docker buildx build --platform linux/amd64,linux/arm64 \
  -f deploy/demo/Dockerfile -t "$DEMO_IMAGE:eval" --push .
docker buildx imagetools inspect "$DEMO_IMAGE:eval"
```

Copy the reported index digest into `DEMO_DIGEST`. All subsequent configuration
uses the immutable digest, not the `eval` tag:

```sh
export DEMO_DIGEST=sha256:REPLACE_WITH_THE_REPORTED_INDEX_DIGEST
python3 deploy/demo/kubernetes.py \
  --support-image "$DEMO_IMAGE@$DEMO_DIGEST" \
  --output "$HOME/.local/state/thinkpixelag-poc"
```

The renderer generates private credentials and four installation stages in
`~/.local/state/thinkpixelag-poc`, mode 0600. Re-running it with the same output directory
retains the credentials. Do not commit or print those generated manifests.
Use a registry-readable image or configure the namespace's image-pull secret
before starting pods if the repository is private.

## 2. Install in dependency order

```sh
kubectl apply -f "$HOME/.local/state/thinkpixelag-poc/foundation.json"
kubectl -n thinkpixelag-poc rollout status deployment/postgres --timeout=180s

kubectl apply -f "$HOME/.local/state/thinkpixelag-poc/provision.json"
kubectl -n thinkpixelag-poc wait --for=condition=complete \
  -f "$HOME/.local/state/thinkpixelag-poc/provision.json" --timeout=180s

kubectl apply -f "$HOME/.local/state/thinkpixelag-poc/dependencies.json"
kubectl -n thinkpixelag-poc rollout status deployment/identity --timeout=180s
kubectl -n thinkpixelag-poc rollout status deployment/opa --timeout=180s

kubectl apply -f "$HOME/.local/state/thinkpixelag-poc/application.json"
kubectl -n thinkpixelag-poc rollout status deployment/api --timeout=180s
```

The provision Job migrates first, creates the sample identities/approved policy
once, and preserves them on subsequent runs. API startup never migrates.
Namespace policy restricts traffic to this evaluation and cluster DNS. Pods
run without service-account tokens or privilege escalation. The API uses the
sample CA and cannot mount the issuer private-key volume.

## 3. Use AG

Keep this running in one terminal:

```sh
kubectl -n thinkpixelag-poc port-forward service/api 18080:8080
```

In another terminal, obtain a sample caller token through the operator-controlled
issuer container:

```sh
export AG_URL=http://127.0.0.1:18080
export AG_TOKEN="$(kubectl -n thinkpixelag-poc exec deployment/identity -- /bootstrap token /state)"
curl --fail-with-body -H "Authorization: Bearer $AG_TOKEN" "$AG_URL/v1/agents"
```

Continue at step 3 of the [quick start](../../quickstart.md), setting
`AG_AGENT_ID` from the discovered agent. Admission, replay, read and cancellation
use exactly the same API. Stopping port-forward does not stop AG or remove data.
Do not delete the namespace or PVCs when pausing your evaluation.

## 4. Adapt for a company deployment

The sample issuer/provisioner and local-mode internal transports are deliberate
PoC choices. Before exposing AG as a company service, use private TLS ingress,
managed database/identity/secret infrastructure, an independently administered
evidence receiver, and the [configuration reference](../../configuration.md).
A real IdP needs the documented audience, UUID principal/tenant claims and role
mappings; it does not automatically provision the corresponding governance data.

Production registry/policy administration and the complete AG-facing harness
integration remain implementation gaps. The [integration guide](../../operations/integrations.md)
spells them out. Do not turn sample fixture signing or provisioning into an
unreviewed production bootstrap. Follow [monitoring](../../operations/monitoring.md)
and [backup/recovery](../../operations/backup-recovery.md) when evaluating durable
state; promotion requires the production qualification recorded in the ADRs.
