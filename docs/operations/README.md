# Operating ThinkPixelAG

Use this guide to evaluate, install and operate the AG governance service.
Start with the [quick start](../quickstart.md) for an isolated test or the
[team PoC](../examples/team-poc/README.md) for a Kubernetes deployment template.

1. **Prepare:** review [tested versions](supported-versions.md),
   [security prerequisites](../security/README.md), and [capacity](capacity.md).
2. **Install:** follow [installation](installation.md), including explicit
   migration, secret delivery and readiness acceptance.
3. **Integrate:** configure [callers, trusted services and harness boundaries](integrations.md).
4. **Observe:** set up [monitoring](monitoring.md) and compare measurements with
   unchanged [production SLOs](slos.md).
5. **Respond:** use [incident procedures](incidents.md) and the detailed
   [runbooks](runbooks.md).
6. **Protect state:** establish [backup and recovery](backup-recovery.md), then
   follow the [upgrade procedure](runbooks.md#migration-upgrade-and-rollback).

## References and rehearsals

- [Configuration](../configuration.md), [HTTP lifecycle](http-server.md),
  [metrics and tracing reference](observability.md), [logging](logging.md).
- [Developer commands](development.md).
- [Recovery rehearsals](testing/recovery-testing.md),
  [resilience tests](testing/resilience-testing.md),
  [deployment lifecycle tests](testing/lifecycle-testing.md),
  [load qualification](testing/load-testing.md).

The current RC qualifies the implemented AG component. The
[accepted deferrals](../adr/0012-integration-rc-qualification-deferrals.md)
identify the production and cross-component evidence still needed.

[Policy administration](administration.md) covers draft editing, promotion,
activation and independent local rollback approval through the API.

[Operator bootstrap and recovery](bootstrap.md) provisions the source-built local
administration candidate without fixture seeding or SQL edits.

The [optional administration console](../../console/README.md) provides browser
policy, role-mapping and supported integration workflows through the same AG APIs.
