# Authoritative PostgreSQL Schema

The initial Phase 2 schema is defined by migrations `001` through `005`. It is
normalized around the transactions that must remain authoritative even when an
API replica, worker, cache, or downstream evidence sink fails.

## Tenant boundary

Tenant-owned tables carry `tenant_id`. Relationships between tenant-owned rows
use composite foreign keys such as `(tenant_id, run_id)` so an identifier from
one tenant cannot be attached to a row from another tenant. Their primary query
indexes begin with `tenant_id`. Globally ordered streams (`revocation_log` and
`audit_events`) use database identity sequences and add tenant/sequence indexes
for filtered consumption.

Repository queries must still supply an explicit tenant predicate. ADR-0002
records the DATA-012 evaluation: the RC deliberately uses repository enforcement
instead of PostgreSQL row-level security. The current pooled, partly
non-transactional repository contract cannot safely bind `SET LOCAL` tenant
context on every operation; ordinary policies would also be bypassed by the
current table-owning development role, while global workers require deliberate
cross-tenant access. Enabling RLS in that shape would create false assurance.

RLS remains a future defense-in-depth option only with separate non-owner
tenant-runtime, global-worker, and migration roles; transaction-scoped trusted
tenant context; complete policies (including nullable global records); owner
bypass protection; and adversarial pool-reuse and worker tests. Until those
conditions are implemented together, explicit tenant-first parameterized SQL,
closed repository entry points, enumeration-safe errors, tenant-leading indexes,
and real-PostgreSQL isolation tests are the enforced boundary. Composite keys
are an additional integrity boundary, not a substitute for authorization.

The Phase 2 repository substrate binds a validated UUIDv7 tenant before any
lookup and exposes only a closed allowlist of tenant-addressable record kinds.
Every identity query uses `tenant_id` as its first predicate and maps both a
missing identifier and another tenant's identifier to the same not-found
result. Table names cannot be supplied by callers. Aggregate child rows with
composite keys are reached through their tenant-owned parent aggregate rather
than exposed as independent identifiers. The repository set can be rebound to
the restricted `DBTX` passed to a transaction callback, without exposing commit
or rollback authority. Typed aggregate projections and mutations are added in
their owning lifecycle phases; this substrate prevents those adapters from
falling back to unscoped identity lookups.

Runtime connections use a bounded pgx pool with configurable minimum/maximum
connections, lifetime, idle time, and connect deadline. PostgreSQL enforces
`statement_timeout` and `lock_timeout` on every pooled session. Dependency
readiness uses its own short deadline and fails closed, while liveness remains
independent of a database outage. Query telemetry records only bounded operation
and outcome categories; SQL text, parameters, tenant IDs, and resource IDs are
excluded from metrics and spans. Driver errors are classified into stable
constraint, conflict, cancellation, timeout, and availability categories, with
retry advice limited to transient serialization, deadlock, lock, and connection
failures.

## Registry and policy

`tenants` and `principals` anchor ownership. `agents` reference same-tenant owner
and sponsor principals. `agent_versions` store content-addressed manifests, and
capabilities and append-only approval decisions reference the matching tenant,
agent, and version. A partial unique index permits at most one currently active
policy activation per tenant/channel while activation versions retain history.

Version manifests, capability declarations, approvals, and policy artifacts are
treated as immutable history. The schema exposes no update timestamp for these
records. Repository methods omit update/delete authority for them, and
database triggers reject direct updates or deletes of registered agent versions
and their normalized capability rows. Approval, deprecation, and revocation
decisions are serialized by locking the version row, then the decision, audit
event, and outbox event are inserted atomically. A trigger also rejects direct
updates or deletes of that decision history.

## Runs and resolution

`runs.state_version` supports optimistic state transitions, while worker leases
carry a monotonically increasing fencing token. Each run has one immutable
`run_version_resolutions` snapshot of the selected version, approval, policy
digest/version, and normalized resolution evidence. Signals are idempotent per
run and key. Run-event sequence numbers are unique per tenant/run, providing a
stable append-only cursor independent of event timestamps.
Resolution persistence locks and rechecks the selected version's current
approval state, verifies that the run, version digest, and originating approval
match, and rejects stale resolution after revocation. A database trigger rejects
updates and deletes of committed snapshots.

## Resource accounting

Resource definitions bind a class, canonical unit, numeric scale, range, and
aggregation rule. Immutable envelope grants are separated from lockable mutable
balances. Reservation and settlement headers are one-to-one for exactly-once
closure; settlement items enforce `returned = reserved - consumed` without
overflow or negative values. Trusted usage is unique by tenant, producer, and
source event ID, with a content digest available to detect mismatched replay.

`resource_rate_windows` is the authoritative fixed-window structural
throughput counter. Its tenant/envelope/dimension/window primary key serializes
competing increments, its grant foreign key prevents detached counters, and a
conditional upsert prevents the resulting count from crossing the immutable
envelope grant. Valkey contains only disposable exhausted-window markers.

