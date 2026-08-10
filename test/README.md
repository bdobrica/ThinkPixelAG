# Test boundary

This directory is reserved for cross-package contract, integration, security,
and end-to-end test suites and their non-sensitive fixtures. Unit tests remain
next to the Go packages they exercise.

Test suites must be deterministic, parallel-safe, tenant-isolated, and runnable
through the Make targets introduced in ENG-008. The initial Go package verifies
that the required target surface and aggregate non-container gates cannot be
removed accidentally.
