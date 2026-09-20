# Installations and usage examples

| Setup | What you get |
|---|---|
| [Local installation](local-sandbox/README.md) | Persistent PostgreSQL/OPA, real operator bootstrap, a loopback development issuer, optional console and harness commands |
| [Team Kubernetes PoC](team-poc/README.md) | Staged migration/bootstrap/API/optional-console resources using deployment-owned identity, dependencies and ingress |

The current candidate provides policy administration, managed role mappings,
OPA configuration and dynamic harness guidance. The UI is optional and forwards
the operator's own token. Both examples preserve local-development signing limits;
full AR/gateway execution and production qualification remain separate.

Existing rc.1 fixture examples remain under `deploy/demo` for retained older
installations. Do not reset their databases or treat them as fresh targets for
the new bootstrap. See [candidate evidence](../evidence/README.md) for exactly
which installation, browser and Kubernetes checks were performed.
