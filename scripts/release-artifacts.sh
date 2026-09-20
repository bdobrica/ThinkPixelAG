#!/usr/bin/env bash
set -euo pipefail

version="${VERSION:?VERSION is required}"
revision="${REVISION:?REVISION is required}"
image="${IMAGE:?IMAGE must be an immutable name or digest}"
output="${OUTPUT_DIR:-dist}"
created="${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required for reproducibility}"
component="${COMPONENT:-ag}"
architecture="${ARCHITECTURE:-amd64}"
[[ "$component" == ag || "$component" == console ]] || { echo 'COMPONENT must be ag or console' >&2; exit 1; }
[[ "$architecture" == amd64 || "$architecture" == arm64 ]] || { echo 'ARCHITECTURE must be amd64 or arm64' >&2; exit 1; }
artifact=thinkpixelag
[[ "$component" != console ]] || artifact=thinkpixelag-console
[[ "$image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] || { echo 'IMAGE must use an immutable sha256 digest' >&2; exit 1; }
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || { echo 'VERSION is not a safe artifact name' >&2; exit 1; }
[[ "$revision" =~ ^[a-f0-9]{40}$ && "$revision" == "$(git rev-parse HEAD)" ]] || { echo 'REVISION must equal the full checkout commit' >&2; exit 1; }
[[ "$created" =~ ^[0-9]+$ && "$created" == "$(git show -s --format=%ct HEAD)" ]] || { echo 'SOURCE_DATE_EPOCH must equal the source commit timestamp' >&2; exit 1; }
git diff --quiet HEAD -- || { echo 'Release source must have no tracked changes' >&2; exit 1; }
[[ -z "$(git ls-files --others --exclude-standard)" ]] || { echo 'Release source must have no untracked files' >&2; exit 1; }
mkdir -p "$output"

# The complete committed source supplies the matching helper, optional console,
# installer, migration files and guides. Ignored credentials are never archived.
git archive --format=tar --prefix="thinkpixelag-$version/" HEAD | gzip -n > "$output/thinkpixelag-source-$version.tar.gz"
if [[ "$component" == ag ]]; then
  export GOTOOLCHAIN="$(awk '$1 == "toolchain" {print $2}' go.mod)"
  [[ "$GOTOOLCHAIN" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Pinned toolchain required' >&2; exit 1; }
  binaries="$output/binaries/linux-$architecture"
  mkdir -p "$binaries"
  for command in thinkpixelag thinkpixelag-migrate thinkpixelag-operator; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=$version -X main.revision=$revision" \
      -o "$binaries/$command" "./cmd/$command"
  done
fi

tar --sort=name --mtime="@$created" --owner=0 --group=0 --numeric-owner \
  -czf "$output/thinkpixelag-kubernetes-$version.tar.gz" deploy/kubernetes
cp api/openapi/thinkpixelag.yaml "$output/thinkpixelag-openapi-$version.yaml"
cp api/schemas/*.json "$output/"

trivy image --quiet --scanners vuln --format json --output "$output/vulnerability-report.json" "$image"
trivy image --quiet --format cyclonedx --output "$output/$artifact-$version.sbom.cdx.json" "$image"
trivy image --quiet --exit-code 1 --severity CRITICAL,HIGH "$image"

# This is an honest local inventory. BuildKit/GitHub attestations and cosign
# signatures are separate outputs; writing JSON does not authenticate a builder.
python3 - "$image" "$revision" "$version" "$created" "$output/provenance.json" "$component" "$architecture" <<'PY'
import json, sys
image, revision, version, created, output, component, architecture = sys.argv[1:]
name, digest = image.rsplit("@sha256:", 1)
with open(output, "w") as stream:
    json.dump({"format_version": "thinkpixelag.release-artifacts/v1",
               "image": {"name": name, "digest": {"sha256": digest}},
               "source": {"git_commit": revision}, "version": version,
               "component": component, "platform": "linux/" + architecture,
               "source_date_epoch": int(created),
               "authentication": "unsigned-local-metadata"}, stream, indent=2)
    stream.write("\n")
PY

if [[ -n "${COSIGN_KEY:-}" ]]; then
  cosign sign --yes --key "$COSIGN_KEY" "$image"
  cosign attest --yes --key "$COSIGN_KEY" --type cyclonedx \
    --predicate "$output/$artifact-$version.sbom.cdx.json" "$image"
  cosign verify --key "${COSIGN_PUBLIC_KEY:?COSIGN_PUBLIC_KEY is required}" "$image" >"$output/signature-verification.json"
else
  printf 'release-artifacts: COSIGN_KEY unset; signing hook was not invoked\n'
fi

(cd "$output" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
