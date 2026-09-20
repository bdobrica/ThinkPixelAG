# Provision a source-built administration candidate

This is the local-development administration path in
[ADR-0016](../adr/0016-local-development-policy-promotion.md). It creates real
persistent AG state through application services. The published `0.1.0-rc.1`
image predates these commands and APIs; build this checkout. No console is needed.

## Prepare the deployment

You need Go (the repository pins its toolchain), PostgreSQL with a dedicated AG
database, OPA with its management API accessible only to AG/operators, and an
OIDC issuer. Use the [installation guide](installation.md) for dependencies and
[configuration reference](../configuration.md) for service configuration.
Keep the database and key directory on persistent storage.

Configure the IdP to issue access tokens for your AG audience with a UUIDv7
`sub`, a UUIDv7 `tenant_id`, and a `roles` array. AG does not create IdP accounts.
Create two operator identities for independent approvals; the first also owns
the sample agent and the second sponsors it. Assign both external roles
`operators`, `registrars`, and `users` for this local walkthrough. They map to
policy-admin, registry-admin, and agent-invoker respectively. An ordinary caller
needs only `users`. Use your issuer's protected login/token tooling; never give
administrative tokens to the harness.

```sh
make build operator
go build -o .cache/bin/thinkpixelag-migrate ./cmd/thinkpixelag-migrate
export THINKPIXELAG_ENVIRONMENT=local
# Supply THINKPIXELAG_DATABASE_URL through your existing private environment.
.cache/bin/thinkpixelag-migrate --directory migrations
umask 077
mkdir -p "$HOME/.local/state/thinkpixelag-admin"
export AG_ADMIN_STATE="$HOME/.local/state/thinkpixelag-admin"
.cache/bin/thinkpixelag-operator new-id
```

Use `new-id` to allocate a tenant ID, two operator IDs, and an agent ID. Configure
the corresponding tenant/subject IDs at the IdP. Write a private
`$AG_ADMIN_STATE/bootstrap.json` from this template, replacing the identifiers,
issuer, and OPA origin with your actual values:

```json
{
  "tenant_id": "01990000-0000-7000-8000-000000000001",
  "slug": "local-administration",
  "issuer": "https://your-issuer.example",
  "principals": [
    "01990000-0000-7000-8000-000000000002",
    "01990000-0000-7000-8000-000000000003"
  ],
  "role_mappings": {
    "operators": "policy-admin",
    "registrars": "registry-admin",
    "users": "agent-invoker"
  },
  "opa": {"endpoint": "http://127.0.0.1:8181", "token_reference": ""},
  "channel": "stable",
  "agent_id": "01990000-0000-7000-8000-000000000004",
  "agent_name": "local-evaluation",
  "manifest": {
    "schema_version": 1,
    "image": "registry.example/evaluation@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "models": [], "tools": [], "skills": [], "subagents": [],
    "limits": {"max_execution_time_seconds": 300, "max_llm_tokens": 1000, "max_tool_calls": 10}
  }
}
```

The shown image is explicitly a **metadata-only evaluation placeholder**. AG
can demonstrate admission with it; no image is pulled and no execution is
claimed. Substitute your real immutable agent image for an actual integration.
AR/harness execution remains separate from this governance setup.

## Bootstrap once, then start AG

Review `policies/authorization.rego` and the snapshot before provisioning:

```sh
.cache/bin/thinkpixelag-operator bootstrap \
  --spec "$AG_ADMIN_STATE/bootstrap.json" \
  --policy policies/authorization.rego \
  --key "$AG_ADMIN_STATE/policy.key" \
  --opa-origin http://127.0.0.1:8181 \
  > "$AG_ADMIN_STATE/provisioned.json"
```

If OPA requires a token, set a nonempty `opa.token_reference` alias in the spec
and add `--opa-token-file /private/path/to/token`. The command accepts only the
explicitly supplied origin and private token file. Keep the signing key (0600)
in its private directory (0700); it is generated once and reopened on reruns.

Provisioning atomically creates the seven closed root resource-dimension definitions
(scale zero, nonnegative coefficients up to 2^53; SUM consumables and MAX
structural dimensions), tenant/principals, initial mapping and OPA
revisions, signed policy/activation, and an approved sample agent. The ordinary
agent registry and policy approval services validate the agent/version. Output
contains IDs and digests only. Identical reruns return the recorded result;
changed snapshots or preexisting tenants without a matching receipt are rejected.
There is no force/rebootstrap flag. Older source installations missing the
resource catalog can rerun the identical command with `--repair-resource-catalog`.
This explicit repair inserts only missing definitions, rejects conflicting
existing definitions and emits `operator.bootstrap.resources` evidence. It
changes no policy, mapping, agent, grant or original bootstrap receipt. A failed database transaction creates no
partial tenant authority; an OPA compilation or new local key may remain for reuse.

