# ThinkPixelAG Design Documentation

These documents define the release-candidate contracts for ThinkPixelAG. Normative terms such as **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used as described by RFC 2119.

## Architecture and contracts

- [System architecture](architecture/system.md)
- [Intended platform composition](architecture/platform-composition.md)
- [Authoritative PostgreSQL schema](architecture/database-schema.md)
- [Domain contracts and state machines](contracts/domain-model.md)
- [Resource accounting](contracts/resource-accounting.md)
- [Domain primitive contracts](contracts/primitives.md)
- [Revocation and freshness](contracts/revocation.md)
- [Policy decision contract](contracts/policy-decision.md)
- [Signed privileged artifacts](contracts/signed-artifacts.md)
- [Privileged evidence events](contracts/evidence-events.md)
- [Break-glass workflow](contracts/break-glass.md)
- [Workload identity and service authorization](contracts/workload-identity.md)
- [OpenAPI contract](../api/openapi/thinkpixelag.yaml)
- [Run lifecycle API examples](api/run-lifecycle.md)
- [Configuration reference](configuration.md)

## Security and operations

- [Threat model](security/threat-model.md)
- [Data classification and redaction](security/data-classification.md)
- [Authentication and tenant mapping](security/authentication.md)
- [Policy evaluation and lifecycle](security/policy-lifecycle.md)
- [Dependency and build-tool policy](security/dependencies.md)
- [Supported versions](supported-versions.md)
- [SLOs and capacity targets](operations/slos.md)
- [Phase 6 revocation distribution and freshness evidence](phase-6-evidence.md)
- [Phase 7 governance self-protection evidence](phase-7-evidence.md)
- [Phase 8 production operations evidence](phase-8-evidence.md)
- [Phase 8 closeout checkpoint and artifact inventory](operations/phase-8-checkpoint.md)
- [Structured logging and redaction](operations/logging.md)
- [Metrics and tracing](operations/observability.md)
- [HTTP server and process lifecycle](operations/http-server.md)
- [Production runbooks](operations/runbooks.md)
- [Recovery and resilience qualification](operations/recovery-testing.md)
- [Load qualification](operations/load-testing.md)
- [SSD-assisted operational qualification](operations/ssd-qualification.md)
- [Durable evidence on SSD](operations/ssd-evidence-qualification.md)
- [Phase 8 integration-RC closeout](operations/phase-8-closeout.md)
- [Deferred production and cross-component qualification](operations/deferred-qualification.md)
- [Development and verification commands](operations/development.md)
- [Phase 0 blueprint review](phase-0-review.md)
- [Phase 1 engineering foundation evidence](phase-1-evidence.md)
- [Phase 2 authoritative persistence evidence](phase-2-evidence.md)
- [Phase 3 identity, policy, and registry evidence](phase-3-evidence.md)
- [Phase 4 run lifecycle API evidence](phase-4-evidence.md)
- [Phase 5 resource governance evidence](phase-5-evidence.md)

## Architecture decisions

Durable decisions live in [Architecture Decision Records](adr/README.md). The
root planning files are retired; [historical snapshots](releases/history/README.md)
preserve their rationale and implementation lineage. Current release completion
and remaining work live in [release status](releases/status.md).

## Integration release candidate

- [Contract freeze and compatibility](releases/contract-freeze.md)
- [Clean-checkout verification and live smoke](releases/verification.md)
- [Measured operating envelope and scaling](releases/capacity-envelope.md)
- [Risk review and remaining qualification](releases/risk-review.md)
- [Game days and component recovery](releases/game-days.md)
- [Checklist and evidence reconciliation](releases/reconciliation.md)
- [Decision/history preservation review](releases/preservation-review.md)
- [0.1.0-rc.1 notes and operator checklist](releases/0.1.0-rc.1.md)
- [Current release completion and remaining work](releases/status.md)
