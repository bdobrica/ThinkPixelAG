# ADR-0015: Tenant-scoped managed governance configuration

- Status: Accepted
- Date: 2026-09-20
- Implementation: RC-105–107 local administration profile and protected operator commands implemented
- Supersedes: ADR-0003 only for the source/refresh of external-role mappings;
  verified issuer, audience, tenant and principal requirements remain unchanged
- Supplements: ADR-0010; closed roles and privileged approval rules remain intact

## Context and decision

The IdP owns accounts, groups and membership. AG owns mappings from verified
external roles to its closed internal roles. Permit API-managed tenant-scoped
mappings and supported integration connection settings, while retaining
deployment-managed files for operators who prefer declarative configuration.

Deployment configuration selects `file` or `api` ownership independently for
each class. Selection is fixed at startup. File mode is read-only through AG's
API; API mode stores complete revisioned records in PostgreSQL. Do not merge
sources. Switching modes is an explicit operator migration with a reviewed
snapshot, not fallback to stale file values after database failure.

Introduce dedicated closed actions `role_mappings.read/manage` and
`integrations.read/manage`, assigned explicitly to `policy-admin` for this RC.
No other administrator inherits them. Authorize against the current mapping
and policy before applying the proposed change. Require digest-bound independent
approval using `EMERGENCY_EXPANSION` when a mapping adds administrative authority,
including an operator expanding their own authority. Privileged writes use live
authoritative state, never stale cached mappings. A role remains an input to
action authorization, not blanket permission to change every governance object.

Derive tenant and principal only from verified OIDC identity. Bind mappings to
that tenant and configured issuer. Permit only human/internal administrative
roles in OIDC mappings; service roles remain behind the mTLS workload boundary.
Do not expose IdP membership management, custom role creation or multiple-issuer
discovery. Changes cannot move a principal across tenants or rewrite token claims.

Ordinary requests may reuse mapping snapshots for at most 30 seconds; privileged
operations load the current revision. A committed change advances the tenant
authorization revision, invalidates local authorization caches and propagates
bounded refresh to replicas. Cached policy decisions bind the effective roles
and configuration revision. Existing tokens are remapped on subsequent requests:
removing a mapping takes effect within that bound without waiting for token
expiry. No fresh API-managed snapshot means denial after the freshness bound,
not fallback to token roles. Implement these semantics before enabling writes.

Mapping edits carry expected revisions and produce atomic audit/outbox evidence.
Reject removing the last configured policy-administrator mapping. This cannot
prove IdP membership still exists: deployment-controlled operator recovery must
also support a reviewed, audited mapping reset using local administrative
credentials and application services, with no unauthenticated recovery endpoint
or standing recovery role. Recovery never suppresses required expansion approval.

Initial provisioning is an explicit operator command with replay protection and
the deployment trust configuration. It creates initial tenant/mapping/policy
state before ordinary administration is enabled. Re-running cannot silently
replace existing authority. Bootstrap/recovery credentials remain outside
harness state and the UI.

## Integration settings

Only fields belonging to already implemented adapters enter the managed catalog.
The first candidate is the OPA decision connection (endpoint and protected token
reference); activation/signing trust and compatible bundle loading remain
separate governance workflows. Enabling this field depends on safe adapter
reload and active-bundle verification in RC-103/106, not just a successful ping.

Endpoint destinations must fit deployment-owned protocol/host/port allowlists.
Connectivity checks are bounded and cannot become arbitrary URL probes. Resolve
secret references only in trusted adapters; never return secret values through
forms, status or harness instructions. Updates validate before activation and
retain the last valid revision if validation fails. Show configured, ready,
unavailable and unsupported distinctly.

OIDC issuer/audience, listeners, PostgreSQL, trust roots, signing profile and
destination allowlists remain deployment-managed and require a restart. Future
peer adapters can extend the catalog through reviewed versioned contracts; a
configured URL cannot advertise an unimplemented capability.

## Alternatives and consequences

Managing IdP users or inventing custom RBAC expands the RC unnecessarily. A
generic editable environment-variable endpoint would expose secrets and trust
roots. Purely static mappings remain supported but cannot meet the requested
dynamic operator workflow. This decision adds persistence/refresh and recovery
work; APIs must not ship before those controls are implemented.

## References

- [OIDC authority](0003-oidc-authentication-and-tenant-authority.md)
- [Privileged authority](0010-privileged-authority-and-managed-keys.md)
- [Approval contract](../contracts/governance-approvals.md)
- [Optional console](0014-optional-administration-console.md)