Set these runtime fields in addition to your existing ceilings:

```json
{
  "policy_channel": "stable",
  "authority_constraints": {"max_execution_time_seconds": 300, "max_llm_tokens": 1000, "max_tool_calls": 10},
  "local_policy_key": "/absolute/private/path/policy.key",
  "role_mappings_mode": "api",
  "integrations_mode": "api",
  "opa_allowed_origins": ["http://127.0.0.1:8181"],
  "opa_secret_files": {}
}
```

Use the actual key path. If using a token alias, map it to the same private token
file under `opa_secret_files`. The API reads that key; the optional UI and harness
do not. Set `THINKPIXELAG_RUNTIME_FILE`, `THINKPIXELAG_CURSOR_HMAC_KEY` (at least
32 secret bytes), `THINKPIXELAG_OIDC_ISSUER_URL`, `THINKPIXELAG_OIDC_AUDIENCE`, and
`THINKPIXELAG_DATABASE_URL` through your protected environment, then run:

```sh
.cache/bin/thinkpixelag
```

Check the exact OIDC variable names in the configuration reference; issuer and
audience must match the bootstrap/IdP configuration. Database migration and
provisioning precede API startup. Existing deployment evidence-sink settings
still apply; configure a receiver to drain the durable audit outbox.

Authenticate as an operator, then use the [administration walkthrough](administration.md)
for drafts, promotion, rollback, mappings and OPA settings. Existing
`GET /v1/agents` and `GET /v1/agents/{id}` expose authorized agent discovery.
`GET /v1/runs?limit=50` lists only Runs passing the existing per-Run read check;
`GET /v1/runs/{id}` reads a selected Run. A filtered page can be empty while
containing a continuation cursor. Policy/source/history and integration views
use their own administrative authorization.

The source runtime now composes the published `POST /v1/admin/agents` and
`POST /v1/admin/agents/{id}/versions` contracts, with atomic replay/evidence.
The existing version approval endpoint remains the separate activation of
registry eligibility. Use distinct provisioned owner/sponsor identities and the
canonical manifest digest; these APIs do not create IdP accounts or execute images.

## Recover administrator mappings

Keep two independently authenticated operator identities available. If IdP group
changes leave no effective policy administrator, local database credentials plus
the pinned deployment OIDC configuration permit the following protected commands.
There is no network recovery endpoint and no special OIDC recovery role.

Save the exact desired mapping and current expected revision in private
`$AG_ADMIN_STATE/recovery.json`, in the same format as a
[role-mapping update](../contracts/managed-configuration.md). Save each operator's
OIDC token to a separate private file. For an administrative expansion:

```sh
.cache/bin/thinkpixelag-operator recovery-request \
  --proposal "$AG_ADMIN_STATE/recovery.json" --token-file /private/requester.token \
  --idempotency-key recovery-request-unique-key
.cache/bin/thinkpixelag-operator recovery-approve \
  --approval APPROVAL_UUID --token-file /private/second-operator.token \
  --idempotency-key recovery-approval-unique-key
# Put APPROVAL_UUID in recovery.json's approval_reference; retain the exact mapping/revision.
.cache/bin/thinkpixelag-operator recovery-apply \
  --proposal "$AG_ADMIN_STATE/recovery.json" --token-file /private/requester.token \
  --idempotency-key recovery-apply-unique-key
```

Use `--channel` if not `stable`. Approval expires after ten minutes; requester and
approver must differ, be verified by the pinned IdP, and be active provisioned
principals of the same tenant. Applying consumes the exact digest-bound approval
with immutable mapping history and `operator.role_mappings.recovery.*` evidence.
Operator evidence identifies `deployment-operator` authority and has no fabricated
OPA policy-decision reference; active policy metadata is recorded as context.
A non-expanding edit uses `not-required`; last-admin and closed-role checks still
apply. There is no approval bypass if the second identity or IdP is unavailable.

This is a local software-key/operator-custody path, **not production KMS/HSM or
independent approval-provider qualification**. Back up the key, deployment trust
configuration and database. Reopening the database/key preserves state and replay
receipts. Do not reset the database merely to restart AG.
