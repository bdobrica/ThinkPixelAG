# Example setups

| Example | Use it for | What you need |
|---|---|---|
| [Local sandbox](local-sandbox/README.md) | Exercise governance workflows and inspect a local process | Docker Compose, Go and Make; synthetic test identities |
| [Team Kubernetes PoC](team-poc/README.md) | Adapt the restricted deployment to your team's infrastructure | Kubernetes, durable PostgreSQL, OPA, OIDC, secret delivery and approved provisioning |

These examples use the existing pinned Compose stack and Kubernetes base, so
security settings and image pins have a single source. The team example is an
environment template, not a claim of turnkey production provisioning or a
working AR/Codex integration. See the [integration boundary](../operations/integrations.md).

## Validation scope

On 2026-09-20, the local quick-start dependency checks, PostgreSQL end-to-end
workflows, build and operational-endpoint walkthrough were executed on AMD64.
The team PoC overlay was rendered with `kubectl kustomize`; it was not deployed
or qualified against a company's identity, storage or network infrastructure.
