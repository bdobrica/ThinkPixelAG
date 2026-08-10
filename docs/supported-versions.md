# Supported Versions and Upgrade Policy

Phase 0 defines compatibility policy without prematurely pinning releases that implementation has not tested. Phase 1 MUST replace each `TBD-tested` entry with an exact version/digest and CI evidence before code is declared supported.

| Component | RC support baseline | Pinning rule | Support policy |
|---|---|---|---|
| Go | `TBD-tested`; current stable major/minor at Phase 1 start | `go.mod`, toolchain directive, builder digest, CI | latest two Go minor releases only after tests pass; patch updates preferred |
| PostgreSQL | `TBD-tested`; target current supported major, minimum one prior major where CI cost permits | test image digest and deployment docs | no EOL major; upgrade one major at a time with restore/migration rehearsal |
| OPA | `TBD-tested`; one exact release | binary/image checksum or digest | policy bundle/decision contract versioned independently; patch/minor after conformance tests |
| Valkey | optional; `TBD-tested` exact major/minor | test image digest | cache is disposable; no correctness migration dependency; support one tested major |
| Kubernetes | `TBD-tested`; conform to upstream supported skew | cluster test matrix and API versions | oldest and newest tested minors documented; avoid deprecated APIs |
| OpenAPI | 3.1.x document | schema validation tool pinned | breaking API changes require a new API version; additive changes remain compatible |
| OCI platforms | `linux/amd64` required; `linux/arm64` target | manifest list and base image digests | both platforms must pass container smoke tests before advertised |

## Dependency policy

- Direct Go modules and build tools are pinned; generated output records tool versions.
- Container base and test-service images are pinned by immutable digest for release builds.
- Updates run formatting, generation drift, unit/race, policy, integration, contract, security, and image tests appropriate to their blast radius.
- Critical exploitable security fixes are expedited. If no safe update exists, the affected feature is disabled or the release is blocked.
- Unsupported/EOL dependencies block a new RC unless an explicit, time-bounded security exception is recorded.
- Database schema rollout follows expand/migrate/contract; already shared migrations are never edited.
- Policy contracts carry an explicit schema version. The service rejects unsupported versions rather than guessing.
- API removal or semantic break requires a new `/vN`; deprecation includes telemetry, documentation, and at least one supported release window.

## Upgrade evidence

Every supported-matrix change records the old/new version, upstream notes reviewed, migration/rollback implications, exact verification commands, and artifact digests in the implementing commit or ADR.
