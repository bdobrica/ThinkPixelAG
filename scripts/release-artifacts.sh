#!/usr/bin/env bash
set -euo pipefail

version="${VERSION:?VERSION is required}"
revision="${REVISION:?REVISION is required}"
image="${IMAGE:?IMAGE must be an immutable name or digest}"
output="${OUTPUT_DIR:-dist}"
created="${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required for reproducibility}"
[[ "$image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] || { echo 'IMAGE must use an immutable sha256 digest' >&2; exit 1; }
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || { echo 'VERSION is not a safe artifact name' >&2; exit 1; }
[[ "$revision" =~ ^[a-f0-9]{40}$ && "$revision" == "$(git rev-parse HEAD)" ]] || { echo 'REVISION must equal the full checkout commit' >&2; exit 1; }
[[ "$created" =~ ^[0-9]+$ && "$created" == "$(git show -s --format=%ct HEAD)" ]] || { echo 'SOURCE_DATE_EPOCH must equal the source commit timestamp' >&2; exit 1; }
git diff --quiet HEAD -- || { echo 'Release source must have no tracked changes' >&2; exit 1; }
[[ -z "$(git ls-files --others --exclude-standard)" ]] || { echo 'Release source must have no untracked files' >&2; exit 1; }
mkdir -p "$output"

tar --sort=name --mtime="@$created" --owner=0 --group=0 --numeric-owner \
  -czf "$output/thinkpixelag-kubernetes-$version.tar.gz" deploy/kubernetes
cp api/openapi/thinkpixelag.yaml "$output/thinkpixelag-openapi-$version.yaml"
cp api/schemas/*.json "$output/"

trivy image --quiet --scanners vuln --format json --output "$output/vulnerability-report.json" "$image"
trivy image --quiet --format cyclonedx --output "$output/thinkpixelag-$version.sbom.cdx.json" "$image"
trivy image --quiet --exit-code 1 --severity CRITICAL,HIGH "$image"

# This is an honest local inventory. BuildKit/GitHub attestations and cosign
# signatures are separate outputs; writing JSON does not authenticate a builder.
python3 - "$image" "$revision" "$version" "$created" "$output/provenance.json" <<'PY'
import json, sys
image, revision, version, created, output = sys.argv[1:]
name, digest = image.rsplit("@sha256:", 1)
with open(output, "w") as stream:
    json.dump({"format_version": "thinkpixelag.release-artifacts/v1",
               "image": {"name": name, "digest": {"sha256": digest}},
               "source": {"git_commit": revision}, "version": version,
               "source_date_epoch": int(created),
               "authentication": "unsigned-local-metadata"}, stream, indent=2)
    stream.write("\n")
PY

if [[ -n "${COSIGN_KEY:-}" ]]; then
  cosign sign --yes --key "$COSIGN_KEY" "$image"
  cosign attest --yes --key "$COSIGN_KEY" --type cyclonedx \
    --predicate "$output/thinkpixelag-$version.sbom.cdx.json" "$image"
  cosign verify --key "${COSIGN_PUBLIC_KEY:?COSIGN_PUBLIC_KEY is required}" "$image" >"$output/signature-verification.json"
else
  printf 'release-artifacts: COSIGN_KEY unset; signing hook was not invoked\n'
fi

(cd "$output" && sha256sum -- * | grep -v ' SHA256SUMS$' > SHA256SUMS)
