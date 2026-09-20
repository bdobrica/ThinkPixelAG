# ThinkPixelAG documentation

Start with the [quick start](quickstart.md), then choose a [local sandbox or team PoC](examples/README.md).

| I want to… | Read |
|---|---|
| Install and operate AG | [Operations guide](operations/README.md) |
| Connect an application, harness or ThinkPixel component | [Integration guide](operations/integrations.md) |
| Understand the API and wire formats | [Contract guide](contracts/README.md) |
| Understand the system | [Architecture](architecture/system.md) and [platform composition](architecture/platform-composition.md) |
| Understand why a choice was made | [Architecture decisions](adr/README.md) |
| Review security and configure trust | [Security guide](security/README.md) |
| Check the current version and changes | [Changelog](../CHANGELOG.md) |
| Inspect historical qualification results | [Evidence archive](evidence/README.md) |

AG is an integration release candidate. It governs Runs; ThinkPixelAR executes
them through a harness. See the integration guide for the current executable's
composition limits before planning a deployment.

Accepted ADRs take precedence over versioned contracts, then normative security
and architecture documentation. Examples illustrate configuration; they do not
extend authority or change contracts. Historical evidence describes dated tests,
not present-day setup instructions.
