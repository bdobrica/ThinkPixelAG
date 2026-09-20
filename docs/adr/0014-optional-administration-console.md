# ADR-0014: Optional administration console and backend bridge

- Status: Accepted
- Date: 2026-09-20
- Implementation: RC-111 console shell implemented; write screens and packaging follow in RC-112–114
- Supersedes: none; complements ADR-0005

## Context and decision

Operators need policy editing and governance state/configuration views without
making a browser UI necessary for AG. Build a separate `console/` package and
container using FastAPI, Jinja2 and minimal JavaScript. Its backend-for-frontend
(BFF) uses only AG's versioned HTTP APIs, the same APIs used programmatically.
AG remains a Go service and builds, starts and functions without Python or the
console. The console has no access to AG's database, OPA or peer administration
interfaces. Governance validation, policy promotion and authorization stay in AG.

Use OIDC authorization-code login with PKCE, state and nonce validation. Keep
caller tokens in expiring server-side sessions; use secure, HttpOnly, SameSite
cookies and CSRF protection. The BFF sends the caller's AG-audience access token;
AG verifies it normally. It never substitutes a shared administrator identity or
trusted role/tenant headers. Deployments whose IdP cannot issue the required
audience need a configured supported IdP flow, not signature/audience bypass.

One console replica with transient in-memory sessions is sufficient for this
RC. Restart requires login again; it never loses authoritative governance data.
The BFF pins its AG destination and uses bounded requests and redacted logs.
Escape policy text and API error content as data, never trusted HTML.

Views cover agents/Runs, effective integration status, external role mappings
and policy draft/validation/history/promotion. Saving a draft cannot activate
policy. Write forms carry the exact revision/digest and surface conflicts and
approval requirements from AG. File-managed settings are visibly read-only.
No signing keys or long-lived provider credentials enter the browser or BFF.

## Alternatives and consequences

React is viable but adds a separate asset pipeline unnecessary for these small
forms. Embedding templates in AG couples UI releases to governance; direct
database access would create a second authority path. Both are rejected.
The additional Python dependencies belong only to the optional component and
require pinned versions, focused tests and an independently built image.

## References

- [Service boundary](0005-service-boundaries-and-contracts.md)
- [Configuration ownership](0015-managed-governance-configuration.md)
- [Implementation sequence](../../TODO.md)
