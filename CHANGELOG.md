# Changelog

## 0.1.0-rc.2 — administration and harness-guidance candidate

Published as independent AMD64/ARM64 AG and console OCI images on 2026-09-20.
[Artifact identity and scoped qualification](docs/evidence/README.md#candidate-rc2)
record source, digests, checks and the absence of a formal GitHub release/signature.

- Ship versioned operator/service/migration binaries, matching source and separate
  per-platform image inventories; update the optional console runtime to pinned
  Python 3.13.15/Alpine with no HIGH/CRITICAL image findings.

- Package a retained real-bootstrap local installer and staged Kubernetes PoC,
  each with an optional console; document actual operator/harness use and recovery.

- Add console role-mapping and OPA configuration review, file-managed read-only
  views, independent expansion approvals and explicit readiness/unsupported status.

- Add console policy editing, source diffs, exact-revision promotion and
  approval-aware activation/rollback through reviewed API requests.

- Add an optional console image with OIDC/PKCE login, protected transient
  operator sessions and authorized governance read views through AG APIs.

- Ship the optional trusted harness helper and appendable instruction bootstrap,
  with HTTPS origin pinning, online refresh and governance operation commands.

- Initialize the root resource catalog during operator bootstrap so approved
  agents can actually admit bounded Runs; provide an audited missing-catalog repair.

- Add authenticated caller/Run-scoped harness capabilities and trusted Markdown,
  with revision/expiry, conditional reauthorization and explicit execution limits.

- Add protected local operator bootstrap/recovery, immutable schema 23 receipts,
  registry creation/version APIs and authorized Run listing.

- Add bounded OPA connection management, protected token references, validated
  live reload and immutable schema 22 revisions.

- Add live tenant-scoped OIDC role mappings, independent approval for administrative
  expansion, file/API ownership and immutable schema 21 revisions.

- Add policy draft editing, validation, exact promotion, paginated history and
  independent OIDC-operator rollback approvals (schema 20).

- Add opt-in local signed policy upload/activation, immutable artifact evaluation
  through OPA, transactional replay/evidence and policy-history protection (schema 19).

- Preserve approved agent/deployment limits when Run callers or policy responses
  omit constraints; reject policy expansion and retain exact OPA/cache numbers.
- Accept the next-RC harness bootstrap, optional console and managed-configuration
  decisions; select an explicit local-development signing/approval profile.

- Replace test-only quick-start steps with a persistent local installation and
  authenticated discovery/admission/replay/read/cancel examples; add a complete
  staged Kubernetes evaluation renderer and restore `docs/configuration.md`.
- Define AG-provided dynamic harness instructions in ADR-0013; clarify AG as
  the harness entry point and distinguish current API support from missing integration.

- Reorganize documentation into quick start, runnable local evaluation, team
  Kubernetes PoC, operator/security/contract guides and focused RC qualification evidence.
- Remove redundant phase reports, copied planning snapshots and diagnostic histories;
  retain selected original results supporting RC claims and limitations.
- Centralize the accepted integration-RC qualification deferrals in ADR-0012.

## 0.1.0-rc.1 — integration candidate

The candidate version is `0.1.0-rc.1`; no semantic Git tag or formal release has
been published. [Exact source, OCI digests and validation](docs/evidence/README.md#artifacts)
identify the qualified artifacts.

- Initial governance component: identity and policy, immutable agent/version
  authority, governed Runs, transactional resources, revocation and durable evidence.
- Restricted AMD64/ARM64 containers and Kubernetes deployment assets;
  OpenAPI `0.1.0-rc.1`, policy `thinkpixelag.authorization/v1alpha1`, schema 18.
- Admission/replay atomicity and replay-safe evidence delivery; optional Valkey
  usage hints preserve PostgreSQL authority.

See [architecture decisions](docs/adr/README.md) for design rationale and
[accepted deferrals](docs/adr/0012-integration-rc-qualification-deferrals.md) for
production capacity, AR/gateway integration and HA limits. This candidate is for
integration development and does not execute a harness itself.
