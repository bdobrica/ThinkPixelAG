#!/usr/bin/env bash
set -euo pipefail

kind_bin="${KIND:-kind}"
kubectl_bin="${KUBECTL:-kubectl}"
cluster="thinkpixelag-resilience"
image="${IMAGE:-thinkpixelag:phase8}"
port="${PORT:-18081}"
forward_pid=""

cleanup() {
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" >/dev/null 2>&1 || true; fi
  "$kind_bin" delete cluster --name "$cluster" >/dev/null 2>&1 || true
}
trap cleanup EXIT

start_forward() {
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" >/dev/null 2>&1 || true; fi
  "$kubectl_bin" port-forward service/thinkpixelag "${port}:8080" >/tmp/thinkpixelag-resilience-port-forward.log 2>&1 &
  forward_pid=$!
}

wait_status() {
  local path="$1" expected="$2"
  for _ in {1..80}; do
    if ! kill -0 "$forward_pid" >/dev/null 2>&1; then start_forward; fi
    if [[ "$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:${port}${path}" || true)" == "$expected" ]]; then
      return 0
    fi
    sleep .25
  done
  printf 'cluster-resilience: %s did not reach HTTP %s\n' "$path" "$expected" >&2
  return 1
}

db_exec() {
  "$kubectl_bin" exec deployment/thinkpixelag-postgres -- env PGPASSWORD=phase8_disposable_only \
    psql -U thinkpixelag_test -d thinkpixelag_test -v ON_ERROR_STOP=1 "$@"
}

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
db_exec -c "INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES('029feba6-b9bb-7fff-bfff-fffffffffff1','resilience','Resilience',now(),now()); INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES('029feba6-b9bb-7fff-bfff-fffffffffff2','029feba6-b9bb-7fff-bfff-fffffffffff1','https://resilience.test','operator','HUMAN',now()); INSERT INTO policy_bundles(id,tenant_id,channel,content_digest,contract_version,artifact_revision,bundle,signature,signer_key_id,signer_key_version,signature_algorithm,validation_status,created_by,created_at) VALUES('029feba6-b9bb-7fff-bfff-fffffffffff3','029feba6-b9bb-7fff-bfff-fffffffffff1','stable','sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','thinkpixelag.authorization/v1alpha1',1,'test','test','resilience-key','1','ED25519','VALIDATED','029feba6-b9bb-7fff-bfff-fffffffffff2',now()); INSERT INTO policy_activations(id,tenant_id,channel,policy_bundle_id,activation_version,actor_principal_id,reason_code,activated_at) VALUES('029feba6-b9bb-7fff-bfff-fffffffffff4','029feba6-b9bb-7fff-bfff-fffffffffff1','stable','029feba6-b9bb-7fff-bfff-fffffffffff3',1,'029feba6-b9bb-7fff-bfff-fffffffffff2','resilience.fixture',now());"
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
start_forward
wait_status /readyz 200

# PostgreSQL's test-only pre-authentication delay exceeds the configured health
# timeout for newly established connections. Governed readiness must fail closed
# while process liveness remains healthy, then recover after the delay is reset.
postgres_pod=$("$kubectl_bin" get pod -l app.kubernetes.io/name=thinkpixelag-postgres -o jsonpath='{.items[0].metadata.name}')
db_exec -c "ALTER SYSTEM SET pre_auth_delay='5s'"
db_exec -c "SELECT pg_reload_conf()"
db_exec -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid()"
wait_status /readyz 503
wait_status /livez 200
db_exec -c "ALTER SYSTEM RESET pre_auth_delay"
db_exec -c "SELECT pg_reload_conf()"
db_exec -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid()"
wait_status /readyz 200

# Crashing the database process exercises connection failure and reconnection
# while retaining this fixture's Pod-scoped data volume. Managed-service
# promotion/failover remains an environment-owned game-day action.
"$kubectl_bin" exec "$postgres_pod" -- kill -KILL 1 >/dev/null 2>&1 || true
"$kubectl_bin" wait --for=condition=ready "pod/${postgres_pod}" --timeout=120s
wait_status /readyz 200

# A process crash and a rolling restart must preserve service availability with
# the two-replica/PDB configuration and converge back to the declared replica set.
api_pod=$("$kubectl_bin" get pod -l app.kubernetes.io/name=thinkpixelag -o jsonpath='{.items[0].metadata.name}')
"$kubectl_bin" delete pod "$api_pod" --wait=false
wait_status /readyz 200
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
"$kubectl_bin" rollout restart deployment/thinkpixelag
for _ in {1..20}; do wait_status /livez 200; sleep .25; done
"$kubectl_bin" rollout status deployment/thinkpixelag --timeout=120s
wait_status /readyz 200

printf 'cluster-resilience: DB latency/process crash, API crash, and rolling restart passed\n'
