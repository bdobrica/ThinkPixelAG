# Integration-RC contract freeze (RC-001)

The first integration candidate freezes OpenAPI `0.1.0-rc.1` and the existing
policy wire identifier `thinkpixelag.authorization/v1alpha1`. Retaining that
identifier avoids breaking already stored policy artifacts and deployed
consumers. This is an integration freeze, not an assertion that AR's future
worker/harness API already exists.

`api/contract-freeze.json` records SHA-256 fingerprints of the OpenAPI document,
all four versioned JSON Schemas, the policy contract reference, and its Go
wire types/closed decoder/catalog. Fingerprints normalize CRLF to LF so Git
line-ending settings do not create false drift; other changes require review.
The policy Go source fingerprint is deliberately stricter than semantic wire
compatibility: even an implementation-only edit requires conscious review.

Run `make contract-check`; it is also part of `make verify`. Existing policy,
OPA, HTTP, evidence and workload-identity tests continue to validate behavior,
including unsupported versions, unknown fields, constraints and authority.
The fingerprint guard detects drift; it is not a general semantic API diff tool.
After an approved compatible correction or versioned contract change, update
the source and documentation, review compatibility, and explicitly regenerate
with `python3 scripts/freeze-contracts.py`. Never regenerate merely to make an
unexplained mismatch pass. Generated code/artifacts continue to use the normal
`make generate-check` workflow.

## Compatibility evidence

The baseline is Phase 8 closeout `72715e1`. There were no prior release tags
in this checkout. Comparison against that committed baseline found:

- OpenAPI paths, operations, security requirements and schemas are unchanged;
  only the stale Phase 0 `info.version` and description labels change.
- All four JSON Schemas are unchanged.
- Policy JSON types, decoder, reason catalog and wire identifier are unchanged.
- The policy reference's input/output and validation sections are unchanged;
  only its experimental/freeze-status paragraph changes.

See [machine-readable comparison](../evidence/results/compatibility.json). No migration or
request/response behavior changes in this item. Breaking changes still require
a new wire version and an explicit compatibility/migration plan under the
accepted ADRs and contract documentation; a new image tag is insufficient.

## Documentation relocation

The documentation cleanup changes only the relative link to this guide in
`policy-decision.md`. Its fingerprint was regenerated with
`scripts/freeze-contracts.py`; the policy wire version, input/output semantics,
OpenAPI and JSON Schemas are unchanged. Historical compatibility reports retain
their original fingerprints and recorded source revision.

## RC-102 constraint inheritance correction

The policy wire identifier and OpenAPI schemas remain unchanged. Root admission
now preserves authoritative limits when caller or policy fields are omitted,
including approved manifest bounds and cached decisions. Explicit policy
expansion and caller dimensions without authority fail closed. Completely
unbounded admission is rejected. These are corrections to the existing
resource-accounting/non-expansion contract, not a new permission model. Existing
policies returning partial objects remain supported through Go inheritance.

OPA/cache JSON decoding retains exact numbers instead of float64 rounding.
The policy reference and Go contract fingerprints are regenerated intentionally
for this reviewed correction; existing signed bundles are not rewritten or
silently reactivated. Release images predating this change still need an upgrade.

## RC-104 administration additions

Editor, policy read/history and local approval routes are additive OpenAPI
operations. Existing upload/activation request and response schemas are retained.
The policy wire identifier is unchanged. The OpenAPI fingerprint was explicitly
regenerated for the new paths and schemas; schemas 19/20 only add policy history
protection and draft/receipt storage. No existing signed artifact is rewritten.

The next candidate adds tenant role-mapping read/update and bound expansion
approval requests. Existing token wire formats and closed roles are unchanged;
API mode remaps existing verified claims live. Schema 21 stores immutable mapping
revisions. See [managed configuration](managed-configuration.md).
