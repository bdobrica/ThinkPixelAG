# ThinkPixelAG Design Documentation

These documents define the release-candidate contracts for ThinkPixelAG. Normative terms such as **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used as described by RFC 2119.

## Architecture and contracts

- [System architecture](architecture/system.md)
- [Domain contracts and state machines](contracts/domain-model.md)
- [Resource accounting](contracts/resource-accounting.md)
- [Domain primitive contracts](contracts/primitives.md)
- [Revocation and freshness](contracts/revocation.md)
- [Policy decision contract](contracts/policy-decision.md)
- [OpenAPI contract](../api/openapi/thinkpixelag.yaml)
- [Configuration reference](configuration.md)

## Security and operations

- [Threat model](security/threat-model.md)
- [Data classification and redaction](security/data-classification.md)
- [Dependency and build-tool policy](security/dependencies.md)
- [Supported versions](supported-versions.md)
- [SLOs and capacity targets](operations/slos.md)
- [Structured logging and redaction](operations/logging.md)
- [Metrics and tracing](operations/observability.md)
- [HTTP server and process lifecycle](operations/http-server.md)
- [Development and verification commands](operations/development.md)
- [Phase 0 blueprint review](phase-0-review.md)
- [Phase 1 engineering foundation evidence](phase-1-evidence.md)

## Architecture decisions

Durable decisions live in [Architecture Decision Records](adr/README.md). During implementation, `PLAN.md` and `TODO.md` remain the execution ledger; their durable rationale will be reconciled into ADRs before the release candidate.
