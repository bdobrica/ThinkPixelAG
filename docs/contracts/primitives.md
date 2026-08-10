# Domain Primitive Contracts

These dependency-free primitives are the common vocabulary for domain,
application, persistence, and transport code. Adapters convert at their
boundary instead of weakening these invariants.

## Identifiers

`domain.ID` accepts and emits only canonical lowercase RFC 9562 UUIDv7 text.
Generation uses the current UTC Unix millisecond timestamp and cryptographic
randomness. IDs are opaque: ordering, creation time, tenant, or authorization
must never be inferred from them. Database and HTTP adapters must parse before
use and reject other UUID versions.

## Time

`domain.Clock` is the injectable authoritative-time interface and
`domain.SystemClock` returns UTC. Persisted/domain timestamps pass through
`domain.RequireUTC`, which rejects non-UTC locations and removes Go's
process-local monotonic component. Adapters parse RFC 3339 inputs before this
validation; database precision will be fixed with the Phase 2 schema.

## Exact numbers and resources

`domain.Decimal` stores a signed 64-bit coefficient and a scale from zero to
18. Its canonical transport form is a JSON string, never a JSON number. Parsing
rejects exponent notation, whitespace, leading plus/zero variants, negative
zero, excessive scale, and coefficient overflow. Arithmetic is checked and
requires matching scales; callers must perform explicit, policy-approved
conversion rather than implicit rounding.

`domain.Quantity` couples a nonnegative decimal with a canonical lowercase
unit name. Arithmetic rejects unit/scale mismatch, overflow, and underflow.
Later resource-dimension definitions further constrain allowed units, scale,
and maxima; the primitive intentionally does not treat arbitrary units as
registered governance dimensions.

## Pagination cursors

`domain.CursorCodec` encodes a versioned sort key plus UUIDv7 tie-breaker and
authenticates it with HMAC-SHA-256. Keys must contain at least 32 bytes and be
provided through secret configuration when HTTP pagination is wired. Cursors
are canonical unpadded base64url, capped at the OpenAPI 512-character limit,
and reject tampering, unknown versions, invalid IDs, and oversized fields.
Cursors convey continuation state only and never authorization.

## Typed errors

`domain.Error` carries a stable code, safe public detail, retry hint, and an
optional wrapped diagnostic cause. Its `Error` text never includes the cause.
Unknown and untyped errors map to `internal`; ENG-007 maps the closed code set
once to RFC 7807 status/type values and must not expose wrapped causes.

Malformed-input fuzz targets cover ID, decimal, and cursor decoding. Checked
arithmetic, cursor authentication, public error rendering, and UTC enforcement
also have deterministic unit and race-tested coverage.