The trusted-usage transaction also locks the owning run before changing a
consumable balance. A zero result advances the run through the exhaustion and
policy disposition states, clears its lease, advances its fencing token, and
inserts ordered run events, audit evidence, and matching outbox records before
commit. No additional exhaustion table or asynchronous correctness path is
required.

Child reservation locks the parent envelope and then consumable balance rows in
dimension-UUID order. In one transaction it validates topology ceilings, moves
each reserved amount from parent availability to open allocation, creates the
child envelope and immutable grants, and initializes child balances. This lock
order is shared by every multi-dimension allocation path; a failed dimension or
structural check rolls back the complete child aggregate.

`resource_extensions` and their immutable items preserve the original grant.
Each approved additive action records its actor, decision, approval reference,
and prior/new value while advancing the envelope version. Consumable additions
credit availability; deadline additions move only forward. The run, extension,
audit, ordered event, and outbox evidence commit together, including an atomic
resume when a paused exhausted run receives new capacity.

Settlement locks the open reservation, child balances, and parent balances. A
unique reservation settlement derives consumption from the child balances,
rolls descendant consumption into the parent, credits only unused capacity,
and closes the reservation in one transaction. Reconciliation claims eligible
terminal or expired reservations with `SKIP LOCKED`, fences and times out live
expired children, and uses the same unique close boundary. Open descendants are
held for a leaf-first later pass. These constraints and locks preserve
`available + direct_consumed + allocated_open = effective_grant` and make retry
or concurrent replicas unable to oversubscribe or double-credit resources.

## Revocation ordering

Global, tenant, and agent epoch rows store checked nonnegative `bigint` values.
Updates must lock the applicable rows and reject overflow. Revocations retain
append-only create/lift/expiry changes; `revocation_log.sequence` supplies one
global committed order and snapshots the resulting epoch vector. Gateway
checkpoints persist the last fully applied order and freshness observations.

## Delivery primitives

Idempotency ownership is unique by tenant, principal, route, and key and binds
that scope to a normalized SHA-256 request hash. Completion constraints require
a response status and timestamp. Audit events form globally ordered,
tenant-filterable evidence with hash-link fields. Outbox messages contain the
availability, claim owner/token/expiry, attempt, publication, and dead-letter
state needed for replay-safe bounded delivery.

The idempotency repository acquires a scoped key with one atomic PostgreSQL
upsert. Exactly one concurrent caller receives an opaque owner token; other
same-hash callers observe an in-progress result, while a different hash receives
the enumeration-safe idempotency conflict. Completion is fenced by tenant,
record ID, owner token, and `IN_PROGRESS` state and stores status, headers, and
body for byte-stable replay. A failed operation or expired lease can be
reacquired only with the same hash and a fresh owner token, so a stale worker
cannot complete after takeover. Once the record TTL expires, the scope may be
atomically replaced, including by a new request hash. Transport adapters remain
responsible for producing canonical normalized request bytes before applying
SHA-256.

Domain mutations that require evidence use one callback-owned PostgreSQL
transaction to write the mutation, immutable audit event, and outbox envelope;
any failure rolls back all three. Audit hashes are derived deterministically
from the exact validated event representation, and optional previous hashes
support tamper-evident linking without making application logs authoritative.

Publishers claim a bounded, availability-ordered batch with `FOR UPDATE SKIP
LOCKED`, increment the attempt count, and issue a fresh UUIDv7 claim token under
a finite lease. Sink delivery is at least once and the outbox message UUID is
the receiver's deduplication key. Publication, retry, and dead-letter updates
compare both message ID and claim token so an expired claimant cannot overwrite
a takeover. Transient failures use capped exponential retry with bounded
jitter; permanent failures and exhausted attempts become poison messages with
a controlled error code and dead-letter timestamp. They remain stored for
operator inspection/replay rather than blocking later messages.

## Migration and compatibility rules

The files are contiguous Tern migrations and include reverse SQL for local
development and pre-release rehearsal. Once shared or released, a migration is
immutable: `migrations/checksums.sha256` verifies every released migration
before database work begins, and production correction is a new forward
migration. API replicas never apply schema changes; an explicit migration job
advances the version table before replicas that require the new schema become
ready.

Schema evolution follows expand/migrate/contract compatibility. An expansion
must preserve existing rows and the prior application's reads and writes;
destructive renames, removals, incompatible type changes, and newly mandatory
fields wait for a later contract release after old replicas have drained.
Migration qualification creates isolated databases and proves a full empty
install, a data-preserving upgrade from the preceding fixture, representative
check/unique/foreign-key constraints, critical tenant and outbox access paths,
and transactional forward recovery after a deliberately failed migration.
