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
| `make image` | build `IMAGE` from immutable Go and distroless base-image indexes with embedded `VERSION`/`REVISION` |
| `make container-smoke` | build and run `IMAGE`, proving non-root/read-only execution, OCI metadata, probes, and graceful SIGTERM shutdown |
| `make compose-check` | validate and fully resolve the pinned Compose definition without starting dependencies |
| `make dev-up` | start pinned PostgreSQL and OPA and wait for healthy state |
| `make dev-up-valkey` | start PostgreSQL, OPA, and the optional Valkey profile and wait for healthy state |
| `make dev-status` | show dependency container and health state |
| `make dev-smoke` | check versions, endpoints, and successful plus deliberately rejected database/cache credentials |
| `make dev-down` | stop the local stack while preserving PostgreSQL state |
| `make dev-reset` | stop the stack and irreversibly remove this Compose project's local volumes |
| `make verify` | run generation drift, lint, unit/race/policy/integration/e2e, Compose validation, dependency source, vulnerability, license, binary, image, and container-smoke gates |

`make verify` is intended for a clean checkout. `generate-check` runs generators
and rejects any resulting unstaged tracked-file difference. CI should invoke the
same target. A container daemon is required because image build and smoke tests
are mandatory parts of this aggregate gate.

## Continuous integration

`.github/workflows/ci.yaml` runs on pull requests and pushes to `main`. Separate
jobs expose formatting/generation/lint, unit, race, policy, integration,
dependency security/license, binary, and OCI-image status checks. Jobs call the
Makefile rather than duplicating its behavior. The workflow grants only
read-only repository access, does not persist checkout credentials, cancels
superseded runs, applies timeouts, and pins third-party actions to full commit
SHAs with their reviewed release tags in comments.

The image job runs `make container-smoke`, so an image that merely builds but
cannot run under the baseline hardened settings fails CI. Configure branch
protection to require all eight named CI jobs.

## Container image

The multi-stage `Dockerfile` pins the Go 1.26.5 Alpine builder and the distroless
Debian 13 `nonroot` runtime by multi-platform index digest. It cross-compiles a
static binary with the same reproducibility and build-metadata flags as
`make build`. The final image contains the binary, CA roots, time-zone data, and
distroless identity files, but no shell or package manager. It declares and
runs as numeric UID/GID `65532:65532` and listens on port 8080.

`.dockerignore` rejects the repository by default and admits only `go.mod`,
`go.sum`, `cmd/`, and `internal/`, preventing documentation, Git metadata,
secrets, local artifacts, and unrelated deployment files from entering the
build context. Production Kubernetes settings must additionally enforce the
documented security context; the local smoke uses read-only root, a restricted
temporary filesystem, dropped capabilities, and `no-new-privileges`.

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
