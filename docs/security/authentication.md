# Authentication and tenant mapping

ThinkPixelAG verifies access tokens against exactly one configured OIDC issuer
and audience per process. Discovery must return that issuer verbatim and a
same-origin JWKS URI. Production uses normal HTTPS certificate verification.

JWT acceptance is fail closed. Protected-route middleware requires one Bearer
credential and checks the configured algorithm allowlist, `kid`, signature,
issuer, audience, subject, `iat`, optional `nbf`, `exp`, clock skew, and maximum
token lifetime. RS256 is the default; ES256 may be explicitly enabled. Unsigned
tokens and symmetric keys are unsupported.

JWKS responses are bounded to 1 MiB. Provider `max-age` advice is clamped
between configured minimum and maximum TTLs. An unknown key ID or expired cache
causes synchronous refresh, making signing-key rotation immediately
discoverable. During an IdP outage, a previously verified known key remains
usable only until the separate stale limit. After that, authentication returns
a retryable unavailable result. Initial verifier construction requires
successful discovery and key retrieval.

Verified `sub` becomes the principal ID and the configured tenant claim becomes
the tenant ID. Token roles have no direct authority: only exact external roles
in the configured mapping become internal roles; unmapped values are discarded
and output is de-duplicated and sorted. Ordinary tenant/principal/role and
`X-Forwarded-*` identity headers are rejected. Request schemas must omit tenant
fields; the shared tenant-hint guard rejects them even when they match the token.

Internal roles are deliberately non-hierarchical. Caller access uses
`agent-invoker`; service identities use the separate `trusted-workload`,
`trusted-meter`, `trusted-settler`, and `trusted-gateway` roles. Administrative
identity is split into `registry-admin`, `resource-admin`, `policy-admin`, and
`revocation-admin`. There is no umbrella administrator or service role. A
principal that needs more than one capability must receive every role explicitly
through the trusted OIDC mapping, and policy evaluates the role required for the
specific action.

Break-glass is not a role. Activation uses a replaceable strong-authentication
verifier and requires provider-verified phishing-resistant MFA within five
minutes, exact digest-bound four-eyes approval, and a maximum fifteen-minute
grant. Raw step-up assertions and grant credentials are not persisted. See
[the break-glass workflow contract](../contracts/break-glass.md).

Operational probes remain anonymous and contain no tenant data. Every future
public, trusted, and administrative route must apply bearer authentication
before policy authorization. Tokens and Authorization headers must never enter
logs, traces, metrics, policy input, or client-visible error detail.
