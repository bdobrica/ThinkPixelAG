# Install AG and make your first Run

This installs a persistent local AG service and uses its real HTTP API.
You need Docker Engine with Compose v2, Bash, curl, jq and OpenSSL. You do not
need Go, a Kubernetes cluster or an account with an identity provider.
The first launch downloads pinned images and builds a small example support
container; the AG service itself uses the published `0.1.0-rc.1` image.

## 1. Install and start

```sh
git clone https://github.com/bdobrica/ThinkPixelAG.git
cd ThinkPixelAG
./deploy/demo/ag up
```

If you already cloned the repository, run the last command from its root.
Wait for `AG is ready at http://127.0.0.1:18080`. To use another free port on
first setup, run `AG_PORT=18081 ./deploy/demo/ag up`.

The installation creates PostgreSQL, OPA, a local OIDC issuer/evidence receiver,
a sample tenant with an approved agent version and signed policy, and AG with
governed routes enabled. Credentials are generated into private
`~/.local/state/thinkpixelag-demo/environment`; database, identity and evidence state use named Docker
volumes. Only AG's port is published, bound to loopback. `/readyz` returns 200.

## 2. Authenticate and discover an agent

```sh
export AG_URL=http://127.0.0.1:18080
export AG_TOKEN="$(./deploy/demo/ag token)"

curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" "$AG_URL/v1/agents" \
  | tee /tmp/ag-agents.json | jq .

export AG_AGENT_ID="$(jq -r '.items[0].id' /tmp/ag-agents.json)"
```

The sample caller has the `agent-invoker` role. The list contains the approved
sample agent (`ops010-agent`, the retained fixture's name). The helper issues
a 15-minute caller token through an operator-only command; no token-issuing
endpoint is exposed on AG. Run the token command again when it expires.

## 3. Request a governed Run

```sh
export AG_ADMISSION_KEY="admit-$(openssl rand -hex 16)"

curl --fail-with-body --silent --show-error \
  -X POST "$AG_URL/v1/agents/$AG_AGENT_ID/runs" \
  -H "Authorization: Bearer $AG_TOKEN" \
  -H "Idempotency-Key: $AG_ADMISSION_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"objective":"Summarize incident INC-42","constraints":{"max_execution_time_seconds":300,"max_llm_tokens":1000,"max_tool_calls":10}}' \
  | tee /tmp/ag-run.json | jq .

export AG_RUN_ID="$(jq -r '.id' /tmp/ag-run.json)"
```

AG returns **201 Created** with an `ADMITTED` Run, the resolved version and its
resource envelope. Repeating that exact POST with the same idempotency key
returns the same Run. A changed request with that key is rejected.

**Admission is governance, not execution.** The sample agent is a provisioned
manifest, not a running incident assistant. AG has authorized and recorded the
Run; this RC does not yet supply the complete harness-execution integration
needed to perform the objective and report completion. The harness's platform
entry point is AG; configuring a direct AR connection is not the missing step.
See [harness integration and current capabilities](operations/integrations.md).

### Optional caller limits and the next candidate

`constraints` requests stricter caller limits; AG owns the effective policy.
The published `0.1.0-rc.1` image above predates the RC-102 inheritance fix, so its
example supplies explicit limits. Do not interpret omitted fields in that image
as safely inheriting all limits. On a build containing RC-102, the normal request
needs only the objective:

```sh
curl --fail-with-body --silent --show-error \
  -X POST "$AG_URL/v1/agents/$AG_AGENT_ID/runs" \
  -H "Authorization: Bearer $AG_TOKEN" \
  -H "Idempotency-Key: admit-$(openssl rand -hex 16)" \
  -H 'Content-Type: application/json' \
  --data '{"objective":"Summarize incident INC-42"}' \
  | tee /tmp/ag-run.json | jq .
export AG_RUN_ID="$(jq -r '.id' /tmp/ag-run.json)"
```

AG inherits the approved agent and deployment ceilings, narrowed by the active
policy. Optional caller constraints can only reduce these limits. This source
fix does not replace the pinned release image; the next release packaging task
will update the installation examples to that qualified image.

## 4. Inspect and cancel it

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" "$AG_URL/v1/runs/$AG_RUN_ID" \
  | tee /tmp/ag-current-run.json | jq .

AG_STATE_VERSION="$(jq -r '.state_version' /tmp/ag-current-run.json)"
curl --fail-with-body --silent --show-error \
  -X POST "$AG_URL/v1/runs/$AG_RUN_ID/cancel" \
  -H "Authorization: Bearer $AG_TOKEN" \
  -H "Idempotency-Key: cancel-$(openssl rand -hex 16)" \
  -H 'Content-Type: application/json' \
  --data "{\"reason_code\":\"caller.request\",\"expected_state_version\":$AG_STATE_VERSION}" | jq .
```

The response is **200 OK** with state `CANCELLED`. Admission and cancellation
are durable database operations; the example receiver also stores governance
evidence. Run state remains available after restarting the containers.

## 5. Keep using the installation

```sh
./deploy/demo/ag status
./deploy/demo/ag logs
./deploy/demo/ag stop
./deploy/demo/ag up
```

`stop` preserves all containers and volumes. `up` reuses the same credentials,
identities and approved data. Do not remove the volumes or the environment file to restart.

Use the [local installation reference](examples/local-sandbox/README.md) for
configuration, storage and troubleshooting; the [Kubernetes team PoC](examples/team-poc/README.md)
for a shared installation; and [configuration.md](configuration.md) for every
runtime setting. This local setup uses sample identity/policy provisioning and
is not a production identity or administrative bootstrap service.

For the current source candidate, [install the harness helper](../integrations/harness/README.md)
and append the bootstrap snippet to connect an existing harness to dynamic guidance.
