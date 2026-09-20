# Administration and harness-guidance candidate

The scoped implementation is complete as `0.1.0-rc.2`. The original detailed
sequence remains in Git history. [TODO.md](TODO.md) records closeout, and the
[candidate inventory](docs/evidence/README.md#candidate-rc2) identifies exact source,
images, verification and publication limits.

Implemented behavior and ownership are documented in:

- [Dynamic harness guidance](docs/adr/0013-dynamic-harness-instructions.md),
  [helper installation](integrations/harness/README.md) and [guidance contract](docs/contracts/harness-guidance.md).
- [Optional console](docs/adr/0014-optional-administration-console.md) and
  [console guide](console/README.md).
- [Managed configuration](docs/adr/0015-managed-governance-configuration.md),
  [operator bootstrap](docs/operations/bootstrap.md) and [administration](docs/operations/administration.md).
- [Local-development signing/approval profile](docs/adr/0016-local-development-policy-promotion.md).

Use the [quick start](docs/quickstart.md) to evaluate this candidate. AG is the
harness entry point; it authorizes governance operations but does not execute
agent logic. The next cross-component execution work needs the contracts and
owner coordination in the [AG/AR/gateway handoff proposal](docs/contracts/execution-handoff-proposal.md).
Do not invent an internal AR worker interface to close that gap.

Production throughput/drops/p99/HA deferrals, production signing-provider
qualification, console HA and broad peer orchestration remain outside this
candidate. The [accepted deferrals](docs/adr/0012-integration-rc-qualification-deferrals.md)
and [platform composition](docs/architecture/platform-composition.md) remain authoritative.
