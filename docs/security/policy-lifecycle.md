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
raw policy input. Revocation freshness is bound to the action-risk catalog.
Managed signing and verification use digest-based, key-version-bound ports and
reject exportable or software-protected production keys as defined by the
[managed signing contract](../contracts/managed-signing.md). The
[signed-artifact contract](../contracts/signed-artifacts.md) binds the policy
class, contract version, artifact revision, payload digest, and managed-key
metadata. Activation rejects incompatible versions, and legacy validations
must be re-promoted through that boundary.

The baseline Rego denies by default. It covers discovery/invocation, lifecycle
and trusted workload operations, governance administration, tenant equality,
risk-sensitive authoritative checks, stale/gapped security state, and numeric
constraint narrowing. `make test-policy` compiles and tests it with the pinned
OPA version.

The action-risk catalog is closed and shared with application freshness
enforcement. Policy grants service actions only to their dedicated workload,
meter, settlement, or gateway roles and grants administrative actions only to
their dedicated registry, resource, policy, or revocation roles. These roles
are non-hierarchical; cross-role escalation tests are part of the golden policy
suite. Unknown actions deny and take the authoritative freshness path.

Server-selected versions use `runs.create`. Explicit historical pins require a
separate authoritative, zero-TTL `versions.pin` governance decision, and use of
a deprecated rollback candidate requires `versions.rollback`. The controlled
selection and invocation decisions must come from the same active policy
digest/version; a mid-resolution activation change fails closed.

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

Each evaluator also has a bounded 1,024-entry process-local cache in front of
Valkey. A successful policy activation/rollback or accepted revocation stream
or reconciliation observation synchronously advances that tenant's local cache
generation and removes its local entries. The generation is included in the
hashed Valkey key alongside policy and epoch versions, so earlier external
entries become unreachable immediately and expire under their original bounded
TTL. Invalidation never depends on listing or deleting remote keys, and a
Valkey outage cannot preserve reachability of an invalidated decision.
