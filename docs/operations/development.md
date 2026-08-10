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
| `make test-policy` | run `opa test policies` once tracked Rego sources exist; until ENG-009/Phase 3 it reports their absence explicitly |
| `make test-integration` | compile and run the repository with the `integration` build tag; later phases add real-dependency suites |
| `make test-e2e` | compile and run the repository with the `e2e` build tag; later phases add workflow suites |
| `make build` | create a static, trimmed `.cache/bin/thinkpixelag` with version/revision metadata |
| `make image` | build `IMAGE` with Docker; fails clearly until ENG-011 provides `Dockerfile` |
| `make verify` | run generation drift, lint, unit/race/policy/integration/e2e, dependency source, vulnerability, license, and binary build gates |

`make verify` is intended for a clean checkout. `generate-check` runs generators
and rejects any resulting unstaged tracked-file difference. CI should invoke the
same target. The container build remains a separate mandatory CI/release gate
after ENG-011 because it requires a container daemon and pinned image assets.

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
until ENG-009 pins the supported binary/container version.
