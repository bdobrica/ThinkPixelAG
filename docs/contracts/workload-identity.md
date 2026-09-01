# Workload identity and service authorization

## Boundary

The deployment binding boundary is `thinkpixelag.workload-identity/v1`, with
the closed schema in
[`api/schemas/workload-identity-v1.json`](../../api/schemas/workload-identity-v1.json).
It maps one exact client-certificate URI SAN to a stable principal, tenant, and
closed set of service roles. The mapping is deployment-owned configuration;
certificate subjects, DNS SANs, headers, source addresses, namespaces, and VPC
membership carry no governance authority.

## Authentication

Production trusted routes MUST be served by a TLS listener configured with
`ClientAuth` equivalent to `RequireAndVerifyClientCert`, explicit trust roots,
TLS 1.2 or newer, and normal certificate time and chain validation. The mTLS
adapter consumes only `VerifiedChains`, requires the verified leaf to be the
presented peer certificate, requires exactly one URI SAN, and performs an exact
binding lookup. Missing, unverified, ambiguous, unknown, or malformed identity
fails closed. Trust-root distribution and listener certificate rotation remain
deployment adapters and MUST NOT change this contract.

Trusted route middleware rejects bearer credentials and forwarding identity
headers. Public and administrative routes continue to require OIDC bearer
authentication and cannot use a workload certificate to acquire human or
administrative roles.

## Authorization

Authentication alone grants no action. Each call still enters the
`thinkpixelag.authorization/v1alpha1` policy boundary with principal type
`workload`, the mapped tenant/principal/roles, the exact action and resource,
and required authoritative freshness. Roles are non-hierarchical:

| Route class | Action | Required role |
|---|---|---|
| usage ingestion | `resources.meter` | `trusted-meter` |
| reservation settlement | `resources.settle` | `trusted-settler` |
| run claim/heartbeat/completion | corresponding run action | `trusted-workload` |
| revocation stream/reconcile | `revocations.reconcile` | `trusted-gateway` |

Tenant and resource IDs are re-bound after authentication. Service roles can
never authorize policy, registry, resource-administration, or
revocation-administration actions.

## Local development exception

Only `local` and `test` deployments may terminate TLS outside the process or
use an explicit test verifier. Such a verifier must inject a fixed, allowlisted
workload binding; it may not trust identity headers, source IP, arbitrary bearer
claims, or request bodies. Production wiring MUST fail when trusted routes are
enabled without verified mTLS. The exception changes transport only: policy
authorization, tenant/resource binding, and role separation remain mandatory.
