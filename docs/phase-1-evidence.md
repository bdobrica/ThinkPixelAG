# Phase 1 Engineering Foundation Evidence

Phase 1 exited on 2026-08-10 after verification of the exact committed tree at
`147cbf4aa45d35d573a884095e90dca058310078` (`147cbf4`). The audit used a fresh
local clone with no shared worktree or hard-linked Git objects. `git status
--porcelain` was empty before and after verification, proving that generators,
tests, builds, and image checks neither relied on nor changed uncommitted files.

## Verification environment

| Component | Audited value |
|---|---|
| Host architecture | `linux/amd64` (`x86_64`) |
| Go | `go1.26.5 linux/amd64` |
| Docker Engine | client/server 29.0.1, API 1.52 |
| Docker Compose | v2.40.3 |
| Redocly CLI | 2.3.0 |
| govulncheck | 1.6.0, Go vulnerability database |
| Builder base | `golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc` |
| Runtime base | `gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6` |

## Clean-checkout gate

The fresh clone passed `make verify`, which exercised:

- generation drift and formatting checks;
- Go vet, checksum verification, read-only module loading, and OpenAPI lint;
- root and tools-module unit/coverage and race tests;
- policy, integration-tagged, and end-to-end-tagged test targets;
- Docker Compose configuration validation;
- the 82-module source/pinning policy;
- reachable vulnerability and transitive license enforcement;
- the statically linked binary build with commit metadata;
- the immutable-base OCI image build and hardened container smoke test.

No reachable Go vulnerabilities or disallowed dependency licenses were found.
Policy sources and dependency-backed application suites are intentionally empty
until their owning later phases; their Make targets ran and reported this
declared state rather than being skipped by the aggregate gate.

## Image evidence

The audit produced `thinkpixelag:eng012-147cbf4` with local image ID
`sha256:87e7a0d6d23863e457088333adfca86be53dc7055c31cbbf83bec1da02d812e9`.
The local artifact was 6,724,807 bytes for `linux/amd64`; its OCI version was
`147cbf4`, revision was the full audited commit, and configured identity was
numeric UID/GID `65532:65532`.

The runtime test proved that `/bin/sh` is absent, the running process uses UID
65532, the root filesystem is read-only, all Linux capabilities are dropped,
`no-new-privileges` is enabled, `/livez` and `/readyz` respond successfully, and
SIGTERM produces a graceful zero exit. The smoke uses only a restricted 1 MiB
temporary filesystem and loopback-only ephemeral port publication.

## Exit conclusion

All Phase 1 deliverables and its clean-checkout/non-root-image exit conditions
are satisfied. This evidence qualifies the engineering foundation on
`linux/amd64`; multi-architecture publication and `linux/arm64` runtime
qualification remain explicitly assigned to OPS-001 in Phase 8. Hosted CI
execution begins when these local commits are pushed; immutable workflow pins
and required job/Make-target contracts are already repository-tested.
