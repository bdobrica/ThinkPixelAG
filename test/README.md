# Test boundary

This directory is reserved for cross-package contract, integration, security,
and end-to-end test suites and their non-sensitive fixtures. Unit tests remain
next to the Go packages they exercise.

Test suites must be deterministic, parallel-safe, tenant-isolated, and runnable
through the Make targets introduced in ENG-008. The initial Go package verifies
that the required target surface and aggregate verification gates cannot be
removed accidentally.

Container contract tests enforce immutable builder/runtime pins, static build
flags, numeric non-root identity, OCI metadata, and the allowlisted Docker build
context. `container_smoke.sh` supplies the daemon-backed runtime proof through
`make container-smoke` and the aggregate `make verify` gate.
