# Local dependency stack and deployment boundary

The root `compose.yaml` provides the development-only dependency stack. Images
are pinned by immutable multi-platform digest: PostgreSQL 18.4 is authoritative,
OPA 1.19.0 evaluates policy, and Valkey 9.1.1 is an optional disposable cache.
It does not represent the production topology or production security settings.

```sh
make dev-up          # PostgreSQL and OPA; wait until healthy
make dev-smoke       # verify versions plus positive/negative authentication
make dev-status
make dev-down        # preserve the PostgreSQL named volume

make dev-up-valkey   # include optional Valkey
make dev-reset       # delete this Compose project's containers and volumes
```

All published ports bind only to `127.0.0.1`. PostgreSQL and Valkey use explicit
local-only credentials; Valkey persistence is disabled because it is never
authoritative. Copy `deploy/local.env.example` to the ignored root `.env` only
when a port is already occupied or isolated credentials need changing. Changing
PostgreSQL initialization values requires `make dev-reset`; that command
irreversibly removes the local database volume, but does not target other
Compose projects.

The OPA server intentionally starts without policies until Phase 3. A healthy
OPA process is not evidence that an application policy is loaded.

Future OCI and Kubernetes assets also live here. Production credentials must
use managed secret delivery, dependencies must use encrypted authenticated
transport, and this local Compose file must not be deployed to Kubernetes.
