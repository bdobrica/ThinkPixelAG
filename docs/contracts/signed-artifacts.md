# Signed Privileged Artifact Contract

## Scope

ThinkPixelAG accepts policy bundles and these privileged configuration classes
only through the common signed-artifact verifier: trust-root sets, revocation
configuration, resource-override configuration, and privileged-agent-class
configuration. The catalog is closed; an unknown class or schema version fails
closed. Individual application workflows remain responsible for authorization,
four-eyes approval where required, semantic validation, monotonic revision
enforcement, persistence, and activation.

## Envelope and signature

An envelope carries an artifact class, schema/contract version, positive
revision, exact `sha256:` payload digest, bounded payload, and the SEC-003
signature metadata: provider key ID, immutable key version, algorithm, and
signature bytes. Payloads are opaque bytes at this boundary; each artifact
owner defines canonical serialization and validates semantics after signature
verification.

The managed signer signs a SHA-256 digest of this deterministic manifest:

```text
"thinkpixelag.signed-artifact/v1\0"
length(kind) || kind
length(format_version) || format_version
length(payload_sha256) || payload_sha256
uint64_be(revision)
```

Lengths are unsigned 32-bit big-endian values. `payload_sha256` is the 32 raw
digest bytes, not its hexadecimal representation. This domain separation and
length-prefixing prevent cross-protocol replay and ambiguous concatenation.

Verification recalculates the payload digest, checks the closed class/version
catalog, constructs the manifest, and calls the provider-neutral managed
verifier. A payload/digest, class, schema version, revision, key ID/version,
algorithm, or signature mismatch rejects the artifact before semantic
validation or persistence.

## Policy activation and compatibility

Policy bundles use class `POLICY_BUNDLE` and schema version
`thinkpixelag.authorization/v1alpha1`. Persistence records the bound artifact
revision and complete signature metadata. Activation rechecks the supported
contract and positive revision. Migration 016 marks legacy validated/approved
rows `REJECTED`; operators must re-promote them through the stronger envelope
rather than silently grandfathering an unverifiable signature binding.
