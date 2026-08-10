# Development and Verification Commands

The repository-root `Makefile` is the stable local and CI interface. Run targets
from the repository root with GNU Make, Bash, Git, the pinned Go toolchain, and
network access for authenticated tool/module and vulnerability database reads.
Targets fail on command errors; scanners do not convert network failures into
clean results.

## Primary targets

| Target | Contract |
|---|---|
| `make tools` | download root/tools module graphs and identify the pinned `govulncheck`, `go-licenses`, and Redocly CLI tools |
| `make generate` | run `go generate ./...` |
| `make fmt` | format every tracked Go source file |
| `make lint` | require formatting, vet both Go modules, verify checksums/read-only package loading, and validate OpenAPI with Redocly CLI 2.3.0 |
| `make test` | test the service and nested tools module with coverage |
| `make test-race` | race-test both Go modules |
| `make test-policy` | run `opa test policies` once tracked Rego sources exist; until Phase 3 it reports their absence explicitly |
| `make test-integration` | compile and run the repository with the `integration` build tag; later phases add real-dependency suites |
| `make test-e2e` | compile and run the repository with the `e2e` build tag; later phases add workflow suites |
| `make build` | create a static, trimmed `.cache/bin/thinkpixelag` with version/revision metadata |
| `make image` | build `IMAGE` with Docker; fails clearly until ENG-011 provides `Dockerfile` |
| `make compose-check` | validate and fully resolve the pinned Compose definition without starting dependencies |
| `make dev-up` | start pinned PostgreSQL and OPA and wait for healthy state |
| `make dev-up-valkey` | start PostgreSQL, OPA, and the optional Valkey profile and wait for healthy state |
| `make dev-status` | show dependency container and health state |
| `make dev-smoke` | check versions, endpoints, and successful plus deliberately rejected database/cache credentials |
| `make dev-down` | stop the local stack while preserving PostgreSQL state |
| `make dev-reset` | stop the stack and irreversibly remove this Compose project's local volumes |
| `make verify` | run generation drift, lint, unit/race/policy/integration/e2e, Compose validation, dependency source, vulnerability, license, and binary build gates |

`make verify` is intended for a clean checkout. `generate-check` runs generators
and rejects any resulting unstaged tracked-file difference. CI should invoke the
same target. The container build remains a separate mandatory CI/release gate
after ENG-011 because it requires a container daemon and pinned image assets.

## Continuous integration

`.github/workflows/ci.yaml` runs on pull requests and pushes to `main`. Separate
jobs expose formatting/generation/lint, unit, race, policy, integration,
dependency security/license, binary, and OCI-image status checks. Jobs call the
Makefile rather than duplicating its behavior. The workflow grants only
read-only repository access, does not persist checkout credentials, cancels
superseded runs, applies timeouts, and pins third-party actions to full commit
SHAs with their reviewed release tags in comments.

Until ENG-011 adds `Dockerfile`, the image job reports that explicit prerequisite
and succeeds without pretending to build an image. The same job automatically
runs `make image` as soon as `Dockerfile` exists; at that point it is a mandatory
build gate. Configure branch protection to require all eight named CI jobs.

Build outputs live only below ignored `.cache/`. Override `BUILD_DIR`, `VERSION`,
`REVISION`, or `IMAGE` for controlled builds. `make clean` removes only the fixed
repository-local `.cache/bin` directory.

## Security subtargets

`make security` combines:

- `make dependency-check`, using the repository source/pseudo-version policy;
- `make vulnerability-check`, using pinned `govulncheck` 1.6.0;
- `make license-check`, using pinned `go-licenses` 2.0.1 and the documented
  disallowed classifications.

These targets consume `tools/go.mod`; they never install an unversioned global
Go tool. Redocly is invoked by exact npm package version. OPA remains external
to the Go module; local orchestration pins OPA 1.19.0 by immutable image digest.

## Local dependencies

Docker Engine and Docker Compose v2 are required. `make dev-up` starts only
PostgreSQL and OPA; Valkey is opt-in because correctness must not depend on it.
All host ports bind to loopback. The ignored root `.env` may override values
listed in `deploy/local.env.example`, which is useful when default ports are in
use. Complete credentials, lifecycle behavior, and the reset warning are in
[`deploy/README.md`](../../deploy/README.md).
