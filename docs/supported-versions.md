# Supported Versions and Upgrade Policy

This matrix distinguishes locally tested dependency pins from later CI and
production-platform qualification. Phase 1 must replace each remaining
`TBD-tested` entry before that component is declared supported for an RC.

| Component | RC support baseline | Pinning rule | Support policy |
|---|---|---|---|
| Go | 1.26.5; builder `golang:1.26.5-alpine3.23` | `go.mod`, toolchain directive, builder image index `sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc`, CI | latest two Go minor releases only after tests pass; patch updates preferred |
| PostgreSQL | 18.4 (`postgres:18.4-alpine3.23`), locally tested on `linux/amd64` | Compose image index digest `sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e` | no EOL major; upgrade one major at a time with restore/migration rehearsal |
| OPA | 1.19.0 (`openpolicyagent/opa:1.19.0-debug`), locally tested on `linux/amd64` | Compose image index digest `sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4` | policy bundle/decision contract versioned independently; patch/minor after conformance tests |
| Valkey | optional; 9.1.1 (`valkey/valkey:9.1.1-alpine3.24`), locally tested on `linux/amd64` | Compose image index digest `sha256:ee91f7a174ac4d6a6b0685b3a60e321f0a9dbbb691f9b0e285be2ba1d1be8328` | cache is disposable; no correctness migration dependency; support one tested major |
| Kubernetes | `TBD-tested`; conform to upstream supported skew | cluster test matrix and API versions | oldest and newest tested minors documented; avoid deprecated APIs |
| OpenAPI | 3.1.x document | schema validation tool pinned | breaking API changes require a new API version; additive changes remain compatible |
| OCI runtime/platforms | distroless `static-debian13:nonroot`; `linux/amd64` required and locally tested; `linux/arm64` target | runtime image index `sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6`; numeric UID/GID 65532 | both platforms must pass container smoke tests before advertised; runtime remains shell-free and non-root |
| Prometheus Go client | 1.24.1 | `go.mod` and `go.sum` | update after metric compatibility, race, and exposition tests pass |
| OpenTelemetry Go | 1.45.0 | `go.mod` and `go.sum` | API, SDK, and OTLP exporter remain on one tested release; update after export/propagation tests pass |

### Build and security tools

| Tool | Pinned version | Purpose |
|---|---|---|
| `govulncheck` | 1.6.0 | reachable Go and standard-library vulnerability scanning |
| `go-licenses` | 2.0.1 | application/test transitive license classification and enforcement |
| Redocly CLI | 2.3.0 | OpenAPI 3.1 validation through the exact-version Make invocation |

The isolated `tools/go.mod` and `tools/go.sum` are authoritative for Go tool
versions and checksums. Tool updates follow the same review and verification
policy as runtime dependencies.

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

Go 1.26.5 was selected on 2026-08-10 as the latest stable patch listed by the
official Go release history. The module/package checks and pinned builder passed
on `linux/amd64`; the distroless runtime passed non-root, read-only, health, build
metadata, and graceful-shutdown smoke tests. Cross-platform publication and
`linux/arm64` runtime qualification remain Phase 8 work.

The complete Phase 1 matrix and engineering foundation were reverified from a
clean clone of `147cbf4` on `linux/amd64`; see
[`phase-1-evidence.md`](phase-1-evidence.md) for the environment, commands,
artifact identity, and declared later-phase qualifications.
