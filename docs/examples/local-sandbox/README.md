# Local sandbox

Follow the [quick start](../../quickstart.md) in the same shell first. It starts
an isolated Compose project, exercises the end-to-end workflows and builds AG.
The dependency containers and database remain available between runs.

## Start and inspect the process

```sh
export THINKPIXELAG_ENVIRONMENT=local
export THINKPIXELAG_HTTP_ADDRESS=127.0.0.1:18080
export THINKPIXELAG_DATABASE_URL='postgresql://thinkpixelag_local:thinkpixelag_local_only_change_me@127.0.0.1:15432/thinkpixelag_local?sslmode=disable'
export THINKPIXELAG_OPA_URL=http://127.0.0.1:18181
export THINKPIXELAG_OIDC_ISSUER_URL=http://127.0.0.1:15556
export THINKPIXELAG_OIDC_AUDIENCE=thinkpixelag-example
.cache/bin/thinkpixelag
```

The issuer address is a configuration placeholder; this example does not start
an IdP. Leave `THINKPIXELAG_RUNTIME_FILE` unset for this operational-endpoint
walkthrough. In another terminal:

```sh
curl --fail http://127.0.0.1:18080/livez
curl --silent --output /dev/null --write-out '%{http_code}\n' http://127.0.0.1:18080/readyz
curl --fail http://127.0.0.1:18080/metrics
```

Liveness should succeed. Readiness is expected to return **503** because this
process has no verified active policy composition; the OPA dependency being
healthy does not establish authorization readiness. Governed `/v1` routes are
not enabled in this mode. Ctrl-C stops the process gracefully.

## Exercise governance

Rerun `make test-e2e` with the quick-start database URL to exercise the mounted
HTTP boundaries with controlled fixtures. The tests cover admission, exact
replay, lifecycle, resources and revocation without relying on a real harness.
For a persistent governed API, use the [runtime configuration](../../operations/configuration.md#governed-runtime-composition)
and [integration prerequisites](../../operations/integrations.md); approved
provisioning is additional integration work in this RC.

Use `make dev-status` to inspect dependencies. `make dev-down` stops the example
project and preserves its database volume. Keep the same Compose project name
and port variables when restarting with `make dev-up`. This stack is bound to
loopback and uses local-only credentials; do not expose it to other machines.
