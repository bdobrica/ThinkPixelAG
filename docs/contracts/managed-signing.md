# Managed Signing and Verification Contract

## Boundary

Signing and verification use the provider-neutral interfaces in
`internal/ports/signing.go`. Callers submit a SHA-256 digest and an opaque key
identifier; they never request, receive, or retain private-key material. A
signature carries the provider key ID, immutable key version, closed algorithm,
and signature bytes. Key inspection returns only bounded metadata describing
status, purpose, protection, and exportability.

The digest boundary matches managed KMS and HSM APIs that sign prehashed
content and avoids sending multi-megabyte artifacts to a provider. The caller
is responsible for canonicalizing the artifact and computing the digest before
signing. Artifact-specific signature envelopes and version rules are defined by
the [signed privileged artifact contract](signed-artifacts.md).

## Production invariants

Production configuration MUST select `kms` or `hsm`, a managed key resource ID,
and one of `ED25519`, `ECDSA_SHA256`, or `RSA_PSS_SHA256`. The service has no
private-key or private-key-file configuration field. Unknown prefixed settings
fail startup, and key IDs that resemble PEM content, file URIs, or filesystem
paths are rejected.

Before use, the managed-key guard obtains provider-authoritative metadata and
requires:

- an enabled, immutable key version matching the requested key;
- the configured signing algorithm;
- sign or verify purpose appropriate to the operation;
- KMS/HSM protection and `exportable=false`.

Every returned signature is rebound to the inspected key ID, version, and
algorithm. Provider outage, disabled/rotated key mismatch, exportable/software
key metadata, unknown algorithm, empty/malformed digest, or mismatched
signature metadata fails closed. A provider adapter authenticates with workload
identity and narrow key grants; credentials are not part of this contract.

Local and test deployments may leave signing disabled. Test-only in-memory
keys do not satisfy the managed-key guard and therefore cannot be promoted into
the production path accidentally.
