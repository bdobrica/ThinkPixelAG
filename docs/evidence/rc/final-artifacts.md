# 0.1.0-rc.1 artifact qualification (RC-012)

The integration candidate was built on 2026-09-19 from the clean final
documentation-transition commit:

```text
a64d323d5c3bfa667e063b5f9dcd69f3cb89258f
```

The immutable multiarchitecture image is:

```text
quay.io/bdobrica/thinkpixelag@sha256:82ad793f6b19460e58aeb43203bfd2f8a3563bc2d8bf53a120e92dbafa78a53a
```

The convenience image tag is `0.1.0-rc.1-a64d323d5c3b`; use the digest for
deployment. No semantic Git tag or formal GitHub release was created.
This later evidence-only commit records the build; it is not the image source.

| Platform | Explicit manifest digest |
|---|---|
| linux/amd64 | `sha256:a07335298fe22e296419ae8b882f14a3f85eeb9000eae4f59cf3abb481fa838a` |
| linux/arm64 | `sha256:a52f300345d7eb9fbde9c391b5fdc46ebaedbc9e80d2a49a83bb85182ace3c5b` |

## Identity and artifacts

Both platform configurations report the expected architecture, full source
revision, version `0.1.0-rc.1`, source creation label and Apache-2.0 license.
Each platform's local `provenance.json` binds its actual OCI SHA-256 to that
source/version. BuildKit provenance records the same VCS revision and
REVISION/VERSION/CREATED build arguments. The index's attestation-manifest
annotations reference the corresponding platform manifests.

Both explicit platform scans report **zero HIGH/CRITICAL findings**, including
unfixed findings. Each retains four MEDIUM and two UNKNOWN binary occurrences,
enumerated in the [inventory](rc012-results/inventory.json). SBOMs, vulnerability
reports, OpenAPI, four JSON Schemas, Kubernetes archives and metadata were
generated from the same source. Every platform checksum verifies; shared
API/schema/Kubernetes bytes match across architectures, and the OpenAPI copy
matches the frozen source. Both per-platform bundle checksums also verify.

Local bundles are retained under ignored `.cache/rc/final/`:

```text
thinkpixelag-0.1.0-rc.1-amd64.tar.gz
thinkpixelag-0.1.0-rc.1-arm64.tar.gz
SHA256SUMS
```

Their SHA-256 values and the complete contained artifact inventory are committed
in [machine-readable evidence](rc012-results/inventory.json). The OCI index and
BuildKit attachments were pushed to the authorized Quay repository. The bundles
were generated locally; they were not uploaded as GitHub release assets.

BuildKit attachments and local metadata were checked for consistency, not
authenticated as a trusted remote builder. Cosign signing and GitHub OIDC
attestation were **not executed**. The local inventory explicitly identifies
itself as unsigned metadata; it is not a SLSA verification claim. Signature
hooks and the tag-triggered draft-release workflow remain available for a
separately executed formal publication.

## Exact-image runtime verification

The final AMD64 platform manifest passed a fresh restricted runtime smoke:
UID 65532, read-only root, dropped capabilities, no privilege escalation,
present CA roots, absent shell, liveness 200, fail-closed readiness 503 with
unconfigured dependencies, and SIGTERM exit 0. The stopped smoke container is
retained. See [AMD64 results](rc012-results/amd64-smoke.json).

The retained ARM64 Kubernetes API deployment now uses the final index digest.
Three API pods are ready; their runtime image IDs resolve to the expected
platform/config/index identity. Fresh admission, exact replay, cross-tenant
denial, forged-token denial and Run-read smoke passed. Probes and restricted
security contexts remain enabled. See [Kubernetes results](rc012-results/kubernetes-smoke.json).

Final state: schema 18, zero pending outbox messages, three ready API replicas,
one ready Valkey replica and HPA bounds 3–4. Database/sink/exporter fixtures and
all homelab resources remain retained; their prior failure-domain limitations
are unchanged. See [state aggregates](rc012-results/retained-state.json).

The source transition's [full gate](rc011-verification.json) passed 2,303
test/subtest executions, 27 policy cases and the restricted container smoke
with zero skips. Final artifact builds/scans and both runtime checks add
exact-image evidence without claiming a new full production qualification.
The [release notes](0.1.0-rc.1.md), [scope/deferrals](../../adr/0012-integration-rc-qualification-deferrals.md)
and [risk record](risk-review.md) remain applicable.
