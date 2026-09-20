# Package an integration candidate

Start from a clean committed checkout. Run `make verify` against the documented
PostgreSQL/OPA dependencies and `make verify-console` for the optional component.
Complete the [local operator/harness walkthrough](../examples/local-sandbox/README.md),
including restart and console-disabled API operation. Review additive API changes,
contract fingerprints and forward migrations before changing a release version.
Keep [accepted qualification limits](../adr/0012-integration-rc-qualification-deferrals.md)
and [local signing limits](../adr/0016-local-development-policy-promotion.md) explicit.

The [release workflow](../../.github/workflows/release.yaml) builds AG and console
independently for Linux AMD64/ARM64, records immutable image indexes and BuildKit
SBOM/provenance attachments, and scans **each platform image**, including unfixed
HIGH/CRITICAL findings. The console remains an optional deployment. A failure in
its release gate must not be represented as a successfully qualified console.

To generate local inventories for an already pushed image, use a fresh private
output directory per component/platform and a clean source commit:

```sh
export VERSION=0.1.0-rc.2
export REVISION="$(git rev-parse HEAD)"
export SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
export IMAGE=quay.io/OWNER/thinkpixelag@sha256:PLATFORM_MANIFEST_DIGEST
export ARCHITECTURE=amd64  # repeat for arm64 with its matching image digest
export COMPONENT=ag      # console for its independent image inventory
export OUTPUT_DIR="$PWD/dist/$COMPONENT-$ARCHITECTURE"
bash scripts/release-artifacts.sh
(cd "$OUTPUT_DIR" && sha256sum --check SHA256SUMS)
```

Use the Go toolchain pinned in `go.mod` and Trivy pinned in the workflow. AG
bundles include static `thinkpixelag`, `thinkpixelag-migrate` and
`thinkpixelag-operator` binaries in `binaries/linux-ARCH`. Both component bundles
include exact committed source (helper, console, evaluation installers, guides,
migrations), API/schemas, deployment assets, scan report, CycloneDX SBOM,
checksums and unsigned source/image metadata. Extract the source archive and
follow its local guide with `--bin-dir` to avoid requiring Go on the operator
host. The source archive contains tracked files only, never ignored local state.

Setting `COSIGN_KEY` and `COSIGN_PUBLIC_KEY` invokes signing and verification;
leaving them unset produces **no signature**. BuildKit metadata and unsigned
local inventories are not authenticated remote-builder provenance. GitHub OIDC
artifact attestations and a draft GitHub release are produced only when that
workflow actually runs successfully on a version tag. A local/Quay rehearsal
must list which outputs were created without claiming those GitHub outputs.

Record exact source, version, image index/platform digests, bundle checksums,
scan date and runtime scope in [release evidence](../evidence/README.md). Never
publish credential-bearing installation configuration or operational payloads.
