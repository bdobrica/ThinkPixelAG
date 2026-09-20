# ADR-0010: Narrow privileged authority and managed key boundaries

- Status: Accepted
- Date: 2026-09-19
- Owners: project maintainers
- Supersedes: none
- Superseded by: none

## Context

Governance administration is itself an attack surface. Broad roles, exportable
keys or reusable emergency credentials could bypass the platform's ordinary
controls. Phase 7's implemented choices refine the original plan without making
a provider, network location or arbitrary metadata an authority source.

## Decision

Use closed, non-hierarchical action roles and digest-bound four-eyes approvals
for high-impact operations. Bind trusted service identity to an exact verified
client-certificate URI SAN and deployment-owned workload/tenant/role mapping;
reject bearer/forwarded identity on those routes. Public identity follows ADR-0003.

Managed signing ports accept digests and return signatures plus immutable key
metadata, never private key material. The guard requires enabled, appropriate
purpose/algorithm, non-exportable KMS/HSM protection and exact version binding.
Unknown/substituted metadata and provider failure deny. Local/test signing may
be disabled; test-only keys are not production KMS qualification.

Signed privileged artifacts bind class, schema, positive revision and exact
payload digest under a domain-separated manifest. Closed catalogs reject unknown
classes/versions. Migration 016 rejects legacy validation rather than treating
unverifiable artifacts as grandfathered approvals.

Break glass is limited to policy/revocation recovery, recent provider-verified
phishing-resistant MFA, exact four-eyes grant digest and a maximum 15-minute
grant. Store only the random one-time credential's digest. Activation, use,
expiry and revocation have separate atomic evidence; emergency scope cannot
become a standing unrestricted administrator role.

## Alternatives considered

Network trust and forwarded roles lack cryptographic identity binding. A generic
superuser defeats least privilege. File/private-key configuration exposes keys
to application state. Unsigned metadata and reusable emergency credentials do
not preserve attribution or bounded authority.

## Consequences

Production deployments must supply provider adapters, workload identity, narrow
grants and real approval/MFA ceremonies. Component tests establish guard and
transaction semantics but cannot qualify an unselected provider. Key rotation
requires explicit version overlap and retirement, not silent alias movement.

## Security impact

Skills, memory and model output cannot expand authority. Privileged artifact
verification supplements, never replaces, authorization and semantic validation.
Unavailable providers fail closed. Credential material stays outside untrusted
agent/harness state, telemetry and committed evidence.

## Operational impact

Follow signed promotion, rollback, key rotation and break-glass runbooks.
The integration RC records component game days and leaves production provider
ceremonies explicitly unqualified. Rotate workload bindings through controlled
configuration/replica restart rather than caller input.

## References and evidence

- [Managed signing](../contracts/managed-signing.md), [signed artifacts](../contracts/signed-artifacts.md)
- [Workload identity](../contracts/workload-identity.md), [break glass](../contracts/break-glass.md)
- [Phase 7 review](https://github.com/bdobrica/ThinkPixelAG/blob/130fbd21ae27e72912174ce6d2c42fa0318a6989/docs/evidence/implementation/phase-7-evidence.md), [game days](../evidence/README.md#recovery)
- [Historical plan](https://github.com/bdobrica/ThinkPixelAG/blob/130fbd21ae27e72912174ce6d2c42fa0318a6989/docs/evidence/rc/history/PLAN.md), Phase 7; SEC-001–011 in the [ledger](https://github.com/bdobrica/ThinkPixelAG/blob/130fbd21ae27e72912174ce6d2c42fa0318a6989/docs/evidence/rc/history/TODO.md)
- Commits `1386a2f`, `63d8b8f`, `a509a04`, `4d7507b`, `acc08c6`, `6d7771a`, `85ba0e7`
