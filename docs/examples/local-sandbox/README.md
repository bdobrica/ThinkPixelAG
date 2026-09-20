# Local installation

The [quick start](../../quickstart.md) installs AG and walks through authenticated
agent discovery, Run admission, replay, read and cancellation. This page explains
what is running and how to operate or adapt it.

## Components and configuration

| Component | Purpose | Configuration / storage |
|---|---|---|
| `api` | Published AG image, governed HTTP routes on loopback port 18080 | [Runtime JSON](../../../deploy/demo/runtime.json), generated environment, read-only CA volume |
| `postgres` | Authoritative governance state | Persistent `thinkpixelag-demo_database` volume; no host port |
| `opa` | Executes the approved example policy | Copy of the initially provisioned policy in the persistent trust volume |
| `identity` | Sample OIDC discovery/JWKS and durable evidence receiver | Private `thinkpixelag-demo_identity` volume; no host port |
| `provision` | Explicit migration and one-time sample setup | Completed container retained; subsequent launches preserve existing identities and activation |

[compose.yaml](../../../deploy/demo/compose.yaml) is the actual installation.
The [helper](../../../deploy/demo/ag) creates private configuration, invokes
Compose and waits for real AG readiness. AG cannot access the issuer's private
key volume. The separate example support image reuses the repository's existing
fixture provisioner; it is not included in the AG release image.

The sample issuer authenticates actual HTTP requests with signed JWTs. It does
not provide login screens or your company's SSO. The provisioner creates one
approved illustrative agent version and synthetic users. It does not execute
that agent or provide a supported production administration API.

## Change settings

Edit `AG_PORT` in the file reported by `./deploy/demo/ag config-path` to change the loopback port, then
run `./deploy/demo/ag up`. Keep your `AG_URL` consistent. Do not change the
initialized database password only in the environment file: PostgreSQL does not rotate its
stored password when a container environment changes.

Edit [runtime.json](../../../deploy/demo/runtime.json) to change admission
ceilings. These are deployment maxima, not grants to callers. Restart AG after
changing the file:

```sh
docker compose --project-name thinkpixelag-demo --env-file "$(./deploy/demo/ag config-path)" -f deploy/demo/compose.yaml restart api
```

All settings, including OIDC, evidence, database pools and optional Valkey, are
in the restored [configuration reference](../../configuration.md). Replacing
the sample issuer also requires matching verified tenant/principal identities
and approved data in AG. Merely pointing the issuer URL at a company IdP does
not create those records; see [installation boundaries](../../operations/installation.md).

## Persistence and credentials

Keep the private environment file and all three named volumes together. The database holds Run and
policy authority; identity storage holds sample keys and durable receipt history;
trust storage holds the CA and the exact provisioned policy. The helper does
not regenerate keys or reset a database on restart.

`./deploy/demo/ag token` issues a fresh 15-minute caller token without changing
its principal or signing key. Certificate lifetime is 14 days; this installation
is for short evaluations. For a longer evaluation, plan certificate renewal
without resetting authoritative state, or use managed identity infrastructure.
Do not use deletion/re-provisioning as credential rotation.

## Troubleshoot

| Symptom | Check |
|---|---|
| Port already in use | Set a free `AG_PORT` and use the same port in `AG_URL` |
| Provisioning exits unsuccessfully | `./deploy/demo/ag logs`; check PostgreSQL health and the explicit migration/setup result |
| Readiness remains unavailable | Check issuer/OPA health, policy activation and certificate validity; do not bypass readiness |
| API returns 401 | Refresh the caller token; verify URL, issuer/audience and clock |
| Agent list is empty | Confirm the provisioner succeeded and the request uses the sample caller's identity |
| API returns 409 | Inspect the current Run/state version and the original idempotent request |
| A Run remains ADMITTED | Expected without the still-missing harness-execution integration; AG has not launched the objective |

Use [Run API examples](../../api/run-lifecycle.md) for signals and event streams.
Use [backup/recovery](../../operations/backup-recovery.md) before keeping valuable
state. There is intentionally no automatic reset command in the helper.
