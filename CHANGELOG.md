# Changelog

## Unreleased

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
