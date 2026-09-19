# API boundary

`openapi/thinkpixelag.yaml` is the canonical HTTP contract. Future generated
code and contract-test artifacts belong below this directory and must retain a
record of the generator version that produced them.

Transport implementations belong under `internal/adapters`; domain and
application packages must not depend on generated HTTP types.

The integration-RC boundary is frozen in [contract-freeze.json](contract-freeze.json).
See the [freeze and compatibility record](../docs/releases/contract-freeze.md);
`make contract-check` rejects unreviewed drift.
