#!/usr/bin/env bash
set -euo pipefail

kind_bin="${KIND:-kind}"
kubectl_bin="${KUBECTL:-kubectl}"
cluster="thinkpixelag-phase8"
image="${IMAGE:-thinkpixelag:phase8}"
cleanup() { "$kind_bin" delete cluster --name "$cluster" >/dev/null 2>&1 || true; }
trap cleanup EXIT

"$kind_bin" create cluster --name "$cluster" --wait 120s
"$kind_bin" load docker-image --name "$cluster" "$image"
"$kubectl_bin" create secret generic thinkpixelag-postgres-test \
  --from-literal=POSTGRES_DB=thinkpixelag_test --from-literal=POSTGRES_USER=thinkpixelag_test \
  --from-literal=POSTGRES_PASSWORD=phase8_disposable_only
"$kubectl_bin" create secret generic thinkpixelag-runtime \
  --from-literal=THINKPIXELAG_DATABASE_URL='postgresql://thinkpixelag_test:phase8_disposable_only@thinkpixelag-postgres:5432/thinkpixelag_test?sslmode=disable'
"$kubectl_bin" create secret generic thinkpixelag-migration \
  --from-literal=THINKPIXELAG_DATABASE_URL='postgresql://thinkpixelag_test:phase8_disposable_only@thinkpixelag-postgres:5432/thinkpixelag_test?sslmode=disable'
"$kubectl_bin" apply -k deploy/kubernetes/test
"$kubectl_bin" rollout status deployment/thinkpixelag-postgres --timeout=120s
"$kubectl_bin" wait --for=condition=complete job/thinkpixelag-migrate --timeout=120s
# Readiness is intentionally fail closed until at least one validated active
# policy and its authoritative revocation head have been reconciled.
"$kubectl_bin" exec deployment/thinkpixelag-postgres -- env PGPASSWORD=phase8_disposable_only \
  psql -U thinkpixelag_test -d thinkpixelag_test -v ON_ERROR_STOP=1 -c \
  "INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES('019feba6-b9bb-7fff-bfff-fffffffffff1','phase8-smoke','Phase 8 smoke',now(),now()); INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES('019feba6-b9bb-7fff-bfff-fffffffffff2','019feba6-b9bb-7fff-bfff-fffffffffff1','https://phase8.test','operator','HUMAN',now()); INSERT INTO policy_bundles(id,tenant_id,channel,content_digest,contract_version,artifact_revision,bundle,signature,signer_key_id,signer_key_version,signature_algorithm,validation_status,created_by,created_at) VALUES('019feba6-b9bb-7fff-bfff-fffffffffff3','019feba6-b9bb-7fff-bfff-fffffffffff1','stable','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','thinkpixelag.authorization/v1alpha1',1,'test','test','phase8-test-key','1','ED25519','VALIDATED','019feba6-b9bb-7fff-bfff-fffffffffff2',now()); INSERT INTO policy_activations(id,tenant_id,channel,policy_bundle_id,activation_version,actor_principal_id,reason_code,activated_at) VALUES('019feba6-b9bb-7fff-bfff-fffffffffff4','019feba6-b9bb-7fff-bfff-fffffffffff1','stable','019feba6-b9bb-7fff-bfff-fffffffffff3',1,'019feba6-b9bb-7fff-bfff-fffffffffff2','phase8.smoke',now());"
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
"$kubectl_bin" port-forward service/thinkpixelag 18080:8080 >/tmp/thinkpixelag-port-forward.log 2>&1 &
forward_pid=$!
trap 'kill "$forward_pid" >/dev/null 2>&1 || true; cleanup' EXIT
for _ in {1..40}; do curl --fail --silent http://127.0.0.1:18080/livez >/dev/null && break; sleep .25; done
curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null
curl --fail --silent http://127.0.0.1:18080/metrics | grep -q thinkpixelag_build_info
"$kubectl_bin" get horizontalpodautoscaler/thinkpixelag >/dev/null
node=$($kubectl_bin get nodes -o jsonpath='{.items[0].metadata.name}')
if "$kubectl_bin" drain "$node" --ignore-daemonsets --delete-emptydir-data \
  --pod-selector=app.kubernetes.io/name=thinkpixelag --timeout=10s; then
  printf 'cluster-smoke: disruption unexpectedly bypassed the PodDisruptionBudget\n' >&2
  exit 1
fi
"$kubectl_bin" uncordon "$node"
"$kubectl_bin" delete pod -l app.kubernetes.io/name=thinkpixelag --wait=false
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
"$kubectl_bin" set env deployment/thinkpixelag THINKPIXELAG_LOG_LEVEL=debug
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
"$kubectl_bin" rollout undo deployment/thinkpixelag
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
"$kubectl_bin" delete -k deploy/kubernetes/test --wait=true
printf 'cluster-smoke: install, migration, restricted runtime, workflow probes, eviction, rolling upgrade/rollback, and uninstall passed\n'
