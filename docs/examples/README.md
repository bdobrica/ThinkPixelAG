# Installations and usage examples

| Setup | What you get |
|---|---|
| [Local installation](local-sandbox/README.md) | A ready AG HTTP service, PostgreSQL, OPA, sample identity/provisioning and durable evidence; real API usage in the [quick start](../quickstart.md) |
| [Team Kubernetes PoC](team-poc/README.md) | The same usable governance workflow in a dedicated namespace with persistent storage and private access |

Both use the published AG image. Sample provisioning fills the RC's missing
administrative bootstrap for an evaluation; it is not a production admin API.
Read the [implemented capabilities and harness boundary](../operations/integrations.md)
before planning an end-to-end platform integration. Dynamic harness instructions
are [proposed future work](../adr/0013-dynamic-harness-instructions.md).

The local Compose installation and documented API workflow were exercised on
AMD64, including token renewal and retained state. The Kubernetes renderer was
checked for staged resources, references and credential reuse; its output has
not been deployed to a cluster as part of this documentation/example change.
