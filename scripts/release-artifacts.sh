#!/usr/bin/env bash
set -euo pipefail

version="${VERSION:?VERSION is required}"
revision="${REVISION:?REVISION is required}"
image="${IMAGE:?IMAGE must be an immutable name or digest}"
output="${OUTPUT_DIR:-dist}"
created="${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required for reproducibility}"
mkdir -p "$output"

tar --sort=name --mtime="@$created" --owner=0 --group=0 --numeric-owner \
  -czf "$output/thinkpixelag-kubernetes-$version.tar.gz" deploy/kubernetes
cp api/openapi/thinkpixelag.yaml "$output/thinkpixelag-openapi-$version.yaml"
cp api/schemas/*.json "$output/"

trivy image --quiet --scanners vuln --format json --output "$output/vulnerability-report.json" "$image"
trivy image --quiet --format cyclonedx --output "$output/thinkpixelag-$version.sbom.cdx.json" "$image"
trivy image --quiet --exit-code 1 --severity CRITICAL,HIGH --ignore-unfixed "$image"

cat >"$output/provenance.json" <<EOF
{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"$image","digest":{"gitCommit":"$revision"}}],"predicateType":"https://slsa.dev/provenance/v1","predicate":{"buildDefinition":{"buildType":"https://github.com/bdobrica/ThinkPixelAG/release/v1","externalParameters":{"version":"$version","sourceDateEpoch":$created}},"runDetails":{"builder":{"id":"https://github.com/bdobrica/ThinkPixelAG/actions"}}}}
EOF

(cd "$output" && sha256sum -- * | grep -v ' SHA256SUMS$' > SHA256SUMS)

if [[ -n "${COSIGN_KEY:-}" ]]; then
  cosign sign --yes --key "$COSIGN_KEY" "$image"
  cosign attest --yes --key "$COSIGN_KEY" --type cyclonedx \
    --predicate "$output/thinkpixelag-$version.sbom.cdx.json" "$image"
  cosign verify --key "${COSIGN_PUBLIC_KEY:?COSIGN_PUBLIC_KEY is required}" "$image" >"$output/signature-verification.json"
else
  printf 'release-artifacts: COSIGN_KEY unset; signing hook was not invoked\n'
fi
