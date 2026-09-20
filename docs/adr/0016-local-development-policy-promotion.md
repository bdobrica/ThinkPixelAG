# ADR-0016: Explicit local-development policy promotion profile

- Status: Accepted
- Date: 2026-09-20
- Implementation: RC-103/104 provide local signing and authenticated approval
  receipts; RC-107 operator provisioning remains pending
- Supersedes: ADR-0010 only where local/test signing was limited to disabled or
  test-only keys; production managed-key requirements are unchanged

## Context and decision

The owner selected a local-only development walkthrough instead of a cloud
KMS/HSM dependency. Provide an explicitly enabled local-development signing
profile and an OIDC-authenticated local approval adapter for this RC. This is
functional qualification of promotion and four-eyes workflows, not production
key custody or external approval-provider qualification.

The local signer uses a generated Ed25519 software key persisted in a protected
operator-owned directory/volume outside the checkout. Implement the existing
Signer/Verifier/KeyInspector ports and report truthful SOFTWARE protection and
exportability. An explicit development-only composition selects this adapter;
never weaken the managed-key guard or claim software keys are non-exportable
KMS keys. Production configuration rejects the development profile and local
key paths. Do not auto-fallback to it when a managed provider is unavailable.

Keep key IDs and versions immutable, validate the same domain-separated artifact
manifest/digest/signature, and preserve compile checks, monotonic activation,
rollback approvals and transaction-bound evidence. Mount the key only in the
trusted signing host, never the BFF, browser, harness or instruction response.
Use a distinct development trust set; production rejects its artifacts even
when the wire signature algorithm is supported. Mark installation status and
qualification evidence as local-development. Document key persistence, rotation
and recovery, including filesystem permission checks, before the walkthrough.

For local approvals, use two distinct OIDC-authenticated tenant operators with
the action's required role. A replaceable approval adapter records an
authenticated, immutable decision receipt binding tenant, approval ID, provider
reference, action digest, approver, decision and server timestamp. Verify against
that receipt through the ApprovalProvider port; never treat a caller-supplied
reference or unverified approver ID as evidence. Receipt issuance requires the
verified caller identity and action authorization. Preserve requester/approver
separation, expiry and single-use consumption in the action transaction. No
auto-approval endpoint or test stub serves the real local walkthrough.

## Setup and limits

RC-103/104 must supply these adapters and protected receipt persistence; this
record does not pretend they already exist. RC-107 provisioning creates the
development key/trust reference and two local IdP identities using protected
configuration. No paid resources or cloud credentials are needed. Localhost
exposure is the example default; remote team-PoC use requires explicitly
configured TLS and authentication. Local filesystem access can compromise keys;
two local identities exercise separation but do not establish an independent
enterprise approval authority or phishing-resistant MFA.

Production still requires managed KMS/HSM keys and qualified approval-provider
integration under ADR-0010. Break-glass MFA requirements are not relaxed. The
next RC can close the local workflow with its limits recorded; it cannot claim
production provider qualification on that evidence.

## Alternatives and references

AWS KMS or an existing HSM remains a production adapter option. Requiring one
now would block the requested local RC; unsigned policy or bypassing approvals
would skip the behavior we need to exercise. A separate development profile
retains those checks while making the custody limitation explicit.

- [Managed signing](../contracts/managed-signing.md)
- [Signed artifacts](../contracts/signed-artifacts.md)
- [Governance approvals](../contracts/governance-approvals.md)
- [Production authority](0010-privileged-authority-and-managed-keys.md)
