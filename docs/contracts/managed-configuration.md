# Managed governance configuration

[ADR-0015](../adr/0015-managed-governance-configuration.md) owns this contract.
The additive `/v1/admin/role-mappings` API is specified in
[OpenAPI](../../api/openapi/thinkpixelag.yaml). This is AG role mapping, not IdP
account or membership administration.

Runtime `role_mappings_mode` selects `file` (default) or `api` once at startup.
File mode reads `THINKPIXELAG_OIDC_ROLE_MAPPINGS` and rejects API writes. API mode
requires the signed administration profile and a provisioned tenant/issuer
mapping; missing storage fails closed with no file fallback. Switching modes
requires an operator-reviewed snapshot and restart.

GET returns mode, configured issuer, revision and mappings. PUT replaces the
complete mapping at `expected_revision` and requires an Idempotency-Key. Only
`policy-admin`, additionally authorized by the active policy through
`role_mappings.read/manage`, can use these operations. Only agent-invoker,
registry-admin, resource-admin, policy-admin and revocation-admin are assignable.
At most 256 external role names of 128 characters are accepted. The last
policy-admin mapping cannot be removed; this does not prove live IdP membership.

Adding any administrative binding requires POST
`/v1/admin/role-mappings/approvals` with the exact proposed update and
`approval_reference: "not-required"`. An independent current policy operator
records a decision through the [approval API](policy-administration.md). PUT
then supplies that approval's ID. The EMERGENCY_EXPANSION digest binds tenant,
issuer, expected revision and the complete mapping; successful application
consumes it atomically with the revision, replay response and audit/outbox evidence.
Non-expanding writes use `not-required`.

Both replicas and existing tokens resolve the current mapping on **every**
verified OIDC request. There is no mapping or authorization decision cache in
this runtime profile. Issuer/audience/signature/time/tenant checks precede the
lookup. Policy inputs include the mapping revision and effective roles.
Administration transactions recheck the mapping revision under the tenant lock;
an intervening edit requires fresh authentication. No database connectivity means
no API-managed authority, even while the token itself remains valid.

The protected operator provisioning/recovery command is delivered in RC-107.
There is no unauthenticated bootstrap/reset endpoint or standing recovery role;
recovery must preserve independent expansion approval. Do not enable API mode
on a fresh deployment before provisioning its initial state.

## Supported integration catalog

The versioned API exposes `GET/PUT /v1/admin/integrations/opa` and
`GET /v1/admin/integrations/opa/status`. `integrations.read/manage` require the
current policy-admin role and active policy authorization.

| Adapter / field | Ownership and reload | Meaning |
|---|---|---|
| OPA `connection.endpoint` | `integrations_mode: file/api`; API reload on next request | Exact HTTP(S) origin, no path, query, credentials or fragment; must match deployment `opa_allowed_origins`. |
| OPA `connection.token_reference` | API revision; trusted resolution on each request | Empty for no token, otherwise an alias in deployment `opa_secret_files`; never a token or caller-supplied file path. |
| OIDC issuer/audience, database, listeners, signing keys and allowlists | Deployment file; restart | Trust roots are not editable through the API. |
| TG, LLMGW, AR, MEM and marketplace connection editing | Unsupported | Existing wire contracts do not imply a runtime connection adapter or an editable setting. |

Mode selection defaults to file and is fixed at startup. API mode requires
signed policy administration and a provisioned configuration revision. Each
request reads the current tenant configuration; storage failures do not fall
back to a previous replica snapshot or deployment token. File mode uses the
existing deployment OPA URL/token, exposes no credential and rejects updates.

PUT uses expected revision and idempotency. Before committing, AG verifies the
active policy signature, compiles/checks the exact artifact at the candidate OPA,
and validates a decision envelope under the same policy. A health ping alone
is insufficient. The whole check is bounded by the configured OPA timeout,
capped at five seconds. Redirects are rejected. A failed check leaves the prior
revision active; staged immutable OPA modules do not gain authority. Atomic
policy-version rechecking prevents a concurrent activation from invalidating the
candidate check. Restart reloads the database configuration; no local cache is
needed. Tenant-specific origins may differ only within the deployment allowlist.

The allowlist trusts deployment DNS/routing for those exact origins. Use dedicated
OPA hosts and TLS on shared networks; this RC does not manage certificates or
network egress rules. Secret files must be regular, private files (0600 or 0400),
with tokens no larger than 8 KiB. Aliases and endpoints are operator-visible;
values, paths, peer bodies and transport errors are absent from status/evidence.

A configuration read means **configured**, not healthy. Status reports `ready`
only after the signed-artifact check, otherwise `unavailable`. An unavailable
current authorization OPA can prevent status/edit authorization itself (503).
Recover that deployment connection using protected deployment configuration;
there is no unauthenticated fail-open repair API. Unsupported peers remain
explicitly unsupported in this catalog rather than advertised as ready.
