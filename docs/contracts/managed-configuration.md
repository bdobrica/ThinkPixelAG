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
