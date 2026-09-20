# Quick start

This walkthrough builds AG and exercises governed Run behavior against a real,
isolated PostgreSQL database. It uses synthetic identities and policy fixtures;
it does not require an IdP account, a cluster or a harness.

Prerequisites: Git, Bash, GNU Make, Docker Engine with Compose v2, and Go 1.26.6
(the repository's pinned toolchain). Run commands from the repository root.
Allow network access for pinned images and Go modules. See the
[tested version matrix](operations/supported-versions.md).

```sh
export COMPOSE_PROJECT_NAME=thinkpixelag-example
export POSTGRES_PORT=15432
export OPA_PORT=18181
make dev-up
make dev-smoke
make test-e2e TEST_DATABASE_URL='postgresql://thinkpixelag_local:thinkpixelag_local_only_change_me@127.0.0.1:15432/thinkpixelag_local?sslmode=disable'
make build
```

Use free local ports. These credentials are public, local-only defaults. Do not
point tests at an existing application database: the suites migrate and insert
synthetic data. The example project keeps its own named PostgreSQL volume.
If a root `.env` overrides credentials or database names, use a clean checkout
or align the connection URL with those settings.

Expected result: healthy PostgreSQL/OPA, passing dependency checks, passing
end-to-end workflows, and `.cache/bin/thinkpixelag`. The workflows exercise
registry security, Run admission/replay/lifecycle, resources and revocation
against PostgreSQL. They compose test handlers and fixtures; they do not leave
an authenticated public API running. OPA starts without a policy bundle.

Next, use the [local sandbox](examples/local-sandbox/README.md) to start the
binary and inspect its operational endpoints. For governed HTTP clients, follow
[installation](operations/installation.md) and [integrations](operations/integrations.md):
OIDC, approved agent/policy provisioning and runtime composition are required.
The current executable has no registration/policy-management bootstrap endpoint
and AG does not run Codex; harness execution belongs to ThinkPixelAR.

The database is retained for reuse. To stop dependencies without removing the
volume, run `make dev-down` in the same shell. Do not run `make dev-reset` unless
you intend to delete that example project's data.
