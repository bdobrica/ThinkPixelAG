# Policy evaluation and lifecycle

Protected operations use the versioned `thinkpixelag.authorization/v1alpha1`
contract at `data.thinkpixelag.authorization.decision`. The Go boundary limits
and strictly validates inputs and outputs, applies a configured deadline,
accepts only registered non-sensitive reason codes, independently rejects
constraint expansion and unknown mandatory obligations, and denies when OPA or
fresh active-policy metadata is unavailable.

Bundles are bounded content-addressed artifacts. Promotion requires an exact
SHA-256 digest, a signature accepted by the configured `SignatureVerifier`, and
successful validation/compile testing through `BundleValidator`. Only verified
artifacts are persisted as `VALIDATED`. Activating a bundle serializably closes
the current activation and appends the next monotonic activation version.
Rollback uses the same operation to create a new activation of an older bundle;
history is never rewritten and activation versions never decrease.

Each process tracks the active tenant/channel digest and activation version
with a bounded age. Evaluation fails closed if that metadata is absent or
stale. Database activation success precedes the local update; if the local
update fails, the instance remains unable to authorize until reconciliation.
Decision evidence carries bundle digest/version and evaluation duration, not
raw policy input. Phase 6 will additionally bind freshness to revocation stream
state, and Phase 7 will supply managed KMS/HSM verifier implementations and
approval workflows for privileged promotions.

The baseline Rego denies by default. It covers discovery/invocation, lifecycle
and trusted workload operations, governance administration, tenant equality,
risk-sensitive authoritative checks, stale/gapped security state, and numeric
constraint narrowing. `make test-policy` compiles and tests it with the pinned
OPA version.

When Valkey is configured, the cache key incorporates the contract and active
policy versions, relevant global/tenant/agent epochs, verified tenant and
principal, action, resource, and normalized authorization-input digest. The
external key is SHA-256 hashed, and values are authenticated with a separately
configured HMAC-SHA-256 secret. Hits are strictly decoded, rebound to the
current decision ID, and revalidated against current input constraints. TTL is
the minimum of policy output, deployment maximum, and remaining security-state
freshness; zero-TTL decisions are never cached. Misses, timeouts, authentication
failures, malformed or forged values, and write errors bypass Valkey and use
OPA. Cache failure cannot create an authorization result.
