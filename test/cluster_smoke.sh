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
