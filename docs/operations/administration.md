# Administer policy through the API

For a clean source-built installation, start with [operator bootstrap](bootstrap.md).

These operations require an installation using the explicit local-development
profile and a `policy-admin` caller token. They exercise the same APIs intended
for the optional console; no console is needed. Production KMS/HSM and independent
approval-provider qualification remain separate. See the
[configuration reference](../configuration.md#local-policy-administration-unreleased)
and [API contract](../contracts/policy-administration.md).

Keep `AG_TOKEN` in your trusted shell/host, not harness instructions. The commands
below assume `AG_URL` and an authenticated administrator token are already set.
Initial installation is covered by the protected bootstrap command.

## Edit, validate and promote

Create a private working directory and capture the draft's identity/revision:

```sh
AG_WORK_DIR=$(mktemp -d)
cp policies/authorization.rego "$AG_WORK_DIR/authorization.rego"
printf '\n# Local operator review revision 2\n' >> "$AG_WORK_DIR/authorization.rego"
jq -n --rawfile source "$AG_WORK_DIR/authorization.rego" \
  '{source:$source,expected_revision:0}' > "$AG_WORK_DIR/draft-request.json"
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: draft-$(openssl rand -hex 16)" \
  --data-binary @"$AG_WORK_DIR/draft-request.json" \
  "$AG_URL/v1/admin/policy-drafts" > "$AG_WORK_DIR/draft.json"
AG_DRAFT_ID=$(jq -r .id "$AG_WORK_DIR/draft.json")
AG_DRAFT_DIGEST=$(jq -r .digest "$AG_WORK_DIR/draft.json")

curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: validate-$(openssl rand -hex 16)" --data '{"revision":1}' \
  "$AG_URL/v1/admin/policy-drafts/$AG_DRAFT_ID/validation"
```

The example adds a review comment to create new signed bytes while retaining
the baseline decisions. For a real policy change, edit and review that private
source before uploading it.

To revise, `PUT` new source with the current `expected_revision` to the draft
URL. A stale revision returns a conflict. Save the returned revision/digest;
validation and promotion can target that exact immutable revision. Invalid Rego
can be kept as a draft but cannot be promoted.

```sh
jq -n --arg digest "$AG_DRAFT_DIGEST" \
  '{revision:1,digest:$digest,artifact_revision:2}' > "$AG_WORK_DIR/promotion.json"
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: promote-$(openssl rand -hex 16)" \
  --data-binary @"$AG_WORK_DIR/promotion.json" \
  "$AG_URL/v1/admin/policy-drafts/$AG_DRAFT_ID/promotions"
```

Choose the artifact revision deliberately; the example assumes revision 1 was
used for the initial policy. Promotion signs/stores the reviewed source. It does
not activate it. If an identical digest already exists with different signature
metadata, AG returns a conflict; reuse its existing artifact instead.

## Activate and review history

For an artifact that has never been active:

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: activate-$(openssl rand -hex 16)" \
  --data '{"channel":"stable","reason_code":"policy.activate","approval_reference":"not-required"}' \
  "$AG_URL/v1/admin/policies/$AG_DRAFT_DIGEST/activations"
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AG_TOKEN" "$AG_URL/v1/admin/policy-activations/current"
```

`GET /v1/admin/policy-activations` returns history, including `policy_epoch` for
ordering. Follow `next_cursor` until it is empty. Requests on all replicas use
current PostgreSQL authority and the matching immutable OPA module. If policy
loading fails, activation does not commit. If an in-flight evaluation overlaps
activation, it fails closed and can be retried.

## Roll back with another operator

1. Read the current epoch and choose the target digest from history.
2. `POST /v1/admin/policies/{digest}/rollback-approvals` with
   `expected_policy_epoch`, `lifetime_seconds` (up to 3600) and
   `reason_code: "policy.rollback"`. Save the returned approval ID/digest.
3. A different authenticated administrator inspects
   `GET /v1/admin/approvals/{id}` and posts `{"approved":true}` to
   `/v1/admin/approvals/{id}/decisions`, with their own bearer token and a new
   idempotency key. The requester cannot approve the request.
4. The requester activates the target using the approval ID as
   `approval_reference`. Approval consumption, new epoch and evidence commit
   together. Expired, reused or incorrectly bound approvals fail.

Use the same idempotency key and identical body when retrying an uncertain
write. Generate a new key for a genuinely new operation. An approval does not
survive a change to the action/epoch it approved; request another approval.

## External role mappings

With provisioned `role_mappings_mode: "api"`, read
`GET /v1/admin/role-mappings`. Save the complete desired mapping with its current
`expected_revision` to a private JSON file. PUT that file to the same endpoint
with a new Idempotency-Key. Non-expanding edits use
`"approval_reference":"not-required"`. To add administrative authority, POST
the proposed body to `/v1/admin/role-mappings/approvals`, have a second operator
approve its ID through `/v1/admin/approvals/{id}/decisions`, then PUT the unchanged
mapping/revision with that approval ID. Concurrent changes require a new review.
Removed bindings stop working on the next verified request on every replica.

File-managed mappings remain readable but reject writes. Use the [protected bootstrap/recovery command](bootstrap.md) to initialize a
clean tenant or recover mappings without an unauthenticated reset endpoint.

## OPA connection configuration

Keep `integrations_mode: "file"` for deployment-owned settings. For API ownership,
provision the initial record and set `integrations_mode: "api"` in runtime JSON,
with `opa_allowed_origins` containing the exact reviewed origins. Optional
`opa_secret_files` maps opaque aliases to private mounted token files. Those
paths and the allowlist are restart-managed and never accepted from API input.

Read `GET /v1/admin/integrations/opa`, then PUT a private JSON file such as:

```json
{"expected_revision":1,"connection":{"endpoint":"http://127.0.0.1:8181","token_reference":""}}
```

Use your operator bearer token and a fresh Idempotency-Key. This validates the
current signed policy at the destination before saving revision 2. Other replicas
read the new record on their next request; a bad candidate leaves revision 1
unchanged. `GET /v1/admin/integrations/opa/status` reports the bounded active-policy
check result. See the [field catalog and recovery limits](../contracts/managed-configuration.md#supported-integration-catalog).
