# Break-glass workflow contract

The break-glass boundary is `thinkpixelag.break-glass/v1`. It is an emergency,
just-in-time capability, not an administrator role and not a way to expand Run
authority. The closed scopes are `POLICY_RECOVERY` for one named policy
resource and `REVOCATION_RECOVERY` for one named revocation resource. Unknown
or broader scopes fail closed.

Activation requires a provider-verified phishing-resistant MFA assertion made
by the requesting principal no more than five minutes earlier. The verifier is
replaceable and returns only bound tenant/principal identity, authentication
time, and an opaque reference; raw assertions are not persisted. Provider
failure denies activation.

The exact tenant, principal, scope, resource, reason, and expiry form a
deterministic SHA-256 grant digest. A distinct four-eyes approval must bind that
digest and the corresponding `POLICY_BYPASS` or `GLOBAL_REVOCATION_CHANGE`
class. Activation consumes that approval atomically. A grant expires no later
than fifteen minutes after issue. It carries a random 256-bit credential shown
once; only its SHA-256 digest is stored. Every use must match the principal,
tenant, scope, resource, credential digest, issue time, expiry, and absence of a
terminal event.

Grant and lifecycle rows are append-only. Activation, each use, expiry, and
revocation append a separate `BREAK_GLASS` event conforming to
`thinkpixelag.evidence/v1` and the export outbox in the same transaction. The
event contains the grant digest and opaque references, never the credential or
strong-authentication assertion. Expiry is enforced at every use even if the
explicit `EXPIRED` event has not yet been reconciled.
