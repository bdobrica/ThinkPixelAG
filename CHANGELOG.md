# Changelog

## Unreleased

- Reorganize documentation into quick start, runnable local evaluation, team
  Kubernetes PoC, operator/security/contract guides and a historical evidence archive.
- Centralize the accepted integration-RC qualification deferrals in ADR-0012.

## 0.1.0-rc.1 — integration candidate

The candidate version is `0.1.0-rc.1`; no semantic Git tag or formal release has
been published. [Exact source, OCI digests and validation](docs/evidence/rc/final-artifacts.md)
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
