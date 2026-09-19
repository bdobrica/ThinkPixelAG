# ThinkPixelAG

ThinkPixelAG is the agent governance and lifecycle control plane for the modular
ThinkPixel platform. It owns durable agent and Run authority: registration,
admission, policy decisions, resource envelopes, approvals, revocation, and
governance evidence. It does not execute agent logic or broker model and tool
credentials.

The service is independently deployable. Integrations with other ThinkPixel
components use replaceable adapters and versioned contracts; they are not
in-process dependencies. See [ALIGNMENT.md](ALIGNMENT.md) for the repository's
ownership and integration boundaries.

## Status

The integration candidate targets development with ThinkPixelAR and a real
harness. Its frozen contracts are OpenAPI `0.1.0-rc.1` and policy
`thinkpixelag.authorization/v1alpha1`. [Verification](docs/releases/verification.md)
and [game-day evidence](docs/releases/game-days.md) cover the implemented AG
component. Final release packaging is still in progress.

Production capacity and unavailable AR/gateway/HA scenarios are
[explicitly deferred](docs/operations/deferred-qualification.md). The
[measured operating envelope](docs/releases/capacity-envelope.md) is separate
from production targets. AG does not yet expose a versioned AR worker API;
registration/policy-management runtime composition and production provider
ceremonies are also outside the current executable's configured surface.

## Quick start

Prerequisites and supported versions are documented in
[docs/supported-versions.md](docs/supported-versions.md).

```sh
make tools
make dev-up
make dev-smoke
make test
make test-integration
make test-policy
make build
```

These commands prepare dependencies, run tests and build AG. To serve governed
APIs, configure OIDC, an approved agent/policy fixture, authoritative PostgreSQL,
`THINKPIXELAG_RUNTIME_FILE` and separately delivered cursor secrets as described
in [runtime configuration](docs/configuration.md#governed-runtime-composition).
Trusted APIs additionally require the mTLS listener and workload bindings.

Use `make dev-up-valkey` to include the optional cache. `make verify` is the
aggregate developer and CI gate; it also requires the documented container
tooling. See [development and verification](docs/operations/development.md) for
the complete workflow and safe test-database requirements.

## Key concepts

- Verified identity and policy establish authority; request content, Skills,
  Workspace membership, memory, model output, and guardrail results cannot
  enlarge it.
- PostgreSQL is authoritative. Valkey is an optional, disposable accelerator
  and cannot create an allow, advance an epoch, or expand a balance.
- Agent versions are immutable, content-addressed artifacts with separate
  approval and revocation state.
- Run admission persists a governed version resolution and resource envelope;
  untrusted runtimes may enforce or narrow that authority but cannot expand it.
- Resource reservation, metering, settlement, and reclaim preserve capacity
  transactionally and emit correlated governance evidence.
- Revocation decisions are ordered, resumable, epoch-bound, and subject to
  operation-specific freshness limits.

## Documentation

- [Design documentation index](docs/README.md)
- [OpenAPI 3.1 contract](api/openapi/thinkpixelag.yaml)
- [System architecture](docs/architecture/system.md)
- [Domain and lifecycle contracts](docs/contracts/domain-model.md)
- [Resource accounting contract](docs/contracts/resource-accounting.md)
- [Revocation and freshness contract](docs/contracts/revocation.md)
- [Threat model](docs/security/threat-model.md)
- [Configuration reference](docs/configuration.md)
- [Deployment guidance](deploy/README.md)

## Repository layout

```text
api/          public API schemas
cmd/          service and migration commands
internal/     domain, application, ports, and adapters
migrations/   PostgreSQL migrations
policies/     Rego policy and tests
deploy/       deployment assets and guidance
docs/         durable architecture, contracts, security, and operations docs
test/         integration, contract, security, and end-to-end tests
```

## Platform and support

See the [platform composition](docs/architecture/platform-composition.md) for
intended cross-component integrations and [ALIGNMENT.md](ALIGNMENT.md) for AG
ownership. This is a development/integration candidate with best-effort
maintainer support; it does not carry a production availability SLA. Report
non-sensitive, reproducible defects through the repository issue tracker.
Security assumptions and outstanding qualification are recorded in the
[threat model](docs/security/threat-model.md) and [release risk review](docs/releases/risk-review.md).

## License


Licensed under the terms in [LICENSE](LICENSE).
